package llm

import (
	"context"
	"fmt"
	"time"

	"github.com/sashabaranov/go-openai"

	"health-nexus/internal/shared/constants"
)

// pingTimeout 连通性测试超时——短超时让前端快速看到结果，超时即视为不可达。
const pingTimeout = 10 * time.Second

// Ping 按 providerType 调用一次最小请求验证连通性（方案 C：每个 provider 都可测试）。
// 用于 POST /api/staff/config/ai-providers/{id}/test 端点。
// 每种类型走与实际业务调用相同的路径，确保"所见即所得"：
//   - LLM/Rewrite → chat completion（与 StreamChat/ToStandaloneQuestion 同端点）
//   - Rerank → /v1/rerank（与 Rerank() 同端点）
//   - Embedding → embeddings（与 Embed() 同端点）
func (c *Client) Ping(ctx context.Context, providerType string) error {
	if c.chat == nil {
		return ErrNotConfigured
	}
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()

	switch providerType {
	case constants.ProviderTypeLLM, constants.ProviderTypeRewrite:
		return c.pingChat(ctx, providerType)
	case constants.ProviderTypeRerank:
		return c.pingRerank(ctx)
	case constants.ProviderTypeEmbedding:
		return c.pingEmbed(ctx)
	default:
		return fmt.Errorf("ping: unknown provider_type %q", providerType)
	}
}

// pingChat 用 chat completion 验证连通。chat/rewrite 走此路径。
func (c *Client) pingChat(ctx context.Context, providerType string) error {
	model := c.cfg.ChatModel
	if providerType == constants.ProviderTypeRewrite {
		model = c.cfg.RewriteModel
	}
	if model == "" {
		return fmt.Errorf("ping: model not configured for provider_type %q", providerType)
	}
	resp, err := c.chat.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: "ping"},
		},
		MaxTokens: 1, // 最小输出，省 token
	})
	if err != nil {
		return fmt.Errorf("ping %s: %w", providerType, err)
	}
	if len(resp.Choices) == 0 {
		return fmt.Errorf("ping %s: empty choices", providerType)
	}
	return nil
}

// pingEmbed 用 embeddings 验证连通。
func (c *Client) pingEmbed(ctx context.Context) error {
	if c.cfg.EmbeddingModel == "" {
		return fmt.Errorf("ping: embedding model not configured")
	}
	resp, err := c.chat.CreateEmbeddings(ctx, openai.EmbeddingRequestStrings{
		Input: []string{"ping"},
		Model: openai.EmbeddingModel(c.cfg.EmbeddingModel),
	})
	if err != nil {
		return fmt.Errorf("ping embedding: %w", err)
	}
	if len(resp.Data) == 0 {
		return fmt.Errorf("ping embedding: empty data")
	}
	return nil
}

// pingRerank 用 /v1/rerank 端点验证连通——与 Rerank() 走同一路径，所见即所得。
func (c *Client) pingRerank(ctx context.Context) error {
	_, err := c.Rerank(ctx, "ping", []string{"ping"}, 1)
	if err != nil {
		return fmt.Errorf("ping rerank: %w", err)
	}
	return nil
}
