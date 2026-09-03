package service

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"health-nexus/internal/domain/wiki/repository"
	"health-nexus/internal/platform/llm"
	"health-nexus/internal/shared/constants"
	"health-nexus/internal/shared/rag"
)

// ChunkSearcher 切片检索能力（消费者定义，ISP）。由 repository.ChunkRepo 实现。
// 纯向量检索：仅依赖向量路（pgvector ANN）。
type ChunkSearcher interface {
	SearchByVector(
		ctx context.Context, embedding []float32, topK int, deptIDs []int64,
		similarityThreshold float64,
	) ([]repository.ChunkSearchHit, error)
}

// RAGSearchConfig wiki 域消费的 RAG 配置视图（消费者定义，ISP）。
// 同时承载检索参数（TopK/SimilarityThreshold/Rerank*/MaxChunks）与切片参数（ChunkSize/ChunkOverlap）：
// 前者由 SearchService 消费，后者由 adapter.VectorizeHandler 消费，共用同一 provider。
type RAGSearchConfig struct {
	TopK                int
	SimilarityThreshold float64
	RerankEnabled       bool
	RerankThreshold     float64
	MaxChunks           int
	ChunkSize           int
	ChunkOverlap        int
}

// RAGConfigProvider RAG 配置提供者（消费者定义，ISP）。由 config 域适配实现。
type RAGConfigProvider interface {
	GetRAGConfig(ctx context.Context) (*RAGSearchConfig, error)
}

// SearchService 知识检索服务（纯向量检索 + 可选 Rerank，REQ-WIKI-013/014）。
// 实现 shared 层定义的 rag.KnowledgeSearcher 接口（消费者定义，ISP），由 chat 域注入。
// 相关性过滤只依赖向量相似度阈值（SimilarityThreshold），低于阈值即裁剪——宁可不答也不给低质候选。
type SearchService struct {
	chunks  ChunkSearcher
	embed   llm.Embedder
	rerank  llm.Reranker
	cfgProv RAGConfigProvider
}

// NewSearchService 构造检索服务。所有依赖均可为 nil（降级返回空切片，避免阻塞 chat 域启动）。
// 完整功能需 chunks/embed/rerank/cfgProv 均注入；任一为 nil 时按 ponytail 降级原则返回空结果。
func NewSearchService(
	chunks ChunkSearcher, embed llm.Embedder, rerank llm.Reranker, cfgProv RAGConfigProvider,
) *SearchService {
	return &SearchService{chunks: chunks, embed: embed, rerank: rerank, cfgProv: cfgProv}
}

// 配置兜底默认值（与 config/entity.DefaultRAGConfig 对齐）。
const (
	defaultTopK                = 5
	defaultSimilarityThreshold = 0.75
	defaultRerankThreshold     = 0.5
	defaultMaxChunks           = 10
	defaultChunkSize           = 500
	defaultChunkOverlap        = 50
)

// defaultRAGSearchConfig 配置不可用时的兜底默认值。
// RerankEnabled 默认 true：提升检索质量；rerank 为 nil 时 SearchSimilarChunks 安全降级跳过。
var defaultRAGSearchConfig = RAGSearchConfig{
	TopK:                defaultTopK,
	SimilarityThreshold: defaultSimilarityThreshold,
	RerankEnabled:       true,
	RerankThreshold:     defaultRerankThreshold,
	MaxChunks:           defaultMaxChunks,
	ChunkSize:           defaultChunkSize,
	ChunkOverlap:        defaultChunkOverlap,
}

// topKMax 配置范围上限，与 config/entity.TopKMax 对齐。
const topKMax = 50

