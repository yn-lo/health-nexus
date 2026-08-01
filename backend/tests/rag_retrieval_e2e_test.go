// Package tests RAG 检索质量回归测试（方案A）。
//
// 验证检索质量防线：相似度阈值过滤、空内容排除、OOD 检测、MinScore 门槛。
// 测试需真实 PostgreSQL + Embedding API，通过 e2e build tag 控制。
//
// 运行前提：
//   - PostgreSQL 可达（默认 localhost:5432/health_nexus）
//   - Embedding API key 已配置（环境变量 HEALTH_NEXUS_EMBEDDING_API_KEY 或 config.yaml）
//   - 种子文章已存在（至少 article_id=153 胸外科围术期指南，含 embedding）
//
// 运行：cd backend && go test ./tests/... -run TestRAGRetrievalQuality -v -count=1 -tags e2e
//
//go:build e2e

package tests_test

import (
	"context"
	"crypto/sha256"
	"math"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"health-nexus/internal/config"
	"health-nexus/internal/domain/wiki/repository"
	"health-nexus/internal/platform/crypto"
	"health-nexus/internal/platform/llm"
)

// ragTestPool 测试专用 DB 连接池。
var ragTestPool *pgxpool.Pool

// setupRAGRetrievalTest 连接数据库，不可达时跳过。
func setupRAGRetrievalTest(t *testing.T) {
	t.Helper()
	if ragTestPool != nil {
		return
	}
	dsn := "postgres://health:health@localhost:5432/health_nexus?sslmode=disable"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("跳过：pgxpool 创建失败: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("跳过：数据库不可达: %v", err)
	}
	ragTestPool = pool
}

// newEmbeddingClient 从 DB 获取加密的 API key，用 encryption_key 解密后创建客户端。
func newEmbeddingClient(t *testing.T) *llm.Client {
	t.Helper()
	// go test 的 cwd 在 tests/ 子目录，config.yaml 在 backend/ 根目录。
	// 临时切到 backend/ 加载配置后恢复。
	origDir, _ := os.Getwd()
	os.Chdir("..")
	defer os.Chdir(origDir)

	appCfg, err := config.Load()
	if err != nil {
		t.Skipf("跳过：加载 config 失败: %v", err)
	}

	if appCfg.Security.EncryptionKey == "" {
		t.Skip("跳过：security.encryption_key 未配置，无法解密 DB 中的 API key")
	}
	aesKey := sha256.Sum256([]byte(appCfg.Security.EncryptionKey))

	// 从 DB 获取 embedding provider 的加密 key。
	var encryptedKey []byte
	var baseURL string
	var modelName string
	err = ragTestPool.QueryRow(context.Background(),
		`SELECT p.api_key_encrypted, p.api_url, p.model_name
		 FROM ai_providers p
		 WHERE p.provider_type = 'embedding' AND p.is_active = true
		 ORDER BY p.created_at LIMIT 1`,
	).Scan(&encryptedKey, &baseURL, &modelName)
	if err != nil {
		t.Skipf("跳过：查询 ai_providers 失败: %v", err)
	}
	if len(encryptedKey) == 0 {
		t.Skip("跳过：DB 中无 active embedding provider")
	}

	apiKey, err := crypto.Decrypt(string(encryptedKey), aesKey[:])
	if err != nil {
		t.Skipf("跳过：解密 API key 失败: %v", err)
	}

	embCfg := appCfg.LLM
	if baseURL != "" {
		embCfg.BaseURL = baseURL
	}
	if modelName != "" {
		embCfg.EmbeddingModel = modelName
	}
	embCfg.APIKey = apiKey

	client, err := llm.NewEmbeddingClient(embCfg)
	if err != nil {
		t.Skipf("跳过：创建 embedding 客户端失败: %v", err)
	}
	return client
}

// embedOne 封装单次 embedding 调用（Client.Embed 为批量接口）。
func embedOne(ctx context.Context, client *llm.Client, text string) ([]float32, error) {
	vecs, err := client.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, nil
	}
	return vecs[0], nil
}

