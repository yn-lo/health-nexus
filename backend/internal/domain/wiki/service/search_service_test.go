// SearchService 单元测试（REQ-WIKI-012/013/014 RAG 混合检索）。
// 覆盖 rrfFuse / filterBySimilarity / applyRerank 纯函数，以及
// SearchSimilarChunks 端到端流程（mock ChunkSearcher / Embedder / Reranker / RAGConfigProvider）。
package service

import (
	"context"
	"errors"
	"sort"
	"testing"

	"health-nexus/internal/domain/wiki/entity"
	"health-nexus/internal/domain/wiki/repository"
	"health-nexus/internal/platform/llm"
	"health-nexus/internal/shared/rag"
)

// ============================================================================
// 测试辅助：mock 实现
// ============================================================================

// mockChunkSearcher 模拟 ChunkSearcher，按预设返回 vec/bm25 命中或错误。
type mockChunkSearcher struct {
	vecHits  []repository.ChunkSearchHit
	bm25Hits []repository.ChunkSearchHit
	vecErr   error
	bm25Err  error
	// 记录调用参数用于断言
	lastVecTopK      int
	lastBM25TopK     int
	lastVecDepts     []int64
	lastBM25Depts    []int64
	lastVecThreshold float64
}

func (m *mockChunkSearcher) SearchByVector(_ context.Context, _ []float32, topK int, deptIDs []int64, similarityThreshold float64) ([]repository.ChunkSearchHit, error) {
	m.lastVecTopK = topK
	m.lastVecDepts = deptIDs
	m.lastVecThreshold = similarityThreshold
	if m.vecErr != nil {
		return nil, m.vecErr
	}
	return m.vecHits, nil
}

func (m *mockChunkSearcher) SearchByFullText(_ context.Context, _ string, topK int, deptIDs []int64) ([]repository.ChunkSearchHit, error) {
	m.lastBM25TopK = topK
	m.lastBM25Depts = deptIDs
	if m.bm25Err != nil {
		return nil, m.bm25Err
	}
	return m.bm25Hits, nil
}

// mockEmbedder 模拟 llm.Embedder。
type mockEmbedder struct {
	vectors [][]float32
	err     error
	called  bool
}

func (m *mockEmbedder) Embed(_ context.Context, _ []string) ([][]float32, error) {
	m.called = true
	if m.err != nil {
		return nil, m.err
	}
	return m.vectors, nil
}

// mockReranker 模拟 llm.Reranker。
type mockReranker struct {
	results  []llm.RerankResult
	err      error
	called   bool
	lastTopK int
}

func (m *mockReranker) Rerank(_ context.Context, _ string, _ []string, topK int) ([]llm.RerankResult, error) {
	m.called = true
	m.lastTopK = topK
	if m.err != nil {
		return nil, m.err
	}
	return m.results, nil
}

// mockConfigProvider 模拟 RAGConfigProvider。
type mockConfigProvider struct {
	cfg *RAGSearchConfig
	err error
}

func (m *mockConfigProvider) GetRAGConfig(_ context.Context) (*RAGSearchConfig, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.cfg, nil
}

// makeHit 构造一个 ChunkSearchHit（仅必要字段）。
// ChunkSearchHit 嵌入了 entity.ArticleChunk，需通过具名初始化设置其嵌入字段。
func makeHit(id int64, score float64, content, title string) repository.ChunkSearchHit {
	return repository.ChunkSearchHit{
		ArticleChunk: entity.ArticleChunk{
			ID:      id,
			Content: content,
		},
		ArticleTitle: title,
		Score:        score,
	}
}

// ============================================================================
// rrfFuse：Reciprocal Rank Fusion
// ============================================================================

