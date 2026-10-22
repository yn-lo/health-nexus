package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/sashabaranov/go-openai"
)

// 故障转移策略参数。
//
// ponytail: 常量硬编码，不引入配置项——上限：运维无法按供应商调参；
// 升级路径：若确有需要，从 ai_providers.parameters 或全局配置读取。
const (
	// failoverMaxAttempts 单次调用允许的最大尝试次数（含同 provider 重试与切换备用）。
	failoverMaxAttempts = 4
	// maxAttemptsPerProvider 单个 provider 的最大尝试次数（仅可重试错误才重试）。
	maxAttemptsPerProvider = 2
	// failoverBaseDelay 首次重试退避基数，之后指数增长。
	failoverBaseDelay = 200 * time.Millisecond
	// failoverMaxDelay 单次退避上限。
	failoverMaxDelay = 2 * time.Second
	// breakerFailThreshold 连续失败达到该次数后熔断（冷却期内跳过该 provider）。
	breakerFailThreshold = 3
	// breakerCooldown 熔断冷却时长。
	breakerCooldown = 30 * time.Second
)

// failoverFirstByteTimeout 流式首 token 等待上限。
// 超时视为该 provider 不可用，切换备用；一旦收到首个 token 即锁定，不再切换。
// 变量而非常量：单测需要缩短等待时间。
var failoverFirstByteTimeout = 30 * time.Second

// errStreamClosedBeforeToken 上游在给出首个 token 前关闭流：允许切换备用。
var errStreamClosedBeforeToken = errors.New("llm: stream closed before first token")

// apiStatusError 手写 HTTP 调用（rerank）的带状态码错误，供错误分类使用。
type apiStatusError struct {
	Code int
	Body string
}

func (e *apiStatusError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.Code, e.Body)
}

// circuitBreaker 单 provider 的简易熔断器（进程内，随客户端热重载重建）。
type circuitBreaker struct {
	mu        sync.Mutex
	fails     int
	openUntil time.Time
}

// allow 返回当前是否允许调用（冷却期结束或从未熔断）。
func (b *circuitBreaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !time.Now().Before(b.openUntil)
}

// success 重置连续失败计数。
func (b *circuitBreaker) success() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fails = 0
	b.openUntil = time.Time{}
}

// failure 记录一次失败；达到阈值后进入冷却期。
func (b *circuitBreaker) failure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fails++
	if b.fails >= breakerFailThreshold {
		b.openUntil = time.Now().Add(breakerCooldown)
		b.fails = 0
	}
}

// NewFailoverClient 把按优先级排列的客户端组装成主备链：clients[0] 为主用，其余为备用。
// 未就绪（缺 API Key/模型）的客户端被剔除；全部不可用时返回 nil。
func NewFailoverClient(clients ...*Client) *Client {
	ready := make([]*Client, 0, len(clients))
	for _, c := range clients {
		if c != nil && c.IsReady() {
			ready = append(ready, c)
		}
	}
	if len(ready) == 0 {
		return nil
	}
	primary := ready[0]
	primary.fallbacks = ready[1:]
	return primary
}

// ProviderCount 返回主备链中的可用 provider 数量（含主用）。
func (c *Client) ProviderCount() int {
	if c == nil {
		return 0
	}
	return 1 + len(c.fallbacks)
}

// allClients 返回主用 + 备用的完整顺序。
func (c *Client) allClients() []*Client {
	if c == nil {
		return nil
	}
	out := make([]*Client, 0, 1+len(c.fallbacks))
	out = append(out, c)
	return append(out, c.fallbacks...)
}

// candidates 返回本次调用可尝试的 provider 顺序：先过滤未就绪，再跳过熔断冷却中的 provider。
// 若全部处于熔断冷却，则仍返回首个可用 provider（避免单 provider 场景永久不可用）。
func (c *Client) candidates() []*Client {
	ready := make([]*Client, 0, 1+len(c.fallbacks))
	allowed := make([]*Client, 0, 1+len(c.fallbacks))
	for _, cc := range c.allClients() {
		if cc == nil || !cc.IsReady() {
			continue
		}
		ready = append(ready, cc)
		if cc.breaker.allow() {
			allowed = append(allowed, cc)
		}
	}
	if len(allowed) > 0 {
		return allowed
	}
	if len(ready) > 0 {
		slog.Warn("llm: all providers circuit-open, retrying primary")
		return ready[:1]
	}
	return nil
}

