package llm

import (
	"context"
	"fmt"

	"github.com/sashabaranov/go-openai"
)

// embeddingBatchSize OpenAI 兼容 embeddings 单次请求输入上限。
const embeddingBatchSize = 100

// Embedder 向量生成接口，用于文章切片向量化与查询向量化。
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Embed 批量生成向量，每批最多 100 个文本，返回与输入顺序一致的向量切片。
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if c.chat == nil {
		return nil, ErrNotConfigured
	}
	result := make([][]float32, 0, len(texts))
	for i := 0; i < len(texts); i += embeddingBatchSize {
		end := i + embeddingBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		resp, err := c.chat.CreateEmbeddings(ctx, openai.EmbeddingRequestStrings{
			Input: texts[i:end],
			Model: openai.EmbeddingModel(c.cfg.EmbeddingModel),
		})
		if err != nil {
			return nil, fmt.Errorf("embed batch [%d:%d): %w", i, end, err)
		}
		if len(resp.Data) != end-i {
			return nil, fmt.Errorf("embed batch [%d:%d): expected %d embeddings, got %d", i, end, end-i, len(resp.Data))
		}
		for _, e := range resp.Data {
			result = append(result, e.Embedding)
		}
	}
	return result, nil
}