func TestRRFFuse(t *testing.T) {
	t.Run("空输入_空输出", func(t *testing.T) {
		got := rrfFuse(nil, nil)
		if len(got) != 0 {
			t.Errorf("期望空输出，实际 %d", len(got))
		}
	})

	t.Run("仅向量路_每条分数为1/(60+rank)", func(t *testing.T) {
		vecHits := []repository.ChunkSearchHit{
			makeHit(1, 0.9, "c1", "t1"),
			makeHit(2, 0.8, "c2", "t2"),
			makeHit(3, 0.7, "c3", "t3"),
		}
		got := rrfFuse(vecHits, nil)
		if len(got) != 3 {
			t.Fatalf("期望 3 条，实际 %d", len(got))
		}
		// 按 ID 查找并校验分数
		byID := map[int64]fusedHit{}
		for _, h := range got {
			byID[h.ID] = h
		}
		// rank=1 → 1/61, rank=2 → 1/62, rank=3 → 1/63
		wantScores := map[int64]float64{
			1: 1.0 / 61.0,
			2: 1.0 / 62.0,
			3: 1.0 / 63.0,
		}
		for id, want := range wantScores {
			h, ok := byID[id]
			if !ok {
				t.Errorf("期望 ID=%d 存在", id)
				continue
			}
			if h.RRFScore != want {
				t.Errorf("ID=%d RRFScore=%v, want %v", id, h.RRFScore, want)
			}
			// VecScore 应等于原 hit.Score（向量路独占）
			if h.VecScore == 0 {
				t.Errorf("ID=%d VecScore 不应为 0（向量路命中）", id)
			}
		}
	})

	t.Run("仅BM25路_每条分数为1/(60+rank)", func(t *testing.T) {
		bm25Hits := []repository.ChunkSearchHit{
			makeHit(10, 0.5, "c10", "t10"),
			makeHit(20, 0.4, "c20", "t20"),
		}
		got := rrfFuse(nil, bm25Hits)
		if len(got) != 2 {
			t.Fatalf("期望 2 条，实际 %d", len(got))
		}
		byID := map[int64]fusedHit{}
		for _, h := range got {
			byID[h.ID] = h
		}
		// rank=1 → 1/61, rank=2 → 1/62
		if byID[10].RRFScore != 1.0/61.0 {
			t.Errorf("ID=10 RRFScore=%v, want %v", byID[10].RRFScore, 1.0/61.0)
		}
		if byID[20].RRFScore != 1.0/62.0 {
			t.Errorf("ID=20 RRFScore=%v, want %v", byID[20].RRFScore, 1.0/62.0)
		}
		// VecScore 应为 0（BM25 路不提供向量分数）
		if byID[10].VecScore != 0 {
			t.Errorf("ID=10 VecScore 应为 0，实际 %v", byID[10].VecScore)
		}
	})

	t.Run("两路命中同chunk_id_分数累加", func(t *testing.T) {
		// chunk_id=1 在两路都出现
		vecHits := []repository.ChunkSearchHit{
			makeHit(1, 0.9, "c1", "t1"), // vec rank=1
		}
		bm25Hits := []repository.ChunkSearchHit{
			makeHit(1, 0.5, "c1", ""), // bm25 rank=1, title 为空由 vec 补
		}
		got := rrfFuse(vecHits, bm25Hits)
		if len(got) != 1 {
			t.Fatalf("期望 1 条（同 ID 合并），实际 %d", len(got))
		}
		// 期望分数 = 1/61 + 1/61 = 2/61
		want := 2.0 / 61.0
		if got[0].RRFScore != want {
			t.Errorf("累加分数 %v, want %v", got[0].RRFScore, want)
		}
		// VecScore 应为向量路的 0.9
		if got[0].VecScore != 0.9 {
			t.Errorf("VecScore %v, want 0.9", got[0].VecScore)
		}
		// ArticleTitle 应被 vec 路的 "t1" 补上
		if got[0].ArticleTitle != "t1" {
			t.Errorf("ArticleTitle %q, want %q", got[0].ArticleTitle, "t1")
		}
	})

	t.Run("rank越小分数越大", func(t *testing.T) {
		// 构造 5 条仅向量路命中
		hits := make([]repository.ChunkSearchHit, 5)
		for i := 0; i < 5; i++ {
			hits[i] = makeHit(int64(i+1), 0.5, "c", "t")
		}
		got := rrfFuse(hits, nil)
		// 排序验证：按 RRFScore 降序后应与 rank 升序对齐
		sort.SliceStable(got, func(i, j int) bool {
			return got[i].RRFScore > got[j].RRFScore
		})
		for i := 1; i < len(got); i++ {
			if got[i].RRFScore >= got[i-1].RRFScore {
				t.Errorf("排序后第 %d 项分数 %v 不小于前一项 %v", i, got[i].RRFScore, got[i-1].RRFScore)
			}
		}
	})
}

