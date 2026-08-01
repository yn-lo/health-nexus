package llm

import (
	"context"
	"log/slog"
	"sync/atomic"
)

// SwappableClient 可热切换的 LLM 客户端包装器。
// 内部持有 atomic.Pointer[Client]，支持运行时原子替换（配置变更后无需重启）。
// 实现 Streamer / Embedder / Rewriter / Reranker 四个接口，委托给当前持有的 Client。
// Swap(nil) 表示该能力未配置，所有方法返回 ErrNotConfigured。
type SwappableClient struct {
	ptr atomic.Pointer[Client]
}

// NewSwappableClient 创建 SwappableClient，持有初始 client。
func NewSwappableClient(initial *Client) *SwappableClient {
	sc := &SwappableClient{}
	sc.ptr.Store(initial)
	return sc
}

// Swap 原子替换内部客户端。调用方在配置变更后调用此方法热切换。
// 传入 nil 表示该能力未配置（IsReady() 返回 false）。
func (sc *SwappableClient) Swap(client *Client) {
	old := sc.ptr.Load()
	sc.ptr.Store(client)
	switch {
	case old != nil && client != nil:
		slog.Info("llm: hot-swapped client",
			"old_model", old.cfg.ChatModel, "new_model", client.cfg.ChatModel)
	case client == nil:
		slog.Warn("llm: hot-swapped client to nil (capability disabled)")
	default:
		slog.Info("llm: hot-swapped client (capability enabled)",
			"new_model", client.cfg.ChatModel)
	}
}

// Load 返回当前客户端，可能为 nil。
// 导出供 DI 层构建 LLMSafetyChecker（需要 *Client 而非接口）。
func (sc *SwappableClient) Load() *Client {
	return sc.ptr.Load()
}

// IsReady 返回当前客户端是否已配置可用。
func (sc *SwappableClient) IsReady() bool {
	c := sc.Load()
	return c != nil && c.IsReady()
}

// ChatModel 返回主对话模型名。
func (sc *SwappableClient) ChatModel() string {
	if c := sc.Load(); c != nil {
		return c.ChatModel()
	}
	return ""
}

// EmbeddingModel 返回 embedding 模型名。
func (sc *SwappableClient) EmbeddingModel() string {
	if c := sc.Load(); c != nil {
		return c.EmbeddingModel()
	}
	return ""
}

// RewriteModel 返回改写用小模型名。
func (sc *SwappableClient) RewriteModel() string {
	if c := sc.Load(); c != nil {
		return c.RewriteModel()
	}
	return ""
}

// StreamChat 委托给当前客户端。未配置时返回 ErrNotConfigured。
func (sc *SwappableClient) StreamChat(
	ctx context.Context, req ChatRequest,
) (<-chan StreamChunk, error) {
	if c := sc.Load(); c != nil {
		return c.StreamChat(ctx, req)
	}
	return nil, ErrNotConfigured
}

// Embed 委托给当前客户端。未配置时返回 ErrNotConfigured。
func (sc *SwappableClient) Embed(
	ctx context.Context, texts []string,
) ([][]float32, error) {
	if c := sc.Load(); c != nil {
		return c.Embed(ctx, texts)
	}
	return nil, ErrNotConfigured
}

// ToStandaloneQuestion 委托给当前客户端。未配置时返回 ErrNotConfigured。
func (sc *SwappableClient) ToStandaloneQuestion(
	ctx context.Context, userQuery string, history []Message,
) (string, error) {
	if c := sc.Load(); c != nil {
		return c.ToStandaloneQuestion(ctx, userQuery, history)
	}
	return "", ErrNotConfigured
}

// Rerank 委托给当前客户端。未配置时返回 ErrNotConfigured。
func (sc *SwappableClient) Rerank(
	ctx context.Context, query string, documents []string, topK int,
) ([]RerankResult, error) {
	if c := sc.Load(); c != nil {
		return c.Rerank(ctx, query, documents, topK)
	}
	return nil, ErrNotConfigured
}

// SwappableClients 4 个能力的可热切换客户端集合。
// 每个字段是 *SwappableClient，通过 Swap 原子替换底层 *Client。
type SwappableClients struct {
	Chat    *SwappableClient
	Embed   *SwappableClient
	Rerank  *SwappableClient
	Rewrite *SwappableClient
}

// 编译期断言：SwappableClient 实现全部对外接口。
var (
	_ Streamer = (*SwappableClient)(nil)
	_ Embedder = (*SwappableClient)(nil)
	_ Rewriter = (*SwappableClient)(nil)
	_ Reranker = (*SwappableClient)(nil)
)
