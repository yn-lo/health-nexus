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

	"golang.org/x/sync/errgroup"
)

// ChunkSearcher 切片检索能力（消费者定义，ISP）。由 repository.ChunkRepo 实现。
// 仅暴露 SearchService 需要的两个检索方法，保持接口最小化。
type ChunkSearcher interface {
	SearchByVector(
		ctx context.Context, embedding []float32, topK int, deptIDs []int64,
		similarityThreshold float64,
	) ([]repository.ChunkSearchHit, error)
	SearchByFullText(ctx context.Context, query string, topK int, deptIDs []int64) ([]repository.ChunkSearchHit, error)
}

// RAGSearchConfig wiki 域消费的 RAG 配置视图（消费者定义，ISP）。
// 由 adapter 层从 config 域 RAGConfig 转换得到，避免 wiki/service 直接 import config/entity（AC-ARCH-02）。
// 同时承载检索参数（TopK/SimilarityThreshold/Rerank*/MaxChunks/OODThreshold）与切片参数（ChunkSize/ChunkOverlap）：
// 前者由 SearchService 消费，后者由 adapter.VectorizeHandler 消费，共用同一 provider 与缓存。
type RAGSearchConfig struct {
	TopK                int
	SimilarityThreshold float64
	RerankEnabled       bool
	RerankThreshold     float64
	MaxChunks           int
	ChunkSize           int
	ChunkOverlap        int
	OODThreshold        float64
}

// RAGConfigProvider RAG 配置提供者（消费者定义，ISP）。由 config 域适配实现。
type RAGConfigProvider interface {
	GetRAGConfig(ctx context.Context) (*RAGSearchConfig, error)
}

// SearchService 知识检索服务（向量 + BM25 混合检索 + 可选 Rerank，REQ-WIKI-014）。
// 实现 shared 层定义的 rag.KnowledgeSearcher 接口（消费者定义，ISP），由 chat 域注入。
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

// rrfK Reciprocal Rank Fusion 的常数 k，标准取 60。rank 从 1 起算。
const rrfK = 60

// 配置兜底默认值（与 config/entity.DefaultRAGConfig 对齐）。
const (
	defaultTopK                = 5
	defaultSimilarityThreshold = 0.75
	defaultRerankThreshold     = 0.5
	defaultMaxChunks           = 10
	defaultChunkSize           = 500
	defaultChunkOverlap        = 50
	defaultOODThreshold        = 0.3
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
	OODThreshold:        defaultOODThreshold,
}

// topKMax 配置范围上限，与 config/entity.TopKMax 对齐。
const topKMax = 50

// SearchSimilarChunks 混合检索：pgvector ANN + ts_rank BM25 → reciprocal rank fusion → 可选 Rerank。
// 步骤：
//  1. 调用 LLM Embedding 生成查询向量
//  2. 并发调用 SearchByVector + SearchByFullText（errgroup）
//  3. RRF (Reciprocal Rank Fusion) 融合两路结果
//  4. similarity_threshold 过滤（基于向量相似度，融合后保留向量最大相似度作为分数）
//  5. 若 RAGConfig.RerankEnabled=true 且结果数 > 1，调用 LLM Rerank 重排
//  6. 截断到 top_k 返回
//
// Embedding 失败时直接向上返回 error（严禁静默降级为 BM25-only，医疗场景宁报 503 也不给虚假否定）。
// 关键依赖（chunks/embed/cfgProv）未注入时返回空切片（启动阶段的降级行为）。
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
	vecHits, bm25Hits := s.searchCandidates(ctx, q, queryVec, candidateK, cfg.SimilarityThreshold)

	// 步骤 3：RRF 融合。两路按 rank 评分：score = 1 / (rrfK + rank)，rank 从 1 起算。
	// 同 chunk_id 在两路都命中时分数累加。
	fused := rrfFuse(vecHits, bm25Hits)
	if len(fused) == 0 {
		return []rag.Chunk{}, nil
	}

	// 步骤 4~7：相似度过滤 → RRF 排序截断 → 可选 Rerank/MMR → 转换。
	fused = s.applyFiltersAndRerank(ctx, q, fused, cfg, topK)
	if len(fused) == 0 {
		return []rag.Chunk{}, nil
	}
	return toRAGChunks(fused), nil
}

