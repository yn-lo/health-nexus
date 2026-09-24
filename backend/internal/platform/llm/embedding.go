package llm

import (
	"context"
	"fmt"

	"github.com/sashabaranov/go-openai"

	"health-nexus/internal/shared/mask"
)

// embeddingBatchSize OpenAI 兼容 embeddings 单次请求输入上限。
const embeddingBatchSize = 100

// Embedder 向量生成接口，用于文章切片向量化与查询向量化。
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// EmbeddingModelNamer 可选接口：Embedder 自报当前生效的向量模型名（*Client / *SwappableClient 均实现）。
// 用于记录切片向量所属模型版本，并在检索侧过滤不同模型的向量（P0：模型版本一致性）。
// 定义为可选接口而非扩展 Embedder——避免强制所有实现（含测试 mock）同步改造。
type EmbeddingModelNamer interface {
	EmbeddingModel() string
}

// Embed 批量生成向量，每批最多 100 个文本，返回与输入顺序一致的向量切片。
// 出站前对输入文本做 PII 脱敏：写库（知识库切片）与检索（患者问题）共用本方法、
// 无法区分用途，故统一处理。知识库切片几乎不含长数字，误伤可忽略；
// 且写库与检索两侧同样脱敏，向量空间保持一致。
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
			Input: mask.SanitizePIIAll(texts[i:end]),
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
