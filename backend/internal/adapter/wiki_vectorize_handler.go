package adapter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"health-nexus/internal/domain/wiki/entity"
	"health-nexus/internal/domain/wiki/repository"
	wikiservice "health-nexus/internal/domain/wiki/service"
	"health-nexus/internal/platform/llm"
	"health-nexus/internal/shared/constants"
	"health-nexus/internal/shared/contenthash"

	asynqlib "github.com/hibiken/asynq"
	"github.com/pgvector/pgvector-go"
)

// articleFetcher 暴露 handler 所需的 article 读取能力（便于测试 mock）。
type articleFetcher interface {
	GetByID(ctx context.Context, id int64) (*entity.Article, error)
}

// chunkWriter 暴露 handler 所需的 chunk 写入能力（便于测试 mock）。
type chunkWriter interface {
	DeactivateByArticle(ctx context.Context, articleID int64) (int64, error)
	DeleteInactiveByArticle(ctx context.Context, articleID int64) (int64, error)
	Create(ctx context.Context, c *entity.ArticleChunk) error
}

// VectorizeHandler 处理 asynq TaskVectorizeArticle 任务（REQ-WIKI-012）。
// 流程：解析 articleID → 取已发布文章 → 读 RAG 配置切片参数 → 切片 → embedding → 失效旧切片 → 写入新切片。
type VectorizeHandler struct {
	articles articleFetcher
	chunks   chunkWriter
	embed    llm.Embedder
	cfg      wikiservice.RAGConfigProvider
}

// NewVectorizeHandler 构造向量化 handler。
// 接受 *repository.ArticleRepo / *repository.ChunkRepo 具体类型（均满足上述接口）。
// cfg 注入 RAG 配置提供者以动态读取 chunk_size/chunk_overlap；可为 nil（回退 constants 默认值）。
func NewVectorizeHandler(
	articles *repository.ArticleRepo, chunks *repository.ChunkRepo,
	embed llm.Embedder, cfg wikiservice.RAGConfigProvider,
) *VectorizeHandler {
	return &VectorizeHandler{articles: articles, chunks: chunks, embed: embed, cfg: cfg}
}

// resolveChunkConfig 解析切片参数：优先用 RAG 配置，失败或未注入时回退 constants 默认值。
func (h *VectorizeHandler) resolveChunkConfig(ctx context.Context) (size, overlap int) {
	size, overlap = constants.DefaultChunkSize, constants.DefaultChunkOverlap
	if h.cfg == nil {
		return
	}
	cfg, err := h.cfg.GetRAGConfig(ctx)
	if err != nil {
		slog.WarnContext(ctx, "wiki: get rag config for chunk params failed, use defaults", "err", err)
		return
	}
	if cfg == nil {
		return
	}
	if cfg.ChunkSize > 0 {
		size = cfg.ChunkSize
	}
	if cfg.ChunkOverlap >= 0 {
		overlap = cfg.ChunkOverlap
	}
	return
}

// HandleVectorize asynq task handler：payload 为 articleID 的十进制字符串（与 AsynqVectorizeEnqueuer.Enqueue 对齐）。
// 错误策略：
//   - 文章不存在/未发布/已归档：返回 SkipRetry，避免重试死循环。
//   - embedding 失败/DB 写入失败：返回 err 触发 asynq 重试。
func (h *VectorizeHandler) HandleVectorize(ctx context.Context, t *asynqlib.Task) error {
	id, err := strconv.ParseInt(string(t.Payload()), 10, 64)
	if err != nil {
		slog.ErrorContext(ctx, "wiki: vectorize task invalid payload",
			"payload", string(t.Payload()), "err", err)
		return fmt.Errorf("parse articleID %q: %w", string(t.Payload()), asynqlib.SkipRetry)
	}

	// 用 GetByID（不读 GetPublishedByID）避免 worker 重试时叠加 view_count 副作用。
	article, err := h.articles.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			slog.WarnContext(ctx, "wiki: vectorize article not found, skip retry", "article_id", id)
			return fmt.Errorf("article %d not found: %w", id, asynqlib.SkipRetry)
		}
		return fmt.Errorf("get article %d: %w", id, err)
	}
	if article.Status != constants.ArticleStatusPublished {
		slog.InfoContext(ctx, "wiki: vectorize article not published, skip retry",
			"article_id", id, "status", article.Status)
		return fmt.Errorf("article %d status=%s not published: %w",
			id, article.Status, asynqlib.SkipRetry)
	}

	chunkSize, chunkOverlap := h.resolveChunkConfig(ctx)
	chunkTexts := chunkContent(article.Content, chunkSize, chunkOverlap)
	if len(chunkTexts) == 0 {
		slog.InfoContext(ctx, "wiki: vectorize empty content, skip", "article_id", id)
		return nil
	}
	slog.InfoContext(ctx, "wiki: vectorize chunking",
		"article_id", id, "chunk_size", chunkSize, "chunk_overlap", chunkOverlap, "chunks", len(chunkTexts))

	embeddings, err := h.embed.Embed(ctx, chunkTexts)
	if err != nil {
		// LLM 未配置或调用失败：触发 asynq 重试；持续失败由 asynq 兜底进入死信。
		return fmt.Errorf("embed article %d: %w", id, err)
	}
	if len(embeddings) != len(chunkTexts) {
		return fmt.Errorf("embed article %d: vector count mismatch (got %d, want %d)",
			id, len(embeddings), len(chunkTexts))
	}

	// 先失效旧切片再写入新切片（与 Update 路径协同：Update 已先 DeactivateByArticle，
	// 这里二次调用幂等；Approve 路径首次写入时无旧切片，RowsAffected=0）。
	if _, err := h.chunks.DeactivateByArticle(ctx, id); err != nil {
		return fmt.Errorf("deactivate chunks for article %d: %w", id, err)
	}
	// 物理删除已失效切片：避免旧版本切片无限堆积（数据卫生）。
	if _, err := h.chunks.DeleteInactiveByArticle(ctx, id); err != nil {
		return fmt.Errorf("delete inactive chunks for article %d: %w", id, err)
	}
	for i, text := range chunkTexts {
		chunk := &entity.ArticleChunk{
			ArticleID:   id,
			ChunkIndex:  i,
			Content:     text,
			ContentHash: contenthash.SHA256(text),
			Embedding:   pgvector.NewVector(embeddings[i]),
			IsActive:    true,
			Version:     article.Version,
		}
		if err := h.chunks.Create(ctx, chunk); err != nil {
			return fmt.Errorf("create chunk[%d] for article %d: %w", i, id, err)
		}
	}

	slog.InfoContext(ctx, "wiki: vectorize article done",
		"article_id", id, "chunks", len(chunkTexts), "version", article.Version)
	return nil
}

// chunkContent 按 rune 切分 content 为带重叠的片段。
// ponytail: 简单定长滑动窗口；step=size-overlap，末尾不足 size 时取剩余部分，简化。
// 升级路径：按 markdown 结构（标题/段落）切片以保留语义边界。
func chunkContent(content string, size, overlap int) []string {
	if size <= 0 || strings.TrimSpace(content) == "" {
		return nil
	}
	if overlap < 0 || overlap >= size {
		overlap = 0
	}
	runes := []rune(content)
	if len(runes) <= size {
		return []string{string(runes)}
	}
	step := size - overlap
	out := make([]string, 0, len(runes)/step+1)
	for i := 0; i < len(runes); i += step {
		end := i + size
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
		if end == len(runes) {
			break
		}
	}
	return out
}