// providerLabel 日志标识：模型@BaseURL（不含密钥）。
func (c *Client) providerLabel() string {
	return c.cfg.ChatModel + "@" + c.cfg.BaseURL
}

// isRetryable 判断错误是否值得重试/切换备用。
// 白名单：429、408、5xx、网络层错误（状态码 0）与供应商异常响应（如无效向量）。
// 黑名单：未配置、客户端取消、401/403/400/404（配置或权限错误，重试无意义）。
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNotConfigured) || errors.Is(err, context.Canceled) {
		return false
	}
	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		return retryableStatus(apiErr.HTTPStatusCode)
	}
	var reqErr *openai.RequestError
	if errors.As(err, &reqErr) {
		return retryableStatus(reqErr.HTTPStatusCode)
	}
	var statusErr *apiStatusError
	if errors.As(err, &statusErr) {
		return retryableStatus(statusErr.Code)
	}
	return true
}

// retryableStatus 按 HTTP 状态码判定是否可重试；0 表示网络层错误。
func retryableStatus(code int) bool {
	switch code {
	case 0, http.StatusRequestTimeout, http.StatusConflict, http.StatusTooEarly, http.StatusTooManyRequests:
		return true
	}
	return code >= http.StatusInternalServerError
}

// failoverBackoff 指数退避：attempt 从 1 开始。
func failoverBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := failoverBaseDelay << (attempt - 1)
	if d > failoverMaxDelay {
		d = failoverMaxDelay
	}
	return d
}

// sleep 等待 d；ctx 取消时返回 false。
func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// withFailover 对非流式调用执行"同 provider 重试 + 主备切换"。
// fn 针对单个客户端执行一次完整调用；返回首个成功结果。
func withFailover[T any](ctx context.Context, c *Client, fn func(*Client) (T, error)) (T, error) {
	var zero T
	cands := c.candidates()
	if len(cands) == 0 {
		return zero, ErrNotConfigured
	}
	var lastErr error
	attempts := 0
	for _, cc := range cands {
		for try := 0; try < maxAttemptsPerProvider; try++ {
			if attempts >= failoverMaxAttempts {
				break
			}
			if attempts > 0 && !sleep(ctx, failoverBackoff(attempts)) {
				if lastErr != nil {
					return zero, lastErr
				}
				return zero, ctx.Err()
			}
			attempts++
			v, err := fn(cc)
			if err == nil {
				cc.breaker.success()
				return v, nil
			}
			lastErr = err
			// 调用方取消/超时不是 provider 的错，不计入熔断。
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				return zero, err
			}
			cc.breaker.failure()
			slog.WarnContext(ctx, "llm: provider call failed",
				"provider", cc.providerLabel(), "attempt", attempts, "retryable", isRetryable(err), "err", err)
			if !isRetryable(err) {
				break
			}
		}
		if attempts >= failoverMaxAttempts {
			break
		}
	}
	return zero, lastErr
}

// streamAttemptResult 单次流式尝试的结果。
type streamAttemptResult struct {
	forwarded bool // 是否已向消费者投递过片段（投递后不可再切换）
	completed bool // 是否收到模型完成信号
	canceled  bool // 调用方取消导致的中断（不计入 provider 熔断）
}

// StreamChat 流式生成聊天回复，支持首 token 前的主备切换。
// 成功建立流后立即返回 channel；切换/failover 在后台完成。
// 语义与单客户端一致：完成信号见 StreamChunk.Done；context 取消时直接关闭 channel。
func (c *Client) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	cands := c.candidates()
	if len(cands) == 0 {
		return nil, ErrNotConfigured
	}
	ch := make(chan StreamChunk)
	go func() {
		defer close(ch)
		c.pumpWithFailover(ctx, req, cands, ch)
	}()
	return ch, nil
}