// ============================================================================
// filterBySimilarity：阈值过滤
// ============================================================================

func TestFilterBySimilarity(t *testing.T) {
	t.Run("VecScore为0_过滤（医疗场景不豁免BM25-only）", func(t *testing.T) {
		hits := []fusedHit{
			{VecScore: 0, RRFScore: 0.5},
			{VecScore: 0, RRFScore: 0.3},
		}
		got := filterBySimilarity(hits, 0.75)
		if len(got) != 0 {
			t.Errorf("期望 0 条（医疗场景不豁免 BM25-only），实际 %d", len(got))
		}
	})

	t.Run("VecScore大于等于threshold_保留", func(t *testing.T) {
		hits := []fusedHit{
			{VecScore: 0.75, RRFScore: 0.5}, // 等于阈值
			{VecScore: 0.9, RRFScore: 0.4},  // 大于阈值
		}
		got := filterBySimilarity(hits, 0.75)
		if len(got) != 2 {
			t.Errorf("期望 2 条（≥阈值 保留），实际 %d", len(got))
		}
	})

	t.Run("VecScore小于threshold_过滤（含0）", func(t *testing.T) {
		hits := []fusedHit{
			{VecScore: 0.0, RRFScore: 0.5},  // 0 -> 过滤（医疗场景不豁免 BM25-only）
			{VecScore: 0.74, RRFScore: 0.4}, // <0.75 -> 过滤
			{VecScore: 0.5, RRFScore: 0.3},  // <0.75 -> 过滤
			{VecScore: 0.75, RRFScore: 0.2}, // 等于阈值 -> 保留
		}
		got := filterBySimilarity(hits, 0.75)
		if len(got) != 1 {
			t.Errorf("期望 1 条（仅 >=0.75），实际 %d", len(got))
		}
	})

	t.Run("threshold小于等于0_不过滤保留全部", func(t *testing.T) {
		hits := []fusedHit{
			{VecScore: 0, RRFScore: 0.5},
			{VecScore: 0.3, RRFScore: 0.4},
		}
		got := filterBySimilarity(hits, 0.0)
		if len(got) != 2 {
			t.Errorf("期望 2 条（threshold<=0 不过滤），实际 %d", len(got))
		}
	})

	t.Run("空输入_空输出", func(t *testing.T) {
		got := filterBySimilarity(nil, 0.75)
		if len(got) != 0 {
			t.Errorf("期望空切片，实际 %d", len(got))
		}
	})
}

// ============================================================================
// defaultRAGSearchConfig：兜底默认配置
// ============================================================================

func TestDefaultRAGSearchConfig(t *testing.T) {
	// P1-6：Rerank 默认启用，提升检索质量；rerank 为 nil 时安全降级（见 SearchSimilarChunks 条件）
	if !defaultRAGSearchConfig.RerankEnabled {
		t.Errorf("期望 defaultRAGSearchConfig.RerankEnabled=true（默认启用 rerank），实际 false")
	}
}

// ============================================================================
// applyRerank：调用 LLM Rerank 重排
// ============================================================================