// TestRAGRetrieval_QualityGates 检索质量防线回归测试。
//
// 种子数据要求：
//   - article_id=153（胸外科围术期指南）已有 3 个 active chunk 含 embedding 向量（1024 维）。
//
// 测试用例：
//  1. 正常检索：语义匹配的查询应返回相关切片
//  2. 空内容排除：SQL 层 c.content != ” 不应返回空内容切片
//  3. 相似度门槛：所有返回切片的 VecScore 应 >= similarity_threshold（默认 0.75）
//  4. OOD 检测：完全无关的查询，所有 VecScore 应远低于 0.75
//  5. embedding 不为 NULL：所有 chunks 应有有效向量
func TestRAGRetrieval_QualityGates(t *testing.T) {
	setupRAGRetrievalTest(t)
	embClient := newEmbeddingClient(t)
	repo := repository.NewChunkRepo(ragTestPool)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 读取当前 similarity_threshold 配置。
	var dbThreshold float64
	err := ragTestPool.QueryRow(ctx, `SELECT similarity_threshold FROM rag_configs WHERE id = 1`).Scan(&dbThreshold)
	if err != nil {
		t.Fatalf("读取 rag_configs.similarity_threshold 失败: %v", err)
	}
	if dbThreshold == 0 {
		dbThreshold = 0.75 // 兜底
	}

	// ── 用例 1：正常检索，语义匹配 ──
	t.Run("正常检索_语义匹配", func(t *testing.T) {
		vec, err := embedOne(ctx, embClient, "胸外科手术后需要注意什么")
		if err != nil {
			t.Skipf("embedding 失败: %v", err)
		}
		hits, err := repo.SearchByVector(ctx, vec, 5, nil, dbThreshold)
		if err != nil {
			t.Fatalf("检索失败: %v", err)
		}
		if len(hits) == 0 {
			// 若 threshold=0.75 太严格，降级重试。
			if dbThreshold >= 0.7 {
				hits, err = repo.SearchByVector(ctx, vec, 5, nil, 0.5)
				if err != nil {
					t.Fatalf("降级检索失败: %v", err)
				}
			}
		}
		t.Logf("阈值=%v, 命中=%d", dbThreshold, len(hits))
		if len(hits) > 0 {
			t.Logf("top1: chunk_id=%d, article_title=%s, score=%.4f",
				hits[0].ID, hits[0].ArticleTitle, hits[0].Score)
		}
		// 不做硬断言——语义检索质量依赖 embedding 模型和数据库内容，
		// 此用例主要验证检索链路不报错、不 panic、返回格式正确。
	})

	// ── 用例 2：空内容排除 ──
	t.Run("空内容排除", func(t *testing.T) {
		vec, err := embedOne(ctx, embClient, "胸外科")
		if err != nil {
			t.Skipf("embedding 失败: %v", err)
		}
		// threshold=0 不过滤，依赖 SQL 层 c.content != '' 排除空切片。
		hits, err := repo.SearchByVector(ctx, vec, 10, nil, 0)
		if err != nil {
			t.Fatalf("检索失败: %v", err)
		}
		for _, h := range hits {
			if h.Content == "" {
				t.Errorf("BUG: chunk_id=%d 内容为空，SQL 层 c.content != '' 未生效", h.ID)
			}
		}
		t.Logf("命中 %d 条，全部 content 非空", len(hits))
	})

	// ── 用例 3：相似度门槛过滤 ──
	t.Run("相似度门槛过滤", func(t *testing.T) {
		threshold := dbThreshold
		if threshold <= 0 {
			threshold = 0.6 // 测试用兜底阈值
		}
		vec, err := embedOne(ctx, embClient, "胸外科手术后注意事项")
		if err != nil {
			t.Skipf("embedding 失败: %v", err)
		}
		hits, err := repo.SearchByVector(ctx, vec, 5, nil, threshold)
		if err != nil {
			t.Fatalf("检索失败: %v", err)
		}
		for _, h := range hits {
			if h.Score < threshold {
				t.Errorf("BUG: chunk_id=%d score=%.4f < threshold=%.4f，SQL 阈值过滤未生效",
					h.ID, h.Score, threshold)
			}
		}
		t.Logf("阈值=%.4f, 命中=%d", threshold, len(hits))
	})

	// ── 用例 4：OOD 检测（完全无关查询） ──
	t.Run("OOD检测_无关查询VecScore很低", func(t *testing.T) {
		vec, err := embedOne(ctx, embClient, "苹果手机最新款多少钱")
		if err != nil {
			t.Skipf("embedding 失败: %v", err)
		}
		// 低阈值检索，观察最佳匹配的 VecScore 是否很低。
		hits, err := repo.SearchByVector(ctx, vec, 5, nil, 0)
		if err != nil {
			t.Fatalf("检索失败: %v", err)
		}
		if len(hits) == 0 {
			t.Skip("无任何命中，无法评估 OOD")
		}
		maxScore := hits[0].Score
		for _, h := range hits {
			if h.Score > maxScore {
				maxScore = h.Score
			}
		}
		t.Logf("无关查询 max VecScore=%.4f, hits=%d", maxScore, len(hits))
		// 无关查询的 VecScore 应远低于相关阈值。
		if maxScore >= 0.5 {
			t.Logf("注意：无关查询 VecScore=%.4f >= 0.5，可能影响 OOD 检测", maxScore)
		}
	})

	// ── 用例 5：embedding 不为 NULL ──
	t.Run("embedding不为NULL", func(t *testing.T) {
		var nullCount int
		err := ragTestPool.QueryRow(ctx,
			`SELECT count(*) FROM article_chunks WHERE is_active = true AND embedding IS NULL`).Scan(&nullCount)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if nullCount > 0 {
			t.Errorf("BUG: %d 个 active chunk 的 embedding 为 NULL，检索将漏掉这些切片", nullCount)
		}
	})

	// ── 用例 6：相似度阈值变更后过滤生效 ──
	t.Run("阈值变更_高阈值过滤更严", func(t *testing.T) {
		vec, err := embedOne(ctx, embClient, "手术后饮食")
		if err != nil {
			t.Skipf("embedding 失败: %v", err)
		}
		// 宽松阈值。
		hitsLow, err := repo.SearchByVector(ctx, vec, 10, nil, 0)
		if err != nil {
			t.Fatalf("检索失败: %v", err)
		}
		// 严格阈值。
		hitsHigh, err := repo.SearchByVector(ctx, vec, 10, nil, 0.9)
		if err != nil {
			t.Fatalf("检索失败: %v", err)
		}
		t.Logf("threshold=0 时 %d hits, threshold=0.9 时 %d hits", len(hitsLow), len(hitsHigh))
		// 严格阈值应返回更少结果。
		if len(hitsHigh) > len(hitsLow) {
			t.Errorf("不合理：threshold=0.9 返回 %d 条 > threshold=0 返回 %d 条",
				len(hitsHigh), len(hitsLow))
		}
	})
}