// SearchSimilarChunks 纯向量检索：pgvector ANN → similarity_threshold 过滤 → 可选 Rerank。
// 步骤：
//  1. 调用 LLM Embedding 生成查询向量
//  2. 调用 SearchByVector 获取 topK 候选（仅向量路，无 BM25）
//  3. similarity_threshold 强制过滤（唯一相关性闸门，低于阈值一律裁剪）
//  4. 向量相似度降序排序并截断到 top_k
//  5. 若 RAGConfig.RerankEnabled=true 且结果数 > 1，调用 LLM Rerank 重排（失败降级原顺序）
//
// Embedding 失败时直接向上返回 error（严禁静默降级，医疗场景宁报 503 也不给虚假否定）。
// 关键依赖未注入或向量检索无命中时返回空切片（chat 域据此拒答，做到"宁可不答"）。
func (s *SearchService) SearchSimilarChunks(ctx context.Context, q rag.SearchQuery) ([]rag.Chunk, error) {
	if s.chunks == nil || s.embed == nil || s.cfgProv == nil {
		// 依赖未注入：降级为空结果，chat 域走拒答路径（REQ-CHAT-003）。
		return []rag.Chunk{}, nil
	}

	cfg, topK, candidateK := s.resolveSearchParams(ctx, q)

	// 步骤 1：生成查询向量。失败时直接向上返回 error（严禁静默降级）。
	queryVec, err := s.embedQueryVec(ctx, q.Query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	// 步骤 2：仅向量检索。检索失败视为无命中（返回空，chat 域拒答），不静默回退。
	var deptIDs []int64
	if q.DeptID != nil {
		deptIDs = []int64{*q.DeptID}
	}
	hits, err := s.chunks.SearchByVector(ctx, queryVec, candidateK, deptIDs, cfg.SimilarityThreshold)
	if err != nil {
		slog.WarnContext(ctx, "wiki: vector search failed, degrading to empty", "err", err)
		return []rag.Chunk{}, nil
	}
	if len(hits) == 0 {
		return []rag.Chunk{}, nil
	}

	// 步骤 3~5：相似度过滤 → 排序截断 → 可选 Rerank。
	hits = s.applyFiltersAndRerank(ctx, q.Query, hits, cfg, topK)
	if len(hits) == 0 {
		return []rag.Chunk{}, nil
	}
	return toRAGChunks(hits), nil
}

// applyFiltersAndRerank 检索后处理：similarity_threshold 过滤 → 排序截断 → 可选 Rerank/MMR。
func (s *SearchService) applyFiltersAndRerank(
	ctx context.Context, query string, hits []repository.ChunkSearchHit, cfg *RAGSearchConfig, topK int,
) []repository.ChunkSearchHit {
	// 步骤 3：similarity_threshold 过滤（唯一相关性闸门，恒生效）。
	threshold := cfg.SimilarityThreshold
	hits = filterBySimilarity(hits, threshold)
	if len(hits) == 0 {
		return hits
	}

	slog.InfoContext(ctx, "wiki: RAG search detail",
		"candidates", len(hits),
		"similarity_threshold", threshold,
		"top_k", topK,
		"rerank_enabled", cfg.RerankEnabled && s.rerank != nil,
	)

	// 按向量相似度降序，截断到 topK。
	sort.SliceStable(hits, func(i, j int) bool {
		return hits[i].Score > hits[j].Score
	})
	if len(hits) > topK {
		hits = hits[:topK]
	}

	// 步骤 4：可选 Rerank。
	if cfg.RerankEnabled && s.rerank != nil && len(hits) > 1 {
		hits = s.applyRerank(ctx, query, hits, topK, cfg.RerankThreshold)
	}

	// 逐条记录检索详情。
	for i, c := range hits {
		slog.InfoContext(ctx, "wiki: RAG chunk detail",
			"rank", i+1,
			"chunk_id", c.ID,
			"vec_score", c.Score,
			"content_len", len(c.Content),
		)
	}
	return hits
}

// resolveSearchParams 解析检索参数：配置不可用时用默认值兜底。
// topK 取查询参数 → 配置 → 全局默认；检索候选数取 topK * 2，给重排留余量，
// 若配置了 MaxChunks 则以其为候选上限，上限与配置范围一致。
// 纯向量单闸：SimilarityThreshold 必须生效——配置 <=0（关闭过滤）时回退默认阈值，
// 避免"过滤被关闭"导致无关切片泄漏（医疗场景宁可不答也不给低质候选）。
func (s *SearchService) resolveSearchParams(
	ctx context.Context, q rag.SearchQuery,
) (cfg *RAGSearchConfig, topK, candidateK int) {
	cfg, err := s.cfgProv.GetRAGConfig(ctx)
	if err != nil || cfg == nil {
		def := defaultRAGSearchConfig
		cfg = &def
	}
	if cfg.SimilarityThreshold <= 0 {
		cfg.SimilarityThreshold = defaultSimilarityThreshold
	}
	topK = q.TopK
	if topK <= 0 {
		topK = cfg.TopK
	}
	if topK <= 0 {
		topK = constants.DefaultTopK
	}
	candidateK = topK * 2
	if cfg.MaxChunks > 0 && candidateK > cfg.MaxChunks {
		candidateK = cfg.MaxChunks
	}
	if candidateK > topKMax {
		candidateK = topKMax
	}
	return cfg, topK, candidateK
}

// embedQueryVec 生成查询向量。失败时向上返回 error，由调用方决定是否降级。
// Embedding 是唯一不可降级的关键依赖：API 返回空结果同样视为失败（严禁静默降级）。
func (s *SearchService) embedQueryVec(ctx context.Context, query string) ([]float32, error) {
	embeds, err := s.embed.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(embeds) == 0 {
		return nil, fmt.Errorf("embed query: API returned empty embeddings")
	}
	return embeds[0], nil
}

// toRAGChunks 将向量检索命中项转换为 rag.Chunk。
func toRAGChunks(hits []repository.ChunkSearchHit) []rag.Chunk {
	out := make([]rag.Chunk, 0, len(hits))
	for _, h := range hits {
		out = append(out, rag.Chunk{
			ChunkID:      fmt.Sprintf("%d", h.ID),
			ArticleID:    fmt.Sprintf("%d", h.ArticleID),
			ArticleTitle: h.ArticleTitle,
			Content:      h.Content,
			Score:        h.Score,
			VecScore:     h.Score,
		})
	}
	return out
}

// filterBySimilarity 保留向量相似度 Score >= threshold 的命中。
// 纯向量路下 Score 即向量相似度，阈值恒生效，低于阈值一律裁剪。
func filterBySimilarity(hits []repository.ChunkSearchHit, threshold float64) []repository.ChunkSearchHit {
	out := make([]repository.ChunkSearchHit, 0, len(hits))
	for _, h := range hits {
		if h.Score >= threshold {
			out = append(out, h)
		}
	}
	return out
}

// applyRerank 调用 LLM Rerank 对 hits 重排，并按 threshold 过滤低分结果。
// Rerank 失败时降级为原相似度顺序。threshold<=0 时不过滤。
func (s *SearchService) applyRerank(
	ctx context.Context, query string, hits []repository.ChunkSearchHit, topK int, threshold float64,
) []repository.ChunkSearchHit {
	docs := make([]string, len(hits))
	for i, h := range hits {
		docs[i] = h.Content
	}
	results, err := s.rerank.Rerank(ctx, query, docs, topK)
	if err != nil {
		slog.WarnContext(ctx, "wiki: rerank failed, fallback to similarity order", "err", err)
		return hits
	}
	if len(results) == 0 {
		return hits
	}
	out := make([]repository.ChunkSearchHit, 0, len(results))
	for _, r := range results {
		if r.Index < 0 || r.Index >= len(hits) {
			continue
		}
		if threshold > 0 && r.Score < threshold {
			continue
		}
		out = append(out, hits[r.Index])
	}
	if len(out) == 0 && len(hits) > 0 {
		slog.WarnContext(ctx, "wiki: rerank filtered all candidates below threshold, keeping top-1",
			"threshold", threshold, "candidates", len(hits))
		return hits[:1]
	}
	return out
}

// 编译期断言：SearchService 实现 shared 层 rag.KnowledgeSearcher 接口。
var _ rag.KnowledgeSearcher = (*SearchService)(nil)