func TestApplyRerank(t *testing.T) {
	// 构造测试用 fused 切片（3 条）
	makeFused := func() []fusedHit {
		return []fusedHit{
			{ChunkSearchHit: makeHit(1, 0.9, "doc1", "t1"), VecScore: 0.9, RRFScore: 0.5},
			{ChunkSearchHit: makeHit(2, 0.8, "doc2", "t2"), VecScore: 0.8, RRFScore: 0.4},
			{ChunkSearchHit: makeHit(3, 0.7, "doc3", "t3"), VecScore: 0.7, RRFScore: 0.3},
		}
	}

	t.Run("Rerank返回正常结果_按其顺序重排", func(t *testing.T) {
		svc := &SearchService{rerank: &mockReranker{results: []llm.RerankResult{
			{Index: 2, Score: 0.95},
			{Index: 0, Score: 0.85},
			{Index: 1, Score: 0.75},
		}}}
		fused := makeFused()
		got := svc.applyRerank(context.Background(), "query", fused, 3, 0.0)
		if len(got) != 3 {
			t.Fatalf("期望 3 条，实际 %d", len(got))
		}
		// 期望顺序：fused[2], fused[0], fused[1]
		if got[0].ID != 3 || got[1].ID != 1 || got[2].ID != 2 {
			t.Errorf("重排顺序错误: got IDs %d,%d,%d; want 3,1,2",
				got[0].ID, got[1].ID, got[2].ID)
		}
	})

	t.Run("Rerank返回error_降级为原fused顺序", func(t *testing.T) {
		svc := &SearchService{rerank: &mockReranker{err: errors.New("llm unavailable")}}
		fused := makeFused()
		got := svc.applyRerank(context.Background(), "query", fused, 3, 0.0)
		if len(got) != 3 {
			t.Fatalf("期望 3 条，实际 %d", len(got))
		}
		// 期望顺序：原 fused[0], fused[1], fused[2]
		if got[0].ID != 1 || got[1].ID != 2 || got[2].ID != 3 {
			t.Errorf("降级顺序错误: got IDs %d,%d,%d; want 1,2,3",
				got[0].ID, got[1].ID, got[2].ID)
		}
	})

	t.Run("Rerank返回空结果_返回原fused", func(t *testing.T) {
		svc := &SearchService{rerank: &mockReranker{results: []llm.RerankResult{}}}
		fused := makeFused()
		got := svc.applyRerank(context.Background(), "query", fused, 3, 0.0)
		if len(got) != 3 {
			t.Fatalf("期望 3 条，实际 %d", len(got))
		}
		// 期望顺序：原 fused
		if got[0].ID != 1 || got[1].ID != 2 || got[2].ID != 3 {
			t.Errorf("空 results 应回退原顺序: got IDs %d,%d,%d; want 1,2,3",
				got[0].ID, got[1].ID, got[2].ID)
		}
	})

	t.Run("Rerank返回nil_返回原fused", func(t *testing.T) {
		svc := &SearchService{rerank: &mockReranker{results: nil}}
		fused := makeFused()
		got := svc.applyRerank(context.Background(), "query", fused, 3, 0.0)
		if len(got) != 3 {
			t.Fatalf("期望 3 条，实际 %d", len(got))
		}
	})

	t.Run("Index越界_跳过", func(t *testing.T) {
		// 输入 results 含 Index=-1, 100, 0 → 仅 0 有效
		svc := &SearchService{rerank: &mockReranker{results: []llm.RerankResult{
			{Index: -1, Score: 0.9},
			{Index: 100, Score: 0.8},
			{Index: 0, Score: 0.7},
		}}}
		fused := makeFused()
		got := svc.applyRerank(context.Background(), "query", fused, 3, 0.0)
		if len(got) != 1 {
			t.Fatalf("期望 1 条（仅 Index=0 有效），实际 %d", len(got))
		}
		if got[0].ID != 1 {
			t.Errorf("期望 ID=1，实际 %d", got[0].ID)
		}
	})

	t.Run("空fused输入_空输出", func(t *testing.T) {
		svc := &SearchService{rerank: &mockReranker{results: []llm.RerankResult{{Index: 0, Score: 0.9}}}}
		got := svc.applyRerank(context.Background(), "query", nil, 3, 0.0)
		if len(got) != 0 {
			t.Errorf("期望空切片，实际 %d", len(got))
		}
	})

	t.Run("RerankThreshold过滤低分结果", func(t *testing.T) {
		// threshold=0.6，应过滤掉 Score<0.6 的结果
		svc := &SearchService{rerank: &mockReranker{results: []llm.RerankResult{
			{Index: 0, Score: 0.9}, // 保留
			{Index: 1, Score: 0.5}, // 过滤（<0.6）
			{Index: 2, Score: 0.7}, // 保留
		}}}
		fused := makeFused()
		got := svc.applyRerank(context.Background(), "query", fused, 3, 0.6)
		if len(got) != 2 {
			t.Fatalf("期望 2 条（过滤 Score<0.6），实际 %d", len(got))
		}
		// 期望顺序：fused[0] (Score=0.9), fused[2] (Score=0.7)
		if got[0].ID != 1 || got[1].ID != 3 {
			t.Errorf("期望 IDs 1,3，实际 %d,%d", got[0].ID, got[1].ID)
		}
	})

	t.Run("RerankThreshold为0_不过滤", func(t *testing.T) {
		svc := &SearchService{rerank: &mockReranker{results: []llm.RerankResult{
			{Index: 0, Score: 0.1},
			{Index: 1, Score: 0.2},
		}}}
		fused := makeFused()
		got := svc.applyRerank(context.Background(), "query", fused, 3, 0.0)
		if len(got) != 2 {
			t.Fatalf("期望 2 条（threshold=0 不过滤），实际 %d", len(got))
		}
	})
}