// applyFiltersAndRerank 检索后处理：similarity_threshold 过滤 → RRF 排序截断 → 可选 Rerank/MMR。
func (s *SearchService) applyFiltersAndRerank(
	ctx context.Context, q rag.SearchQuery, fused []fusedHit, cfg *RAGSearchConfig, topK int,
) []fusedHit {
	// 步骤 4：similarity_threshold 过滤（基于向量相似度，仅向量路有意义的分数）。
	threshold := cfg.SimilarityThreshold
	if threshold > 0 {
		fused = filterBySimilarity(fused, threshold)
		if len(fused) == 0 {
			return fused
		}
	}

	slog.InfoContext(ctx, "wiki: RAG search detail",
		"candidates", len(fused),
		"after_threshold", len(fused),
		"similarity_threshold", threshold,
		"top_k", topK,
		"rerank_enabled", cfg.RerankEnabled && s.rerank != nil,
	)

	// 按 RRF 分数降序，截断到 topK。
	sort.SliceStable(fused, func(i, j int) bool {
		return fused[i].RRFScore > fused[j].RRFScore
	})
	if len(fused) > topK {
		fused = fused[:topK]
	}

	// 步骤 5：可选 Rerank。
	if cfg.RerankEnabled && s.rerank != nil && len(fused) > 1 {
		fused = s.applyRerank(ctx, q.Query, fused, topK, cfg.RerankThreshold)
	}

	// 步骤 6：逐条记录检索详情。
	for i, c := range fused {
		slog.InfoContext(ctx, "wiki: RAG chunk detail",
			"rank", i+1,
			"chunk_id", c.ID,
			"vec_score", c.VecScore,
			"rrf_score", c.RRFScore,
			"content_len", len(c.Content),
		)
	}
	return fused
}

