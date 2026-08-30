package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"

	"health-nexus/internal/shared/constants"
)

// rewriteTimeout 改写兜底超时：仅当客户端配置未携带 timeout 时使用。
// 实际改写超时优先取客户端配置（管理后台 provider 统一 60s / config.yaml rewrite 30s），
// 与 HTTP ResponseHeaderTimeout 保持一致，避免 context 先于 HTTP 层超时导致误判。
const rewriteTimeout = 30 * time.Second

// rewriteSystemPrompt 改写系统提示：把追问改写为不含代词的独立问题。
const rewriteSystemPrompt = `你是一个问题改写助手。根据对话历史，把用户最新追问改写为一个独立、完整、不含代词的问题。
规则：
1. 只输出改写后的问题，不要任何解释或前后缀
2. 若无需改写（首问或无歧义），原样返回用户问题
3. 将"他/她/它/这个/那个"等指代词替换为历史中的具体对象`

// Rewriter 查询改写接口，生成 Standalone Question（REQ-CHAT-006）。
type Rewriter interface {
	ToStandaloneQuestion(ctx context.Context, userQuery string, history []Message) (string, error)
}

// ToStandaloneQuestion 使用 RewriteModel 将追问改写为独立问题。
// 超时 5 秒；失败返回 error，由调用方降级为原始查询。
func (c *Client) ToStandaloneQuestion(ctx context.Context, userQuery string, history []Message) (string, error) {
	if c.chat == nil {
		return "", ErrNotConfigured
	}
	timeout := c.cfg.Timeout
	if timeout <= 0 {
		timeout = rewriteTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	msgs := buildRewriteMessages(userQuery, history)
	// 无专用 rewrite 模型时复用主对话模型（rewrite 本质是轻量 LLM 调用）
	model := c.cfg.RewriteModel
	if model == "" {
		model = c.cfg.ChatModel
	}
	resp, err := c.chat.CreateChatCompletion(ctx, c.chatRequestPlain(model, msgs))
	if err != nil {
		return "", fmt.Errorf("rewrite query: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("rewrite query: empty choices")
	}
	q := strings.TrimSpace(resp.Choices[0].Message.Content)
	if q == "" {
		return "", errors.New("rewrite query: empty result")
	}
	return q, nil
}

// buildRewriteMessages 构造改写请求消息：系统提示 + 最近 HistoryTurns 轮历史 + 当前问题。
// 一轮 = user + assistant 两条消息。
func buildRewriteMessages(userQuery string, history []Message) []openai.ChatCompletionMessage {
	if historyLimit := constants.HistoryTurns * 2; len(history) > historyLimit {
		history = history[len(history)-historyLimit:]
	}
	msgs := make([]openai.ChatCompletionMessage, 0, len(history)+2)
	msgs = append(msgs, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleSystem,
		Content: rewriteSystemPrompt,
	})
	for _, h := range history {
		msgs = append(msgs, openai.ChatCompletionMessage{
			Role:    h.Role,
			Content: h.Content,
		})
	}
	msgs = append(msgs, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: userQuery,
	})
	return msgs
}