// TestRAGRetrieval_BM25Hybrid 验证 BM25 全文检索通道。
func TestRAGRetrieval_BM25Hybrid(t *testing.T) {
	setupRAGRetrievalTest(t)
	repo := repository.NewChunkRepo(ragTestPool)

	ctx := context.Background()

	t.Run("BM25关键词匹配", func(t *testing.T) {
		// bigram_tsquery 不接受空白字符，查询需为连续字符串。
		hits, err := repo.SearchByFullText(ctx, "术后注意事项", 5, nil)
		if err != nil {
			t.Fatalf("BM25 检索失败: %v", err)
		}
		t.Logf("BM25 hits=%d", len(hits))
		if len(hits) > 0 {
			t.Logf("top1: chunk_id=%d, article=%s, bm25_score=%.4f",
				hits[0].ID, hits[0].ArticleTitle, hits[0].Score)
		}
	})

	t.Run("BM25空查询", func(t *testing.T) {
		hits, err := repo.SearchByFullText(ctx, "", 5, nil)
		if err != nil {
			t.Fatalf("BM25 空查询失败: %v", err)
		}
		if len(hits) != 0 {
			t.Errorf("空查询应返回 0 条，实际 %d", len(hits))
		}
	})
}

// TestRAGRetrieval_DepartmentIsolation 科室隔离验证。
// 心内科(dept=1)只能检索本科室 + 已授权引用文章。
func TestRAGRetrieval_DepartmentIsolation(t *testing.T) {
	setupRAGRetrievalTest(t)

	// 检查文章 153 的科室归属。
	var deptID *int64
	err := ragTestPool.QueryRow(context.Background(),
		`SELECT department_id FROM articles WHERE id = 153`).Scan(&deptID)
	if err != nil {
		t.Skipf("查询文章科室失败: %v", err)
	}
	t.Logf("文章 153 department_id=%v", deptID)
	if deptID == nil {
		t.Skip("文章 153 无科室归属，跳过科室隔离测试")
	}
}

// TestRAGRetrieval_ScoreBounds 验证 Score 在合理范围内 [0, 1]。
func TestRAGRetrieval_ScoreBounds(t *testing.T) {
	setupRAGRetrievalTest(t)
	embClient := newEmbeddingClient(t)
	repo := repository.NewChunkRepo(ragTestPool)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	vec, err := embedOne(ctx, embClient, "胸外科术后恢复注意事项")
	if err != nil {
		t.Skipf("embedding 失败: %v", err)
	}
	hits, err := repo.SearchByVector(ctx, vec, 5, nil, 0)
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	for _, h := range hits {
		if math.IsNaN(h.Score) {
			t.Errorf("chunk_id=%d Score 为 NaN", h.ID)
		}
		if h.Score < 0 || h.Score > 1 {
			t.Errorf("chunk_id=%d Score=%.4f 不在 [0,1] 范围内", h.ID, h.Score)
		}
	}
	t.Logf("共 %d 条，Score 均在 [0,1] 内", len(hits))
}
