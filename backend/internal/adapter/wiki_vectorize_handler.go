package adapter

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"regexp"
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

// articleFetcher 暴露 handler 所需的 article 读取与行锁版本读取能力（便于测试 mock）。
type articleFetcher interface {
	GetByID(ctx context.Context, id int64) (*entity.Article, error)
	// LockVersion 事务内锁定文章行并返回当前版本（FOR UPDATE），见 ArticleRepo.LockVersion。
	LockVersion(ctx context.Context, id int64) (int, error)
}

// chunkWriter 暴露 handler 所需的 chunk 写入能力（便于测试 mock）。
type chunkWriter interface {
	DeactivateByArticle(ctx context.Context, articleID int64) (int64, error)
	DeleteInactiveByArticle(ctx context.Context, articleID int64) (int64, error)
	Create(ctx context.Context, c *entity.ArticleChunk) error
}

// TxRunner 事务执行能力（消费者定义，ISP）。*postgres.TxManager 实现此接口。
type TxRunner interface {
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// VectorizeHandler 处理 asynq TaskVectorizeArticle 任务（REQ-WIKI-012）。
// 流程：解析 articleID → 取已发布文章 → 读 RAG 配置切片参数 → 切片 → embedding
// → 事务内锁定文章复核版本 → 原子替换切片（失效旧 + 删除 + 写入新）。
type VectorizeHandler struct {
	articles articleFetcher
	chunks   chunkWriter
	embed    llm.Embedder
	cfg      wikiservice.RAGConfigProvider
	tx       TxRunner // nil 时退化为无事务写入（仅测试兼容；生产必须注入）
}

// NewVectorizeHandler 构造向量化 handler。
// 接受 *repository.ArticleRepo / *repository.ChunkRepo 具体类型（均满足上述接口）。
// cfg 注入 RAG 配置提供者以动态读取 chunk_size/chunk_overlap；可为 nil（回退 constants 默认值）。
// tx 注入事务管理器，用于切片的原子替换与版本校验。
func NewVectorizeHandler(
	articles *repository.ArticleRepo, chunks *repository.ChunkRepo,
	embed llm.Embedder, cfg wikiservice.RAGConfigProvider, tx TxRunner,
) *VectorizeHandler {
	return &VectorizeHandler{articles: articles, chunks: chunks, embed: embed, cfg: cfg, tx: tx}
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
	// 切片用纯文本：正文为富文本 HTML，直接切片会把标签、样式与 <img src> URL 一并喂给
	// embedding，成为检索噪声（且图片本身不参与向量化）。
	plainText := htmlToPlainText(article.Content)
	chunkTexts := chunkContent(plainText, chunkSize, chunkOverlap)
	// P1：纯图片/空正文是**有效重建结果**（新版正文不含可检索文本），必须清除旧切片——
	// 直接 return nil 会让已被替换的旧指引持续被检索命中（患者收到过期内容）。
	// 走与正常路径相同的版本复核 + 事务性清除，只是不写入新切片。
	if len(chunkTexts) == 0 {
		if err := h.clearChunks(ctx, id, article.Version); err != nil {
			return err
		}
		slog.WarnContext(ctx, "wiki: vectorize empty content, cleared stale chunks",
			"article_id", id, "version", article.Version)
		return nil
	}
	slog.InfoContext(ctx, "wiki: vectorize chunking",
		"article_id", id, "chunk_size", chunkSize, "chunk_overlap", chunkOverlap, "chunks", len(chunkTexts))

	// P2：用**同一客户端快照**完成"生成向量 + 读取模型标识"。
	// EmbedWithModel 在单次调用内固定客户端，消除"读取模型名 / 生成向量"两次 Load 之间
	// 发生热切换导致 A 模型向量被标成 B 模型的问题（向量空间混用、自动重建失效）。
	embeddings, model, err := h.embedWithModel(ctx, chunkTexts)
	if err != nil {
		// LLM 未配置或调用失败：触发 asynq 重试；持续失败由 asynq 兜底进入死信。
		return fmt.Errorf("embed article %d: %w", id, err)
	}
	if len(embeddings) != len(chunkTexts) {
		return fmt.Errorf("embed article %d: vector count mismatch (got %d, want %d)",
			id, len(embeddings), len(chunkTexts))
	}

	// 事务内原子替换切片：锁定文章行复核版本 → 失效旧切片 → 删除已失效 → 写入新切片。
	// embedding 是长耗时操作，期间文章可能已被再次更新（新任务已入队）。若不复核就直接写入，
	// 后完成的旧任务会把新版本切片失效并删除，造成旧内容覆盖新版本（P0 向量版本一致性）。
	swap := func(ctx context.Context) error {
		if h.tx == nil {
			// 未注入事务管理器（单测）时不具备行锁，仍执行版本复核。
			return h.swapChunks(ctx, id, article.Version, model, chunkTexts, embeddings)
		}
		return h.tx.WithTx(ctx, func(ctx context.Context) error {
			return h.swapChunks(ctx, id, article.Version, model, chunkTexts, embeddings)
		})
	}
	if err := swap(ctx); err != nil {
		return err
	}

	slog.InfoContext(ctx, "wiki: vectorize article done",
		"article_id", id, "chunks", len(chunkTexts), "version", article.Version)
	return nil
}

// embedWithModel 生成向量并返回生成它们的模型名，二者来自同一客户端快照。
// 若 embedder 支持 EmbedderWithModel（*SwappableClient / *Client）则一次调用完成；
// 否则退化为"先读模型名再 Embed"（旧 mock 兼容，存在热切换不一致窗口，仅测试路径）。
func (h *VectorizeHandler) embedWithModel(
	ctx context.Context, texts []string,
) (embeddings [][]float32, model string, err error) {
	if em, ok := h.embed.(llm.EmbedderWithModel); ok {
		return em.EmbedWithModel(ctx, texts)
	}
	embeddings, err = h.embed.Embed(ctx, texts)
	if err != nil {
		return nil, h.embeddingModel(), err
	}
	return embeddings, h.embeddingModel(), nil
}

// embeddingModel 返回当前生效的向量模型名；实现未暴露时返回空串（视为未知，检索侧不过滤）。
func (h *VectorizeHandler) embeddingModel() string {
	if p, ok := h.embed.(llm.EmbeddingModelNamer); ok {
		return p.EmbeddingModel()
	}
	return ""
}

// clearChunks 纯图片/空正文版本重建：版本复核后事务性清除旧切片（不写入新切片）。
// 与 swapChunks 相同的版本守卫，避免 embedding 期间文章被再次更新后误清白更新的切片。
func (h *VectorizeHandler) clearChunks(ctx context.Context, id int64, version int) error {
	doClear := func(ctx context.Context) error {
		return h.clearChunksTx(ctx, id, version)
	}
	if h.tx == nil {
		return doClear(ctx)
	}
	return h.tx.WithTx(ctx, doClear)
}

// clearChunksTx 在调用方事务内完成版本复核 + 旧切片清除。
func (h *VectorizeHandler) clearChunksTx(ctx context.Context, id int64, version int) error {
	cur, err := h.articles.LockVersion(ctx, id)
	if err != nil {
		return fmt.Errorf("lock article %d version: %w", id, err)
	}
	if cur != version {
		slog.WarnContext(ctx, "wiki: article version changed during embedding, skip clearing chunks",
			"article_id", id, "embedded_version", version, "current_version", cur)
		return fmt.Errorf("article %d version changed %d -> %d during embedding: %w",
			id, version, cur, asynqlib.SkipRetry)
	}
	if _, err := h.chunks.DeactivateByArticle(ctx, id); err != nil {
		return fmt.Errorf("deactivate chunks for article %d: %w", id, err)
	}
	if _, err := h.chunks.DeleteInactiveByArticle(ctx, id); err != nil {
		return fmt.Errorf("delete inactive chunks for article %d: %w", id, err)
	}
	return nil
}

// swapChunks 在调用方事务内完成版本复核 + 切片原子替换。
// 版本已变化（embedding 期间文章被更新）时放弃本次写入：新版本自有其入队任务负责重建，
// 本任务返回 SkipRetry 避免重复覆盖（重试也仍会被版本复核拦下）。
func (h *VectorizeHandler) swapChunks(
	ctx context.Context, id int64, version int, model string, chunkTexts []string, embeddings [][]float32,
) error {
	cur, err := h.articles.LockVersion(ctx, id)
	if err != nil {
		return fmt.Errorf("lock article %d version: %w", id, err)
	}
	if cur != version {
		slog.WarnContext(ctx, "wiki: article version changed during embedding, discard stale chunks",
			"article_id", id, "embedded_version", version, "current_version", cur)
		return fmt.Errorf("article %d version changed %d -> %d during embedding: %w",
			id, version, cur, asynqlib.SkipRetry)
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
	// 记录向量所属模型：模型切换后据此识别需重建的切片（检索侧按模型过滤，避免新旧向量混用）。
	// model 为任务开始时的快照，与 embeddings 实际使用的模型一致（P1）。
	for i, text := range chunkTexts {
		chunk := &entity.ArticleChunk{
			ArticleID:      id,
			ChunkIndex:     i,
			Content:        text,
			ContentHash:    contenthash.SHA256(text),
			Embedding:      pgvector.NewVector(embeddings[i]),
			EmbeddingModel: model,
			IsActive:       true,
			Version:        version,
		}
		if err := h.chunks.Create(ctx, chunk); err != nil {
			return fmt.Errorf("create chunk[%d] for article %d: %w", i, id, err)
		}
	}
	return nil
}

// reHTMLBlockTags 匹配块级标签（含 <br>），转纯文本时替换为空格以保留语义边界，
// 否则 "<p>甲</p><p>乙</p>" 会被拼成 "甲乙"，跨块语义粘连。
var reHTMLBlockTags = regexp.MustCompile(
	`(?i)</?(?:p|div|br|hr|li|ul|ol|h[1-6]|blockquote|pre|table|thead|tbody|tr|td|th|section|article)[^>]*>`)

// reHTMLTags 匹配剩余任意标签（含 <img src="...">）：直接丢弃，不参与向量化。
var reHTMLTags = regexp.MustCompile(`<[^>]*>`)

// htmlToPlainText 将富文本 HTML 正文转为纯文本，供切片向量化使用。
// 步骤：块级标签→空格 → 其余标签（含 img）丢弃 → 解码 HTML 实体 → 空白归一。
// 空白归一同时清掉 &nbsp; 解码出的 NBSP，避免不可见字符混入向量。
func htmlToPlainText(s string) string {
	if !strings.Contains(s, "<") && !strings.Contains(s, "&") {
		// 已是纯文本：仅做去首尾空白，避免无谓的复制开销。
		return strings.TrimSpace(s)
	}
	s = reHTMLBlockTags.ReplaceAllString(s, " ")
	s = reHTMLTags.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}

// chunkContent 按 rune 切分 content 为带重叠的片段（content 应为纯文本，见 htmlToPlainText）。
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
