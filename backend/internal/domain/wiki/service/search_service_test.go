// SearchService 单元测试（REQ-WIKI-012/013/014 纯向量检索）。
// 覆盖 filterBySimilarity / applyRerank 纯函数，以及
// SearchSimilarChunks 端到端流程（mock ChunkSearcher / Embedder / Reranker / RAGConfigProvider）。
package service

import (
	"context"
	"errors"
	"testing"

	"health-nexus/internal/domain/wiki/entity"
	"health-nexus/internal/domain/wiki/repository"
	"health-nexus/internal/platform/llm"
	"health-nexus/internal/shared/rag"
)

// ============================================================================
// 测试辅助：mock 实现
// ============================================================================

// mockChunkSearcher 模拟 ChunkSearcher，按预设返回向量命中或错误。
type mockChunkSearcher struct {
	vecHits []repository.ChunkSearchHit
	vecErr  error
	// 记录调用参数用于断言
	lastVecTopK      int
	lastVecDepts     []int64
	lastVecThreshold float64
}

func (m *mockChunkSearcher) SearchByVector(_ context.Context, _ []float32, topK int, deptIDs []int64, similarityThreshold float64) ([]repository.ChunkSearchHit, error) {
	m.lastVecTopK = topK
	m.lastVecDepts = deptsCopy(deptIDs)
	m.lastVecThreshold = similarityThreshold
	if m.vecErr != nil {
		return nil, m.vecErr
	}
	return m.vecHits, nil
}

