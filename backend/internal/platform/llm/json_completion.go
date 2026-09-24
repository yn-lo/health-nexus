package llm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sashabaranov/go-openai"

	"health-nexus/internal/shared/mask"
)

// JSONCompleter 一次性（非流式）结构化输出能力。
// 供上层适配器实现"结构化审查"协议：由适配器负责提示词与结果解析，platform 只负责请求与超时。
type JSONCompleter interface {
	CompleteJSON(ctx context.Context, systemPrompt, userContent string, timeout time.Duration) (string, error)
}

// CompleteJSON 以 JSON 输出模式调用对话模型，返回响应原文（可能是 JSON 对象或带 ```json 围栏的文本）。
// 超时优先取调用方传入值；<=0 时使用客户端自身配置的超时（由 HTTP 层兜底）。
// 未配置（API Key 缺失）时返回 ErrNotConfigured，调用方按不可用降级。
// userContent 承载患者来源文本（统一审查 / 答案审核的输入），出站前做 PII 脱敏。
func (c *Client) CompleteJSON(
	ctx context.Context, systemPrompt, userContent string, timeout time.Duration,
) (string, error) {
	if c == nil || c.chat == nil {
		return "", ErrNotConfigured
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	msgs := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
		{Role: openai.ChatMessageRoleUser, Content: mask.SanitizePII(userContent)},
	}
	// chatRequest：保留供应商配置的 response_format=json_object（结构化审查需要合法 JSON）。
	resp, err := c.chat.CreateChatCompletion(ctx, c.chatRequest(c.cfg.ChatModel, msgs))
	if err != nil {
		return "", fmt.Errorf("json completion: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("json completion: empty choices")
	}
	return resp.Choices[0].Message.Content, nil
}

// CompleteJSON 委托给当前客户端。未配置时返回 ErrNotConfigured。
func (sc *SwappableClient) CompleteJSON(
	ctx context.Context, systemPrompt, userContent string, timeout time.Duration,
) (string, error) {
	if c := sc.Load(); c != nil {
		return c.CompleteJSON(ctx, systemPrompt, userContent, timeout)
	}
	return "", ErrNotConfigured
}

// 编译期断言：SwappableClient 实现结构化输出接口。
var _ JSONCompleter = (*SwappableClient)(nil)