// pumpWithFailover 依次尝试主备 provider，直到某个 provider 投递出首个 token（此后锁定）。
func (c *Client) pumpWithFailover(
	ctx context.Context, req ChatRequest, cands []*Client, out chan<- StreamChunk,
) {
	var lastErr error
	attempts := 0
	for _, cc := range cands {
		for try := 0; try < maxAttemptsPerProvider && attempts < failoverMaxAttempts; try++ {
			if !waitBeforeAttempt(ctx, attempts) {
				return // 客户端取消：保持"取消不投递错误"的既有语义
			}
			attempts++
			res, err := cc.streamAttempt(ctx, req, out)
			if res.forwarded {
				cc.recordStreamResult(res)
				if err != nil {
					slog.WarnContext(ctx, "llm: stream ended abnormally after tokens",
						"provider", cc.providerLabel(), "err", err)
				}
				return
			}
			if err == nil {
				cc.breaker.success()
				return
			}
			lastErr = err
			if abortOnClientError(ctx, cc, err, attempts) {
				return
			}
			if !isRetryable(err) {
				break
			}
		}
	}
	if lastErr == nil {
		lastErr = ErrNotConfigured
	}
	sendChunk(ctx, out, StreamChunk{Err: lastErr})
}

// waitBeforeAttempt 首次尝试不等待；后续尝试按指数退避，ctx 取消时返回 false。
func waitBeforeAttempt(ctx context.Context, attempts int) bool {
	if attempts == 0 {
		return true
	}
	return sleep(ctx, failoverBackoff(attempts))
}

// recordStreamResult 按是否收到完成信号更新熔断状态（调用方取消不计入失败）。
func (c *Client) recordStreamResult(res streamAttemptResult) {
	if res.canceled {
		return
	}
	if res.completed {
		c.breaker.success()
		return
	}
	c.breaker.failure()
}

// abortOnClientError 调用方取消/超时不计入 provider 失败；返回 true 表示应立即结束。
func abortOnClientError(ctx context.Context, cc *Client, err error, attempts int) bool {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return true
	}
	cc.breaker.failure()
	slog.WarnContext(ctx, "llm: stream attempt failed",
		"provider", cc.providerLabel(), "attempt", attempts, "retryable", isRetryable(err), "err", err)
	return false
}

// streamAttempt 执行一次流式尝试：等待首个片段，成功则继续转发到 out 直到流结束。
// 关键约束：一旦投递出首个 token 就返回 forwarded=true，调用方不得再切换 provider
// （否则患者会看到两段不同模型拼接的答案）。
func (c *Client) streamAttempt(
	ctx context.Context, req ChatRequest, out chan<- StreamChunk,
) (streamAttemptResult, error) {
	attemptCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream, err := c.streamChatOne(attemptCtx, req)
	if err != nil {
		return streamAttemptResult{}, err
	}

	first, canceled, err := waitFirstChunk(ctx, stream)
	if err != nil {
		return streamAttemptResult{canceled: canceled}, err
	}
	if first.Err != nil {
		return streamAttemptResult{}, first.Err
	}
	if first.Done { // 无 token 直接完成（如空回答）
		sendChunk(ctx, out, first)
		return streamAttemptResult{forwarded: true, completed: true}, nil
	}
	if !sendChunk(ctx, out, first) {
		return streamAttemptResult{forwarded: true, canceled: true}, ctx.Err()
	}
	completed, canceled := forwardStream(ctx, stream, out)
	return streamAttemptResult{forwarded: true, completed: completed, canceled: canceled}, nil
}

// waitFirstChunk 等待首个片段；超时视为该 provider 不可用（可切换备用）。
func waitFirstChunk(
	ctx context.Context, stream <-chan StreamChunk,
) (chunk StreamChunk, canceled bool, err error) {
	timer := time.NewTimer(failoverFirstByteTimeout)
	defer timer.Stop()
	select {
	case first, ok := <-stream:
		if !ok {
			return StreamChunk{}, false, errStreamClosedBeforeToken
		}
		return first, false, nil
	case <-timer.C:
		return StreamChunk{}, false, fmt.Errorf("llm: no first token within %s", failoverFirstByteTimeout)
	case <-ctx.Done():
		return StreamChunk{}, true, ctx.Err()
	}
}

// forwardStream 把已建立流的后续片段转发给消费者。
// 返回是否收到完成信号、是否因调用方取消而中止。
func forwardStream(
	ctx context.Context, stream <-chan StreamChunk, out chan<- StreamChunk,
) (completed, canceled bool) {
	for {
		select {
		case chunk, ok := <-stream:
			if !ok {
				return false, false
			}
			if chunk.Err != nil {
				sendChunk(ctx, out, chunk)
				return false, false
			}
			if chunk.Done {
				sendChunk(ctx, out, chunk)
				return true, false
			}
			if !sendChunk(ctx, out, chunk) {
				return false, true
			}
		case <-ctx.Done():
			return false, true
		}
	}
}