// deptsCopy 复制 dept 切片，避免断言受调用方后续改动影响。
func deptsCopy(in []int64) []int64 {
	if in == nil {
		return nil
	}
	out := make([]int64, len(in))
	copy(out, in)
	return out
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
// filterBySimilarity：阈值过滤
// ============================================================================

func TestFilterBySimilarity(t *testing.T) {
	t.Run("Score大于等于threshold_保留", func(t *testing.T) {
		hits := []repository.ChunkSearchHit{
			makeHit(1, 0.75, "c1", "t1"), // 等于阈值
			makeHit(2, 0.9, "c2", "t2"),  // 大于阈值
		}
		got := filterBySimilarity(hits, 0.75)
		if len(got) != 2 {
			t.Errorf("期望 2 条（≥阈值 保留），实际 %d", len(got))
		}
	})

	t.Run("Score小于threshold_过滤", func(t *testing.T) {
		hits := []repository.ChunkSearchHit{
			makeHit(1, 0.74, "c1", "t1"), // <0.75 -> 过滤
			makeHit(2, 0.5, "c2", "t2"),  // <0.75 -> 过滤
			makeHit(3, 0.75, "c3", "t3"), // 等于阈值 -> 保留
		}
		got := filterBySimilarity(hits, 0.75)
		if len(got) != 1 {
			t.Errorf("期望 1 条（仅 >=0.75），实际 %d", len(got))
		}
		if got[0].ID != 3 {
			t.Errorf("期望保留 id=3，实际 %d", got[0].ID)
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
	if defaultRAGSearchConfig.SimilarityThreshold != 0.75 {
		t.Errorf("期望默认 SimilarityThreshold=0.75，实际 %v", defaultRAGSearchConfig.SimilarityThreshold)
	}
}

// ============================================================================
// applyRerank：调用 LLM Rerank 重排
// ============================================================================

// makeHits 构造测试用 3 条命中。
func makeHits() []repository.ChunkSearchHit {
	return []repository.ChunkSearchHit{
		makeHit(1, 0.9, "doc1", "t1"),
		makeHit(2, 0.8, "doc2", "t2"),
		makeHit(3, 0.7, "doc3", "t3"),
	}
}

func TestApplyRerank(t *testing.T) {
	t.Run("Rerank返回正常结果_按其顺序重排", func(t *testing.T) {
		svc := &SearchService{rerank: &mockReranker{results: []llm.RerankResult{
			{Index: 2, Score: 0.95},
			{Index: 0, Score: 0.85},
			{Index: 1, Score: 0.75},
		}}}
		hits := makeHits()
		got := svc.applyRerank(context.Background(), "query", hits, 3, 0.0)
		if len(got) != 3 {
			t.Fatalf("期望 3 条，实际 %d", len(got))
		}
		// 期望顺序：hits[2], hits[0], hits[1]
		if got[0].ID != 3 || got[1].ID != 1 || got[2].ID != 2 {
			t.Errorf("重排顺序错误: got IDs %d,%d,%d; want 3,1,2",
				got[0].ID, got[1].ID, got[2].ID)
		}
	})

	t.Run("Rerank返回error_降级为原顺序", func(t *testing.T) {
		svc := &SearchService{rerank: &mockReranker{err: errors.New("llm unavailable")}}
		hits := makeHits()
		got := svc.applyRerank(context.Background(), "query", hits, 3, 0.0)
		if len(got) != 3 {
			t.Fatalf("期望 3 条，实际 %d", len(got))
		}
		// 期望顺序：原 hits[0], hits[1], hits[2]
		if got[0].ID != 1 || got[1].ID != 2 || got[2].ID != 3 {
			t.Errorf("降级顺序错误: got IDs %d,%d,%d; want 1,2,3",
				got[0].ID, got[1].ID, got[2].ID)
		}
	})

	t.Run("Rerank返回空结果_返回原hits", func(t *testing.T) {
		svc := &SearchService{rerank: &mockReranker{results: []llm.RerankResult{}}}
		hits := makeHits()
		got := svc.applyRerank(context.Background(), "query", hits, 3, 0.0)
		if len(got) != 3 {
			t.Fatalf("期望 3 条，实际 %d", len(got))
		}
		if got[0].ID != 1 {
			t.Errorf("空 results 应回退原顺序，实际 ID=%d", got[0].ID)
		}
	})

	t.Run("Index越界_跳过", func(t *testing.T) {
		// 输入 results 含 Index=-1, 100, 0 → 仅 0 有效
		svc := &SearchService{rerank: &mockReranker{results: []llm.RerankResult{
			{Index: -1, Score: 0.9},
			{Index: 100, Score: 0.8},
			{Index: 0, Score: 0.7},
		}}}
		hits := makeHits()
		got := svc.applyRerank(context.Background(), "query", hits, 3, 0.0)
		if len(got) != 1 {
			t.Fatalf("期望 1 条（仅 Index=0 有效），实际 %d", len(got))
		}
		if got[0].ID != 1 {
			t.Errorf("期望 ID=1，实际 %d", got[0].ID)
		}
	})

	t.Run("空输入_空输出", func(t *testing.T) {
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
		hits := makeHits()
		got := svc.applyRerank(context.Background(), "query", hits, 3, 0.6)
		if len(got) != 2 {
			t.Fatalf("期望 2 条（过滤 Score<0.6），实际 %d", len(got))
		}
		// 期望顺序：hits[0] (Score=0.9), hits[2] (Score=0.7)
		if got[0].ID != 1 || got[1].ID != 3 {
			t.Errorf("期望 IDs 1,3，实际 %d,%d", got[0].ID, got[1].ID)
		}
	})
}

// ============================================================================
// toRAGChunks：ChunkSearchHit -> rag.Chunk，验证 Score/VecScore 填充
// ============================================================================

func TestToRAGChunks(t *testing.T) {
	t.Run("Score与VecScore均为向量相似度", func(t *testing.T) {
		hits := []repository.ChunkSearchHit{
			makeHit(1, 0.9, "c1", "t1"),
			makeHit(2, 0.8, "c2", "t2"),
		}
		got := toRAGChunks(hits)
		if len(got) != 2 {
			t.Fatalf("期望 2 条，实际 %d", len(got))
		}
		if got[0].Score != 0.9 || got[0].VecScore != 0.9 {
			t.Errorf("got[0].Score=%v VecScore=%v, want 0.9 / 0.9", got[0].Score, got[0].VecScore)
		}
		if got[1].Score != 0.8 || got[1].VecScore != 0.8 {
			t.Errorf("got[1].Score=%v VecScore=%v, want 0.8 / 0.8", got[1].Score, got[1].VecScore)
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
// SearchSimilarChunks：端到端流程（纯向量）
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

	t.Run("正常路径_阈值过滤与topK截断", func(t *testing.T) {
		// vec 提供 4 条命中（score 递减混杂），topK=3 → 过滤后截断为 3 且按相似度降序
		chunks := &mockChunkSearcher{
			vecHits: []repository.ChunkSearchHit{
				makeHit(1, 0.95, "c1", "t1"),
				makeHit(2, 0.80, "c2", "t2"),
				makeHit(3, 0.85, "c3", "t3"),
				makeHit(4, 0.76, "c4", "t4"),
			},
		}
		embed := &mockEmbedder{vectors: [][]float32{{0.1, 0.2, 0.3}}}
		cfgProv := &mockConfigProvider{cfg: &RAGSearchConfig{
			TopK:                3,
			SimilarityThreshold: 0.75,
			RerankEnabled:       false,
		}}
		svc := NewSearchService(chunks, embed, nil, cfgProv)

		got, err := svc.SearchSimilarChunks(context.Background(), rag.SearchQuery{Query: "q"})
		if err != nil {
			t.Fatalf("期望 nil error，实际 %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("期望 3 条（topK 截断），实际 %d", len(got))
		}
		// 期望顺序按相似度降序：id=1(0.95), id=3(0.85), id=2(0.80)
		if got[0].ChunkID != "1" || got[1].ChunkID != "3" || got[2].ChunkID != "2" {
			t.Errorf("排序顺序错误: IDs %s,%s,%s; want 1,3,2",
				got[0].ChunkID, got[1].ChunkID, got[2].ChunkID)
		}
		if !embed.called {
			t.Error("期望 embed 被调用")
		}
		if chunks.lastVecTopK != 6 {
			t.Errorf("期望 vec candidateK=6（topK*2），实际 %d", chunks.lastVecTopK)
		}
	})

	t.Run("embed失败_返回error不降级", func(t *testing.T) {
		// embedding 失败时严禁静默降级，必须向上返回 error（宁报 503 也不给虚假否定）。
		chunks := &mockChunkSearcher{vecHits: []repository.ChunkSearchHit{makeHit(1, 0.9, "c1", "t1")}}
		embed := &mockEmbedder{err: errors.New("embed service unavailable")}
		cfgProv := &mockConfigProvider{cfg: &RAGSearchConfig{TopK: 5, RerankEnabled: false}}
		svc := NewSearchService(chunks, embed, nil, cfgProv)

		_, err := svc.SearchSimilarChunks(context.Background(), rag.SearchQuery{Query: "q"})
		if err == nil {
			t.Fatal("期望 non-nil error（embedding 失败严禁降级），实际 nil")
		}
	})

	t.Run("similarity_threshold过滤生效", func(t *testing.T) {
		// vec 提供 3 条命中：0.9（保留）、0.6（<0.75 过滤）、0.74（<0.75 过滤）
		chunks := &mockChunkSearcher{
			vecHits: []repository.ChunkSearchHit{
				makeHit(1, 0.9, "v1", "t1"),
				makeHit(2, 0.6, "v2", "t2"),
				makeHit(3, 0.74, "v3", "t3"),
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
		if len(got) != 1 {
			t.Fatalf("期望 1 条（仅 VecScore=0.9 >= 0.75），实际 %d", len(got))
		}
		if got[0].ChunkID != "1" {
			t.Errorf("期望保留 id=1（VecScore=0.9 >= 0.75），实际 %s", got[0].ChunkID)
		}
	})

	t.Run("配置阈值为0_回退默认阈值仍过滤", func(t *testing.T) {
		// 纯向量单闸：SimilarityThreshold=0（关闭过滤）时回退默认 0.75，低分命中仍被裁剪。
		chunks := &mockChunkSearcher{
			vecHits: []repository.ChunkSearchHit{
				makeHit(1, 0.9, "v1", "t1"),  // >= 0.75 -> 保留
				makeHit(2, 0.5, "v2", "t2"),  // < 0.75 -> 过滤
				makeHit(3, 0.74, "v3", "t3"), // < 0.75 -> 过滤
			},
		}
		embed := &mockEmbedder{vectors: [][]float32{{0.1}}}
		cfgProv := &mockConfigProvider{cfg: &RAGSearchConfig{
			TopK:                5,
			SimilarityThreshold: 0.0, // 关闭过滤
			RerankEnabled:       false,
		}}
		svc := NewSearchService(chunks, embed, nil, cfgProv)

		got, _ := svc.SearchSimilarChunks(context.Background(), rag.SearchQuery{Query: "q"})
		if len(got) != 1 {
			t.Fatalf("期望 1 条（阈值 0 回退默认 0.75），实际 %d", len(got))
		}
		if got[0].ChunkID != "1" {
			t.Errorf("期望保留 id=1，实际 %s", got[0].ChunkID)
		}
		if chunks.lastVecThreshold != 0.75 {
			t.Errorf("期望 SearchByVector 收到回退阈值 0.75，实际 %v", chunks.lastVecThreshold)
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
			SimilarityThreshold: 0.75,
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
		chunks := &mockChunkSearcher{}
		embed := &mockEmbedder{vectors: [][]float32{{0.1}}}
		cfgProv := &mockConfigProvider{cfg: &RAGSearchConfig{TopK: 5, RerankEnabled: false}}
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

	t.Run("向量检索失败_返回空切片不报错", func(t *testing.T) {
		// 纯向量路失败视为无命中，chat 域据此拒答，不静默回退。
		chunks := &mockChunkSearcher{vecErr: errors.New("vector search down")}
		embed := &mockEmbedder{vectors: [][]float32{{0.1}}}
		cfgProv := &mockConfigProvider{cfg: &RAGSearchConfig{TopK: 5, RerankEnabled: false}}
		svc := NewSearchService(chunks, embed, nil, cfgProv)

		got, err := svc.SearchSimilarChunks(context.Background(), rag.SearchQuery{Query: "q"})
		if err != nil {
			t.Errorf("期望 nil error（向量失败降级空），实际 %v", err)
		}
		if got == nil || len(got) != 0 {
			t.Errorf("期望空切片，实际 %v", got)
		}
	})

	t.Run("DeptID传入_传递给SearchByVector", func(t *testing.T) {
		chunks := &mockChunkSearcher{}
		embed := &mockEmbedder{vectors: [][]float32{{0.1}}}
		cfgProv := &mockConfigProvider{cfg: &RAGSearchConfig{TopK: 5, RerankEnabled: false}}
		svc := NewSearchService(chunks, embed, nil, cfgProv)

		deptID := int64(42)
		_, _ = svc.SearchSimilarChunks(context.Background(), rag.SearchQuery{Query: "q", DeptID: &deptID})
		if len(chunks.lastVecDepts) != 1 || chunks.lastVecDepts[0] != 42 {
			t.Errorf("期望 depts=[42]，实际 %v", chunks.lastVecDepts)
		}
	})

	t.Run("cfgProv返回error_用默认配置兜底", func(t *testing.T) {
		// cfgProv 返回 error，应使用 defaultRAGSearchConfig（SimThreshold=0.75）
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
			SimilarityThreshold: 0.75,
			MaxChunks:           3, // 候选上限
		}}
		svc := NewSearchService(chunks, embed, nil, cfgProv)

		_, _ = svc.SearchSimilarChunks(context.Background(), rag.SearchQuery{Query: "q"})
		// candidateK 应被 MaxChunks 限制为 3，而非 topK*2=10
		if chunks.lastVecTopK != 3 {
			t.Errorf("期望 vec candidateK=3（MaxChunks 限制），实际 %d", chunks.lastVecTopK)
		}
	})

	t.Run("MaxChunks为0_使用topK*2默认", func(t *testing.T) {
		chunks := &mockChunkSearcher{}
		embed := &mockEmbedder{vectors: [][]float32{{0.1}}}
		cfgProv := &mockConfigProvider{cfg: &RAGSearchConfig{
			TopK:                5,
			SimilarityThreshold: 0.75,
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
			vecHits: []repository.ChunkSearchHit{makeHit(1, 0.9, "v1", "t1")},
		}
		embed := &mockEmbedder{vectors: [][]float32{{0.1}}}
		cfgProv := &mockConfigProvider{cfg: &RAGSearchConfig{
			TopK:                5,
			SimilarityThreshold: 0.80,
		}}
		svc := NewSearchService(chunks, embed, nil, cfgProv)

		_, _ = svc.SearchSimilarChunks(context.Background(), rag.SearchQuery{Query: "q"})
		if chunks.lastVecThreshold != 0.80 {
			t.Errorf("期望 SearchByVector 收到 threshold=0.80，实际 %v", chunks.lastVecThreshold)
		}
	})
}
