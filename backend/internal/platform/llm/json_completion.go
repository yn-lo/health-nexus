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

// judgeTemperature 判定/审查类任务的采样温度。
// CompleteJSON 仅服务意图/风险判定（adapter.LLMAssessor）与答案审核（LLMOutputReviewer），
// 这类任务要求可复现、稳定的结构化输出，故锁定低温，覆盖 provider 的全局温度；
// 答案生成走 StreamChat，仍使用 provider 配置的温度，不受此影响。
const judgeTemperature = 0.1

// CompleteJSON 以 JSON 输出模式调用对话模型，返回响应原文（可能是 JSON 对象或带 ```json 围栏的文本）。
// 超时优先取调用方传入值；<=0 时使用客户端自身配置的超时（由 HTTP 层兜底）。
// 未配置（API Key 缺失）时返回 ErrNotConfigured，调用方按不可用降级。
// userContent 承载患者来源文本（统一审查 / 答案审核的输入），出站前做 PII 脱敏。
// 单次失败按"同 provider 重试 + 主备切换"策略处理（判定不可用会直接影响安全审查，必须有冗余）。
func (c *Client) CompleteJSON(
	ctx context.Context, systemPrompt, userContent string, timeout time.Duration,
) (string, error) {
	return withFailover(ctx, c, func(cc *Client) (string, error) {
		return cc.completeJSONOne(ctx, systemPrompt, userContent, timeout)
	})
}

// completeJSONOne 用本客户端完成一次结构化输出调用。
func (c *Client) completeJSONOne(
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
	// chatRequest：保留供应商配置的其它参数（temperature/top_p/max_tokens 会被下面覆盖）。
	req := c.chatRequest(c.cfg.ChatModel, msgs)
	// 本方法的唯一用途就是结构化判定/审核，强制 JSON 输出模式（不依赖 provider 配置里是否写了
	// response_format）——漏配会退化为“从自由文本里抠 JSON”，模型多输出一句话就导致判定不可用，
	// 患者侧表现为“未能完成安全审查”。供应商不支持时请求会报错，属于配置问题，应在连通性测试暴露。
	req.ResponseFormat = &openai.ChatCompletionResponseFormat{
		Type: openai.ChatCompletionResponseFormatTypeJSONObject,
	}
	// 判定/审查任务锁定低温，保证结构化输出与风险判定的确定性。
	req.Temperature = judgeTemperature
	resp, err := c.chat.CreateChatCompletion(ctx, req)
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