// ============================================================================
// toRAGChunks：fusedHit -> rag.Chunk，验证 VecScore 正确填充
// ============================================================================

func TestToRAGChunks_VecScore(t *testing.T) {
	t.Run("VecScore正确填充", func(t *testing.T) {
		fused := []fusedHit{
			{ChunkSearchHit: makeHit(1, 0.9, "c1", "t1"), VecScore: 0.9, RRFScore: 0.5},
			{ChunkSearchHit: makeHit(2, 0.8, "c2", "t2"), VecScore: 0.8, RRFScore: 0.4},
		}
		got := toRAGChunks(fused)
		if len(got) != 2 {
			t.Fatalf("期望 2 条，实际 %d", len(got))
		}
		if got[0].VecScore != 0.9 {
			t.Errorf("got[0].VecScore = %v, want 0.9", got[0].VecScore)
		}
		if got[1].VecScore != 0.8 {
			t.Errorf("got[1].VecScore = %v, want 0.8", got[1].VecScore)
		}
		// Score 仍为 RRFScore
		if got[0].Score != 0.5 {
			t.Errorf("got[0].Score = %v, want 0.5 (RRFScore)", got[0].Score)
		}
	})

	t.Run("BM25-only命中_VecScore为0", func(t *testing.T) {
		fused := []fusedHit{
			{ChunkSearchHit: makeHit(3, 0.4, "c3", "t3"), VecScore: 0, RRFScore: 0.3},
		}
		got := toRAGChunks(fused)
		if len(got) != 1 {
			t.Fatalf("期望 1 条，实际 %d", len(got))
		}
		if got[0].VecScore != 0 {
			t.Errorf("BM25-only VecScore 期望 0，实际 %v", got[0].VecScore)
		}
	})

	t.Run("空输入_空输出", func(t *testing.T) {
		got := toRAGChunks(nil)
		if len(got) != 0 {
			t.Errorf("期望空切片，实际 %d", len(got))
		}
	})
}

// ============================================================================
// SearchSimilarChunks：端到端流程
// ============================================================================