// resolveSearchParams 解析检索参数：配置不可用时用默认值兜底（与 config service 缺失时一致），
// topK 取查询参数 → 配置 → 全局默认；检索候选数取 topK * 2，给 RRF 融合与 Rerank 留余量，
// 若配置了 MaxChunks 则以其为候选上限（避免大 topK 时检索过多候选），上限与配置范围一致。
func (s *SearchService) resolveSearchParams(
	ctx context.Context, q rag.SearchQuery,
) (cfg *RAGSearchConfig, topK, candidateK int) {
	cfg, err := s.cfgProv.GetRAGConfig(ctx)
	if err != nil || cfg == nil {
		def := defaultRAGSearchConfig
		cfg = &def
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
// Embedding 是唯一不可降级的关键依赖：API 返回空结果同样视为失败（严禁静默降级为 BM25-only）。
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

// searchCandidates 并发调用向量 + 全文检索。单路失败不阻塞另一路（降级为空命中）。
// similarityThreshold 透传给 SearchByVector，在 SQL 层预过滤低相似度候选。
func (s *SearchService) searchCandidates(
	ctx context.Context, q rag.SearchQuery, queryVec []float32, candidateK int, similarityThreshold float64,
) (vecHits, bm25Hits []repository.ChunkSearchHit) {
	var deptIDs []int64
	if q.DeptID != nil {
		deptIDs = []int64{*q.DeptID}
	}
	g, gctx := errgroup.WithContext(ctx)
	if queryVec != nil {
		g.Go(func() error {
			hits, e := s.chunks.SearchByVector(gctx, queryVec, candidateK, deptIDs, similarityThreshold)
			if e != nil {
				slog.WarnContext(gctx, "wiki: vector search failed", "err", e)
				return nil // 单路失败不阻塞另一路
			}
			vecHits = hits
			return nil
		})
	}
	g.Go(func() error {
		hits, e := s.chunks.SearchByFullText(gctx, q.Query, candidateK, deptIDs)
		if e != nil {
			slog.WarnContext(gctx, "wiki: fulltext search failed", "err", e)
			return nil
		}
		bm25Hits = hits
		return nil
	})
	_ = g.Wait() // errgroup 内 goroutine 永不返回非 nil error（单路失败已降级）
	return vecHits, bm25Hits
}

// toRAGChunks 将融合后的命中项转换为 rag.Chunk。
func toRAGChunks(fused []fusedHit) []rag.Chunk {
	out := make([]rag.Chunk, 0, len(fused))
	for _, h := range fused {
		out = append(out, rag.Chunk{
			ChunkID:      fmt.Sprintf("%d", h.ID),
			ArticleID:    fmt.Sprintf("%d", h.ArticleID),
			ArticleTitle: h.ArticleTitle,
			Content:      h.Content,
			Score:        h.RRFScore,
			VecScore:     h.VecScore,
		})
	}
	return out
}

// fusedHit RRF 融合后的命中项。
type fusedHit struct {
	repository.ChunkSearchHit
	VecScore float64 // 向量路原始相似度分数（1 - cosine_distance），仅向量路命中时有意义
	RRFScore float64 // RRF 融合分数
}

// rrfFuse 对向量路与全文路结果做 Reciprocal Rank Fusion。
// 同 chunk_id 在两路都命中时分数累加。rank 从 1 起算，score = 1 / (rrfK + rank)。
func rrfFuse(vecHits, bm25Hits []repository.ChunkSearchHit) []fusedHit {
	idx := make(map[int64]*fusedHit, len(vecHits)+len(bm25Hits))
	add := func(hits []repository.ChunkSearchHit, isVec bool) {
		for i, h := range hits {
			rank := i + 1
			rrf := 1.0 / float64(rrfK+rank)
			f, ok := idx[h.ID]
			if !ok {
				f = &fusedHit{ChunkSearchHit: h}
				if isVec {
					f.VecScore = h.Score
				}
				idx[h.ID] = f
			} else if isVec {
				// 已存在（来自另一路）；保留 VecScore 中较大的向量分数。
				if h.Score > f.VecScore {
					f.VecScore = h.Score
				}
				// 若 ArticleTitle 之前为空，这里补上。
				if f.ArticleTitle == "" && h.ArticleTitle != "" {
					f.ArticleTitle = h.ArticleTitle
				}
			}
			f.RRFScore += rrf
		}
	}
	add(vecHits, true)
	add(bm25Hits, false)
	out := make([]fusedHit, 0, len(idx))
	for _, f := range idx {
		out = append(out, *f)
	}
	return out
}

// filterBySimilarity 保留 VecScore >= threshold 的命中。
// 医疗场景不豁免 VecScore==0 的 BM25-only 命中（避免无向量证据的低质结果混入）。
// threshold <= 0 时不过滤（保留全部）：VecScore 恒 >= 0 >= threshold，条件恒真。
func filterBySimilarity(hits []fusedHit, threshold float64) []fusedHit {
	out := make([]fusedHit, 0, len(hits))
	for _, h := range hits {
		if h.VecScore >= threshold {
			out = append(out, h)
		}
	}
	return out
}

// applyRerank 调用 LLM Rerank 对 fused 重排，并按 threshold 过滤低分结果。
// Rerank 失败时降级为原 RRF 顺序。threshold<=0 时不过滤。
func (s *SearchService) applyRerank(
	ctx context.Context, query string, fused []fusedHit, topK int, threshold float64,
) []fusedHit {
	docs := make([]string, len(fused))
	for i, h := range fused {
		docs[i] = h.Content
	}
	results, err := s.rerank.Rerank(ctx, query, docs, topK)
	if err != nil {
		slog.WarnContext(ctx, "wiki: rerank failed, fallback to RRF order", "err", err)
		return fused
	}
	if len(results) == 0 {
		return fused
	}
	out := make([]fusedHit, 0, len(results))
	for _, r := range results {
		if r.Index < 0 || r.Index >= len(fused) {
			continue
		}
		if threshold > 0 && r.Score < threshold {
			continue
		}
		out = append(out, fused[r.Index])
	}
	if len(out) == 0 && len(fused) > 0 {
		slog.WarnContext(ctx, "wiki: rerank filtered all candidates below threshold, keeping top-1",
			"threshold", threshold, "candidates", len(fused))
		return fused[:1]
	}
	return out
}

// 编译期断言：SearchService 实现 shared 层 rag.KnowledgeSearcher 接口。
var _ rag.KnowledgeSearcher = (*SearchService)(nil)
