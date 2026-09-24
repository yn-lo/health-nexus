package llm

import (
	"context"
	"fmt"
	"math"

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
//
// 注意：EmbeddingModel() 与 Embed() 是两次独立调用，热切换客户端上二者之间可能发生模型切换，
// 导致"用 A 生成向量、却按 B 记录/检索模型"（P2）。需要强一致时用 EmbedWithModel。
type EmbeddingModelNamer interface {
	EmbeddingModel() string
}

// EmbedderWithModel 可选接口：一次调用内用**同一客户端快照**完成"生成向量 + 返回模型标识"，
// 消除热切换下两次 Load() 的不一致窗口（P2 向量空间混用）。
// 写入侧据此记录切片模型、查询侧据此按同模型检索。
type EmbedderWithModel interface {
	// EmbedWithModel 生成向量并返回生成它们的模型名（同一客户端快照内完成）。
	EmbedWithModel(ctx context.Context, texts []string) (embeddings [][]float32, model string, err error)
}

// EmbedWithModel 用当前客户端快照一次完成向量生成与模型名读取（*Client）。
// Model 名来自 c.cfg.EmbeddingModel，与实际请求使用的模型必然一致（同一 c）。
func (c *Client) EmbedWithModel(
	ctx context.Context, texts []string,
) (embeddings [][]float32, model string, err error) {
	embeddings, err = c.Embed(ctx, texts)
	if err != nil {
		return nil, "", err
	}
	return embeddings, c.EmbeddingModel(), nil
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
			if err := validateEmbedding(e.Embedding); err != nil {
				return nil, fmt.Errorf("embed batch [%d:%d): %w", i, end, err)
			}
			result = append(result, e.Embedding)
		}
	}
	return result, nil
}

// validateEmbedding 校验供应商返回的向量是否可用（空 / NaN / Inf / 全零一律视为无效）。
// 供应商异常时可能返回 HTTP 200 + 无效向量：检索侧会得到"0 命中"，被上层当成
// "知识库没有相关内容"，把故障伪装成无资料（患者看到误导性拒答）；
// 写入侧则会把坏向量写进索引且无从察觉。故在唯一出口处拦截，让故障显式暴露。
func validateEmbedding(v []float32) error {
	if len(v) == 0 {
		return fmt.Errorf("invalid embedding: empty vector")
	}
	nonZero := false
	for _, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return fmt.Errorf("invalid embedding: contains NaN/Inf")
		}
		if x != 0 {
			nonZero = true
		}
	}
	if !nonZero {
		return fmt.Errorf("invalid embedding: vector is all zeros")
	}
	return nil
}