func TestSearchSimilarChunks(t *testing.T) {
	t.Run("chunks为nil_返回空切片不报错", func(t *testing.T) {
		svc := NewSearchService(nil,
			&mockEmbedder{vectors: [][]float32{{0.1}}},
			&mockReranker{},
			&mockConfigProvider{cfg: &RAGSearchConfig{TopK: 5}})
		got, err := svc.SearchSimilarChunks(context.Background(), rag.SearchQuery{Query: "q"})
		if err != nil {
			t.Errorf("期望 nil error，实际 %v", err)
		}
		if got == nil {
			t.Fatal("期望非 nil 切片")
		}
		if len(got) != 0 {
			t.Errorf("期望空切片，实际 %d", len(got))
		}
	})

	t.Run("embed为nil_返回空切片不报错", func(t *testing.T) {
		svc := NewSearchService(&mockChunkSearcher{}, nil, nil,
			&mockConfigProvider{cfg: &RAGSearchConfig{TopK: 5}})
		got, _ := svc.SearchSimilarChunks(context.Background(), rag.SearchQuery{Query: "q"})
		if len(got) != 0 {
			t.Errorf("期望空切片，实际 %d", len(got))
		}
	})

	t.Run("cfgProv为nil_返回空切片不报错", func(t *testing.T) {
		svc := NewSearchService(&mockChunkSearcher{},
			&mockEmbedder{vectors: [][]float32{{0.1}}},
			nil, nil)
		got, _ := svc.SearchSimilarChunks(context.Background(), rag.SearchQuery{Query: "q"})
		if len(got) != 0 {
			t.Errorf("期望空切片，实际 %d", len(got))
		}
	})

	t.Run("正常路径_vec和bm25命中_topK截断与排序", func(t *testing.T) {
		// vec 提供 3 条命中（id=1,2,3，score 递减）
		// bm25 提供 2 条命中（id=2,4，与 vec id=2 重合）
		// 期望融合后：id=2 分数最高（两路累加），其余按 RRF 排序
		chunks := &mockChunkSearcher{
			vecHits: []repository.ChunkSearchHit{
				makeHit(1, 0.9, "v1", "t1"),
				makeHit(2, 0.8, "v2", "t2"),
				makeHit(3, 0.7, "v3", "t3"),
			},
			bm25Hits: []repository.ChunkSearchHit{
				makeHit(2, 0.5, "v2", ""),
				makeHit(4, 0.4, "v4", "t4"),
			},
		}
		embed := &mockEmbedder{vectors: [][]float32{{0.1, 0.2, 0.3}}}
		cfgProv := &mockConfigProvider{cfg: &RAGSearchConfig{
			TopK:                3,
			SimilarityThreshold: 0.0, // 不做相似度过滤
			RerankEnabled:       false,
		}}
		svc := NewSearchService(chunks, embed, nil, cfgProv)

		got, err := svc.SearchSimilarChunks(context.Background(), rag.SearchQuery{Query: "q", TopK: 3})
		if err != nil {
			t.Fatalf("期望 nil error，实际 %v", err)
		}
		// 期望 4 个候选 chunk（id=1,2,3,4），topK=3 截断为 3
		if len(got) != 3 {
			t.Fatalf("期望 3 条（topK 截断），实际 %d", len(got))
		}
		// 第一条应为 id=2（两路累加分数最高：1/61 + 1/61 = 2/61）
		if got[0].ChunkID != "2" {
			t.Errorf("期望首条 ChunkID=2（RRF 累加最高），实际 %s", got[0].ChunkID)
		}
		// 验证 embed 被调用
		if !embed.called {
			t.Error("期望 embed 被调用")
		}
		// 验证 chunks 两次都被调用
		if chunks.lastVecTopK == 0 {
			t.Error("期望 SearchByVector 被调用")
		}
		if chunks.lastBM25TopK == 0 {
			t.Error("期望 SearchByFullText 被调用")
		}
		// 验证 candidateK = topK * 2 = 6
		if chunks.lastVecTopK != 6 {
			t.Errorf("期望 vec topK=6（topK*2），实际 %d", chunks.lastVecTopK)
		}
		if chunks.lastBM25TopK != 6 {
			t.Errorf("期望 bm25 topK=6（topK*2），实际 %d", chunks.lastBM25TopK)
		}
	})

	t.Run("embed失败_返回error不降级", func(t *testing.T) {
		// embedding 失败时严禁静默降级为 BM25-only，必须向上返回 error。
		// 医疗场景：宁报 503 也不给用户"暂无相关内容"的虚假否定。
		chunks := &mockChunkSearcher{
			vecHits: []repository.ChunkSearchHit{
				makeHit(1, 0.9, "v1", "t1"),
			},
			bm25Hits: []repository.ChunkSearchHit{
				makeHit(10, 0.5, "b10", "t10"),
			},
		}
		embed := &mockEmbedder{err: errors.New("embed service unavailable")}
		cfgProv := &mockConfigProvider{cfg: &RAGSearchConfig{
			TopK:                5,
			SimilarityThreshold: 0.0,
			RerankEnabled:       false,
		}}
		svc := NewSearchService(chunks, embed, nil, cfgProv)

		_, err := svc.SearchSimilarChunks(context.Background(), rag.SearchQuery{Query: "q"})
		if err == nil {
			t.Fatal("期望 non-nil error（embedding 失败严禁降级），实际 nil")
		}
	})

	t.Run("similarity_threshold过滤生效", func(t *testing.T) {
		// vec 提供 2 条命中：score=0.9（保留）、score=0.5（被过滤，因 <0.75）
		// bm25 提供 1 条命中：score=0.4（VecScore=0，医疗场景不豁免 -> 过滤）
		chunks := &mockChunkSearcher{
			vecHits: []repository.ChunkSearchHit{
				makeHit(1, 0.9, "v1", "t1"),
				makeHit(2, 0.5, "v2", "t2"), // VecScore=0.5 < 0.75 -> 过滤
			},
			bm25Hits: []repository.ChunkSearchHit{
				makeHit(3, 0.4, "b3", "t3"), // VecScore=0 -> 过滤（医疗场景不豁免 BM25-only）
			},
		}
		embed := &mockEmbedder{vectors: [][]float32{{0.1}}}
		cfgProv := &mockConfigProvider{cfg: &RAGSearchConfig{
			TopK:                5,
			SimilarityThreshold: 0.75,
			RerankEnabled:       false,
		}}
		svc := NewSearchService(chunks, embed, nil, cfgProv)

		got, _ := svc.SearchSimilarChunks(context.Background(), rag.SearchQuery{Query: "q"})
		// 期望仅保留 id=1（vec=0.9 >= 0.75）；id=2、id=3 均被过滤
		ids := map[string]bool{}
		for _, c := range got {
			ids[c.ChunkID] = true
		}
		if !ids["1"] {
			t.Errorf("期望保留 id=1（VecScore=0.9 >= 0.75），实际结果 %v", ids)
		}
		if ids["3"] {
			t.Errorf("期望过滤 id=3（VecScore=0 BM25-only，医疗场景不豁免），实际结果 %v", ids)
		}
		if ids["2"] {
			t.Errorf("期望过滤 id=2（VecScore=0.5 < 0.75），实际结果 %v", ids)
		}
	})

	t.Run("RerankEnabled=true_调用reranker", func(t *testing.T) {
		chunks := &mockChunkSearcher{
			vecHits: []repository.ChunkSearchHit{
				makeHit(1, 0.9, "v1", "t1"),
				makeHit(2, 0.8, "v2", "t2"),
			},
		}
		embed := &mockEmbedder{vectors: [][]float32{{0.1}}}
		rerank := &mockReranker{results: []llm.RerankResult{
			{Index: 1, Score: 0.9},
			{Index: 0, Score: 0.8},
		}}
		cfgProv := &mockConfigProvider{cfg: &RAGSearchConfig{
			TopK:                5,
			SimilarityThreshold: 0.0,
			RerankEnabled:       true,
		}}
		svc := NewSearchService(chunks, embed, rerank, cfgProv)

		got, _ := svc.SearchSimilarChunks(context.Background(), rag.SearchQuery{Query: "q"})
		if !rerank.called {
			t.Fatal("期望 rerank 被调用")
		}
		if len(got) != 2 {
			t.Fatalf("期望 2 条，实际 %d", len(got))
		}
	})

	t.Run("无任何命中_返回空切片", func(t *testing.T) {
		chunks := &mockChunkSearcher{} // 两个都返回空
		embed := &mockEmbedder{vectors: [][]float32{{0.1}}}
		cfgProv := &mockConfigProvider{cfg: &RAGSearchConfig{TopK: 5, SimilarityThreshold: 0.0}}
		svc := NewSearchService(chunks, embed, nil, cfgProv)

		got, err := svc.SearchSimilarChunks(context.Background(), rag.SearchQuery{Query: "q"})
		if err != nil {
			t.Errorf("期望 nil error，实际 %v", err)
		}
		if got == nil {
			t.Fatal("期望非 nil 切片")
		}
		if len(got) != 0 {
			t.Errorf("期望空切片，实际 %d", len(got))
		}
	})

	t.Run("DeptID传入_传递给SearchByVector和SearchByFullText", func(t *testing.T) {
		chunks := &mockChunkSearcher{}
		embed := &mockEmbedder{vectors: [][]float32{{0.1}}}
		cfgProv := &mockConfigProvider{cfg: &RAGSearchConfig{TopK: 5, SimilarityThreshold: 0.0}}
		svc := NewSearchService(chunks, embed, nil, cfgProv)

		deptID := int64(42)
		_, _ = svc.SearchSimilarChunks(context.Background(), rag.SearchQuery{Query: "q", DeptID: &deptID})
		if len(chunks.lastVecDepts) != 1 || chunks.lastVecDepts[0] != 42 {
			t.Errorf("期望 vec depts=[42]，实际 %v", chunks.lastVecDepts)
		}
		if len(chunks.lastBM25Depts) != 1 || chunks.lastBM25Depts[0] != 42 {
			t.Errorf("期望 bm25 depts=[42]，实际 %v", chunks.lastBM25Depts)
		}
	})

	t.Run("cfgProv返回error_用默认配置兜底", func(t *testing.T) {
		// cfgProv 返回 error，应使用 defaultRAGSearchConfig（TopK=5, SimThreshold=0.75）
		chunks := &mockChunkSearcher{
			vecHits: []repository.ChunkSearchHit{
				makeHit(1, 0.9, "v1", "t1"), // 0.9 >= 0.75 → 保留
				makeHit(2, 0.6, "v2", "t2"), // 0.6 < 0.75 → 过滤
			},
		}
		embed := &mockEmbedder{vectors: [][]float32{{0.1}}}
		cfgProv := &mockConfigProvider{err: errors.New("config unavailable")}
		svc := NewSearchService(chunks, embed, nil, cfgProv)

		got, _ := svc.SearchSimilarChunks(context.Background(), rag.SearchQuery{Query: "q"})
		// 默认 SimThreshold=0.75，应仅保留 id=1
		if len(got) != 1 {
			t.Fatalf("期望 1 条（默认阈值过滤），实际 %d", len(got))
		}
		if got[0].ChunkID != "1" {
			t.Errorf("期望 ChunkID=1，实际 %s", got[0].ChunkID)
		}
	})

	t.Run("MaxChunks限制候选上限", func(t *testing.T) {
		// MaxChunks=3 应限制 candidateK，即使 topK*2=10 也只用 3
		chunks := &mockChunkSearcher{
			vecHits: []repository.ChunkSearchHit{
				makeHit(1, 0.9, "v1", "t1"),
				makeHit(2, 0.8, "v2", "t2"),
				makeHit(3, 0.7, "v3", "t3"),
			},
		}
		embed := &mockEmbedder{vectors: [][]float32{{0.1}}}
		cfgProv := &mockConfigProvider{cfg: &RAGSearchConfig{
			TopK:                5,
			SimilarityThreshold: 0.0,
			MaxChunks:           3, // 候选上限
		}}
		svc := NewSearchService(chunks, embed, nil, cfgProv)

		_, _ = svc.SearchSimilarChunks(context.Background(), rag.SearchQuery{Query: "q"})
		// candidateK 应被 MaxChunks 限制为 3，而非 topK*2=10
		if chunks.lastVecTopK != 3 {
			t.Errorf("期望 vec candidateK=3（MaxChunks 限制），实际 %d", chunks.lastVecTopK)
		}
		if chunks.lastBM25TopK != 3 {
			t.Errorf("期望 bm25 candidateK=3（MaxChunks 限制），实际 %d", chunks.lastBM25TopK)
		}
	})

	t.Run("MaxChunks为0_使用topK*2默认", func(t *testing.T) {
		// MaxChunks=0 或未设置时，candidateK 应为 topK*2
		chunks := &mockChunkSearcher{}
		embed := &mockEmbedder{vectors: [][]float32{{0.1}}}
		cfgProv := &mockConfigProvider{cfg: &RAGSearchConfig{
			TopK:                5,
			SimilarityThreshold: 0.0,
			MaxChunks:           0, // 未设置
		}}
		svc := NewSearchService(chunks, embed, nil, cfgProv)

		_, _ = svc.SearchSimilarChunks(context.Background(), rag.SearchQuery{Query: "q"})
		// candidateK 应为 topK*2=10
		if chunks.lastVecTopK != 10 {
			t.Errorf("期望 vec candidateK=10（topK*2），实际 %d", chunks.lastVecTopK)
		}
	})

	t.Run("SimilarityThreshold传递给SearchByVector", func(t *testing.T) {
		// 配置中的 SimilarityThreshold 应原样传递给 SearchByVector 的 SQL 层阈值过滤
		chunks := &mockChunkSearcher{
			vecHits: []repository.ChunkSearchHit{
				makeHit(1, 0.9, "v1", "t1"),
			},
		}
		embed := &mockEmbedder{vectors: [][]float32{{0.1}}}
		cfgProv := &mockConfigProvider{cfg: &RAGSearchConfig{
			TopK:                5,
			SimilarityThreshold: 0.75,
		}}
		svc := NewSearchService(chunks, embed, nil, cfgProv)

		_, _ = svc.SearchSimilarChunks(context.Background(), rag.SearchQuery{Query: "q"})
		if chunks.lastVecThreshold != 0.75 {
			t.Errorf("期望 SearchByVector 收到 threshold=0.75，实际 %v", chunks.lastVecThreshold)
		}
	})
}
