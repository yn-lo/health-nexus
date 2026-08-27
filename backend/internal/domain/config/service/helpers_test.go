// helpers 单元测试（API Key 掩码防护 + RAG 配置校验 + 错误翻译）。
// 覆盖 looksLikeMaskedAPIKey / validateRAGConfig / applyRAGPatch /
// isUniqueViolation / translateRepoErr 五个纯函数。
package service

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"health-nexus/internal/domain/config/entity"
	"health-nexus/internal/domain/config/repository"
	"health-nexus/internal/platform/postgres"
	apperrors "health-nexus/internal/shared/errors"
)

// ============================================================================
// looksLikeMaskedAPIKey：检测掩码格式
// MaskAPIKey 输出 s[:3] + "****" + s[len-4:]，长度 ≥ 11，索引 3-6 为 4 个 '*'
// ============================================================================

func TestLooksLikeMaskedAPIKey(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want bool
	}{
		// 正确掩码格式：sk-****cdef（长度 11，索引 3-6 全为 '*'）
		{"正确掩码格式", "sk-****cdef", true},
		{"长掩码格式", "sk-****abcdef", true},
		// 无 **** 段
		{"无掩码-普通key", "sk-1234567890abcdef", false},
		// 长度 < 11
		{"短字符串", "sk-abc", false},
		{"空字符串", "", false},
		{"刚好10字符", "sk-1234567", false},
		// 长度 ≥ 11 但索引 3-6 不是全 '*'
		{"索引3非星号", "sk-1****defg", false}, // 索引 3='1'，长度 12
		{"索引3-4非星号", "sk-ab****defg", false},
		{"索引6非星号", "sk-***abcdef", false}, // 索引 6='a'，长度 12
		// 边界：刚好 11 字符且索引 3-6 全 '*'
		{"边界11字符全星", "sk-****abcd", true}, // 0-2='sk-', 3-6='****', 7-10='abcd'
		// 边界：刚好 11 字符但索引 3-6 不全为 '*'
		{"边界11字符缺一星", "sk-***abcd0", false}, // 索引 6='a'
		// 中文 rune 计数（一个中文算 1 rune）
		{"含中文rune计数_长度不足", "中-****中中中中", false},    // 10 rune < 11 → false
		{"含中文rune计数_索引3-6全星", "中中中****中中中中", true}, // 11 rune，索引 3-6 全为 '*'
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := looksLikeMaskedAPIKey(tc.s)
			if got != tc.want {
				t.Errorf("looksLikeMaskedAPIKey(%q) = %v, want %v", tc.s, got, tc.want)
			}
		})
	}
}

// 单独校验含中文场景的边界行为。
func TestLooksLikeMaskedAPIKey_RuneCount(t *testing.T) {
	// "中中中****中中中中" = 11 个 rune，索引 3-6 为 "****"
	s := "中中中****中中中中"
	if got := looksLikeMaskedAPIKey(s); !got {
		t.Errorf("11 rune 含中文且索引 3-6 为 **** 期望 true，实际 %v", got)
	}
	// "中中中****中中中" = 10 rune < 11 → false
	short := "中中中****中中中"
	if got := looksLikeMaskedAPIKey(short); got {
		t.Errorf("10 rune 含中文期望 false（长度不足），实际 %v", got)
	}
}

// ============================================================================
// validateRAGConfig：RAG 参数范围校验
// ============================================================================

func TestValidateRAGConfig(t *testing.T) {
	t.Run("空请求_通过", func(t *testing.T) {
		err := validateRAGConfig(UpdateRAGConfigRequest{})
		if err != nil {
			t.Errorf("期望 nil，实际 %v", err)
		}
	})

	t.Run("全字段合法_通过", func(t *testing.T) {
		req := UpdateRAGConfigRequest{
			ChunkSize:           intPtr(500),
			ChunkOverlap:        intPtr(50),
			MaxChunks:           intPtr(10),
			TopK:                intPtr(5),
			SimilarityThreshold: float64Ptr(0.75),
			RerankEnabled:       boolPtr(true),
			RerankThreshold:     float64Ptr(0.5),
			OODThreshold:        float64Ptr(0.3),
		}
		err := validateRAGConfig(req)
		if err != nil {
			t.Errorf("期望 nil，实际 %v", err)
		}
	})

	// ChunkSize 范围测试
	t.Run("ChunkSize低于下限", func(t *testing.T) {
		req := UpdateRAGConfigRequest{ChunkSize: intPtr(entity.ChunkSizeMin - 1)}
		err := validateRAGConfig(req)
		assertAppErrCode(t, err, "CONFIG_RAG_CHUNK_SIZE_RANGE")
	})
	t.Run("ChunkSize高于上限", func(t *testing.T) {
		req := UpdateRAGConfigRequest{ChunkSize: intPtr(entity.ChunkSizeMax + 1)}
		err := validateRAGConfig(req)
		assertAppErrCode(t, err, "CONFIG_RAG_CHUNK_SIZE_RANGE")
	})
	t.Run("ChunkSize等于下限_通过", func(t *testing.T) {
		req := UpdateRAGConfigRequest{ChunkSize: intPtr(entity.ChunkSizeMin)}
		if err := validateRAGConfig(req); err != nil {
			t.Errorf("期望 nil，实际 %v", err)
		}
	})
	t.Run("ChunkSize等于上限_通过", func(t *testing.T) {
		req := UpdateRAGConfigRequest{ChunkSize: intPtr(entity.ChunkSizeMax)}
		if err := validateRAGConfig(req); err != nil {
			t.Errorf("期望 nil，实际 %v", err)
		}
	})

	// ChunkOverlap 范围测试
	t.Run("ChunkOverlap低于下限", func(t *testing.T) {
		req := UpdateRAGConfigRequest{ChunkOverlap: intPtr(entity.ChunkOverlapMin - 1)}
		err := validateRAGConfig(req)
		assertAppErrCode(t, err, "CONFIG_RAG_CHUNK_OVERLAP_RANGE")
	})
	t.Run("ChunkOverlap高于上限", func(t *testing.T) {
		req := UpdateRAGConfigRequest{ChunkOverlap: intPtr(entity.ChunkOverlapMax + 1)}
		err := validateRAGConfig(req)
		assertAppErrCode(t, err, "CONFIG_RAG_CHUNK_OVERLAP_RANGE")
	})

	// MaxChunks 范围测试
	t.Run("MaxChunks低于下限", func(t *testing.T) {
		req := UpdateRAGConfigRequest{MaxChunks: intPtr(entity.MaxChunksMin - 1)}
		err := validateRAGConfig(req)
		assertAppErrCode(t, err, "CONFIG_RAG_MAX_CHUNKS_RANGE")
	})
	t.Run("MaxChunks高于上限", func(t *testing.T) {
		req := UpdateRAGConfigRequest{MaxChunks: intPtr(entity.MaxChunksMax + 1)}
		err := validateRAGConfig(req)
		assertAppErrCode(t, err, "CONFIG_RAG_MAX_CHUNKS_RANGE")
	})

	// TopK 范围测试
	t.Run("TopK低于下限", func(t *testing.T) {
		req := UpdateRAGConfigRequest{TopK: intPtr(entity.TopKMin - 1)}
		err := validateRAGConfig(req)
		assertAppErrCode(t, err, "CONFIG_RAG_TOP_K_RANGE")
	})
	t.Run("TopK高于上限", func(t *testing.T) {
		req := UpdateRAGConfigRequest{TopK: intPtr(entity.TopKMax + 1)}
		err := validateRAGConfig(req)
		assertAppErrCode(t, err, "CONFIG_RAG_TOP_K_RANGE")
	})

	// SimilarityThreshold 范围测试
	t.Run("SimilarityThreshold低于下限", func(t *testing.T) {
		req := UpdateRAGConfigRequest{SimilarityThreshold: float64Ptr(entity.SimilarityThresholdMin - 0.1)}
		err := validateRAGConfig(req)
		assertAppErrCode(t, err, "CONFIG_RAG_SIMILARITY_RANGE")
	})
	t.Run("SimilarityThreshold高于上限", func(t *testing.T) {
		req := UpdateRAGConfigRequest{SimilarityThreshold: float64Ptr(entity.SimilarityThresholdMax + 0.1)}
		err := validateRAGConfig(req)
		assertAppErrCode(t, err, "CONFIG_RAG_SIMILARITY_RANGE")
	})

	// RerankThreshold 范围测试
	t.Run("RerankThreshold低于下限", func(t *testing.T) {
		req := UpdateRAGConfigRequest{RerankThreshold: float64Ptr(entity.RerankThresholdMin - 0.1)}
		err := validateRAGConfig(req)
		assertAppErrCode(t, err, "CONFIG_RAG_RERANK_THRESHOLD_RANGE")
	})
	t.Run("RerankThreshold高于上限", func(t *testing.T) {
		req := UpdateRAGConfigRequest{RerankThreshold: float64Ptr(entity.RerankThresholdMax + 0.1)}
		err := validateRAGConfig(req)
		assertAppErrCode(t, err, "CONFIG_RAG_RERANK_THRESHOLD_RANGE")
	})

	// OODThreshold 范围测试
	t.Run("OODThreshold低于下限", func(t *testing.T) {
		req := UpdateRAGConfigRequest{OODThreshold: float64Ptr(entity.OODThresholdMin - 0.1)}
		err := validateRAGConfig(req)
		assertAppErrCode(t, err, "CONFIG_RAG_OOD_THRESHOLD_RANGE")
	})
	t.Run("OODThreshold高于上限", func(t *testing.T) {
		req := UpdateRAGConfigRequest{OODThreshold: float64Ptr(entity.OODThresholdMax + 0.1)}
		err := validateRAGConfig(req)
		assertAppErrCode(t, err, "CONFIG_RAG_OOD_THRESHOLD_RANGE")
	})
	t.Run("OODThreshold等于下限_通过", func(t *testing.T) {
		req := UpdateRAGConfigRequest{OODThreshold: float64Ptr(entity.OODThresholdMin)}
		if err := validateRAGConfig(req); err != nil {
			t.Errorf("期望 nil，实际 %v", err)
		}
	})
	t.Run("OODThreshold等于上限_通过", func(t *testing.T) {
		req := UpdateRAGConfigRequest{OODThreshold: float64Ptr(entity.OODThresholdMax)}
		if err := validateRAGConfig(req); err != nil {
			t.Errorf("期望 nil，实际 %v", err)
		}
	})

	// nil 字段不参与校验
	t.Run("nil字段不参与校验", func(t *testing.T) {
		req := UpdateRAGConfigRequest{} // 全部 nil
		if err := validateRAGConfig(req); err != nil {
			t.Errorf("全 nil 字段应通过校验，实际 %v", err)
		}
	})
}

// ============================================================================
// applyRAGPatch：nil 字段保持原值，非 nil 字段覆盖
// ============================================================================

func TestApplyRAGPatch(t *testing.T) {
	t.Run("nil字段_保持原值", func(t *testing.T) {
		original := &entity.RAGConfig{
			ChunkSize:           500,
			ChunkOverlap:        50,
			MaxChunks:           10,
			TopK:                5,
			SimilarityThreshold: 0.75,
			RerankEnabled:       false,
			RerankThreshold:     0.5,
			OODThreshold:        0.3,
		}
		// 空 patch：所有字段 nil
		applyRAGPatch(original, UpdateRAGConfigRequest{})

		if original.ChunkSize != 500 {
			t.Errorf("ChunkSize 期望保持 500，实际 %d", original.ChunkSize)
		}
		if original.ChunkOverlap != 50 {
			t.Errorf("ChunkOverlap 期望保持 50，实际 %d", original.ChunkOverlap)
		}
		if original.MaxChunks != 10 {
			t.Errorf("MaxChunks 期望保持 10，实际 %d", original.MaxChunks)
		}
		if original.TopK != 5 {
			t.Errorf("TopK 期望保持 5，实际 %d", original.TopK)
		}
		if original.SimilarityThreshold != 0.75 {
			t.Errorf("SimilarityThreshold 期望保持 0.75，实际 %v", original.SimilarityThreshold)
		}
		if original.RerankEnabled != false {
			t.Errorf("RerankEnabled 期望保持 false，实际 %v", original.RerankEnabled)
		}
		if original.RerankThreshold != 0.5 {
			t.Errorf("RerankThreshold 期望保持 0.5，实际 %v", original.RerankThreshold)
		}
		if original.OODThreshold != 0.3 {
			t.Errorf("OODThreshold 期望保持 0.3，实际 %v", original.OODThreshold)
		}
	})

	t.Run("非nil字段_覆盖原值", func(t *testing.T) {
		original := &entity.RAGConfig{
			ChunkSize:           500,
			ChunkOverlap:        50,
			MaxChunks:           10,
			TopK:                5,
			SimilarityThreshold: 0.75,
			RerankEnabled:       false,
			RerankThreshold:     0.5,
			OODThreshold:        0.3,
		}
		patch := UpdateRAGConfigRequest{
			ChunkSize:           intPtr(1000),
			ChunkOverlap:        intPtr(100),
			MaxChunks:           intPtr(20),
			TopK:                intPtr(10),
			SimilarityThreshold: float64Ptr(0.9),
			RerankEnabled:       boolPtr(true),
			RerankThreshold:     float64Ptr(0.8),
			OODThreshold:        float64Ptr(0.4),
		}
		applyRAGPatch(original, patch)

		if original.ChunkSize != 1000 {
			t.Errorf("ChunkSize 期望 1000，实际 %d", original.ChunkSize)
		}
		if original.ChunkOverlap != 100 {
			t.Errorf("ChunkOverlap 期望 100，实际 %d", original.ChunkOverlap)
		}
		if original.MaxChunks != 20 {
			t.Errorf("MaxChunks 期望 20，实际 %d", original.MaxChunks)
		}
		if original.TopK != 10 {
			t.Errorf("TopK 期望 10，实际 %d", original.TopK)
		}
		if original.SimilarityThreshold != 0.9 {
			t.Errorf("SimilarityThreshold 期望 0.9，实际 %v", original.SimilarityThreshold)
		}
		if original.RerankEnabled != true {
			t.Errorf("RerankEnabled 期望 true，实际 %v", original.RerankEnabled)
		}
		if original.RerankThreshold != 0.8 {
			t.Errorf("RerankThreshold 期望 0.8，实际 %v", original.RerankThreshold)
		}
		if original.OODThreshold != 0.4 {
			t.Errorf("OODThreshold 期望 0.4，实际 %v", original.OODThreshold)
		}
	})

	t.Run("部分字段覆盖_其余保持", func(t *testing.T) {
		original := &entity.RAGConfig{
			ChunkSize:     500,
			ChunkOverlap:  50,
			TopK:          5,
			RerankEnabled: false,
		}
		// 仅覆盖 ChunkSize 与 RerankEnabled
		patch := UpdateRAGConfigRequest{
			ChunkSize:     intPtr(1500),
			RerankEnabled: boolPtr(true),
		}
		applyRAGPatch(original, patch)

		if original.ChunkSize != 1500 {
			t.Errorf("ChunkSize 期望 1500，实际 %d", original.ChunkSize)
		}
		if original.RerankEnabled != true {
			t.Errorf("RerankEnabled 期望 true，实际 %v", original.RerankEnabled)
		}
		// 未覆盖的字段保持原值
		if original.ChunkOverlap != 50 {
			t.Errorf("ChunkOverlap 期望保持 50，实际 %d", original.ChunkOverlap)
		}
		if original.TopK != 5 {
			t.Errorf("TopK 期望保持 5，实际 %d", original.TopK)
		}
	})
}

// ============================================================================
// isUniqueViolation：PostgreSQL 唯一约束违反检测
// ============================================================================

func TestIsUniqueViolation(t *testing.T) {
	t.Run("PgError_23505_true", func(t *testing.T) {
		err := &pgconn.PgError{Code: "23505"}
		if !postgres.IsUniqueViolation(err) {
			t.Error("期望 true（23505 是唯一约束违反）")
		}
	})

	t.Run("PgError_其他code_false", func(t *testing.T) {
		err := &pgconn.PgError{Code: "42P01"} // undefined_table
		if postgres.IsUniqueViolation(err) {
			t.Error("期望 false（非 23505 错误码）")
		}
	})

	t.Run("nil_false", func(t *testing.T) {
		if postgres.IsUniqueViolation(nil) {
			t.Error("期望 false（nil error）")
		}
	})

	t.Run("非PgError_false", func(t *testing.T) {
		err := errors.New("some other error")
		if postgres.IsUniqueViolation(err) {
			t.Error("期望 false（非 PgError 类型）")
		}
	})

	t.Run("包装的PgError_true", func(t *testing.T) {
		// errors.As 应穿透 wrap
		inner := &pgconn.PgError{Code: "23505"}
		wrapped := errors.Join(errors.New("context"), inner)
		if !postgres.IsUniqueViolation(wrapped) {
			t.Error("期望 true（wrapped PgError 23505）")
		}
	})
}

// ============================================================================
// translateRepoErr：repo 错误翻译为 AppError
// ============================================================================

func TestTranslateRepoErr(t *testing.T) {
	t.Run("ErrNotFound_翻译为NotFound", func(t *testing.T) {
		err := translateRepoErr(repository.ErrNotFound, "CONFIG_NOT_FOUND", "配置不存在")
		appErr, ok := err.(*apperrors.AppError)
		if !ok {
			t.Fatalf("期望 *AppError，实际 %T", err)
		}
		if appErr.HTTP != 404 {
			t.Errorf("期望 HTTP=404，实际 %d", appErr.HTTP)
		}
		if appErr.Code != "CONFIG_NOT_FOUND" {
			t.Errorf("期望 Code=CONFIG_NOT_FOUND，实际 %s", appErr.Code)
		}
		if appErr.Message != "配置不存在" {
			t.Errorf("期望 Message=配置不存在，实际 %s", appErr.Message)
		}
	})

	t.Run("包装的ErrNotFound_翻译为NotFound", func(t *testing.T) {
		// errors.Is 应穿透 wrap
		wrapped := errors.Join(errors.New("context"), repository.ErrNotFound)
		err := translateRepoErr(wrapped, "CONFIG_NOT_FOUND", "配置不存在")
		appErr, ok := err.(*apperrors.AppError)
		if !ok {
			t.Fatalf("期望 *AppError，实际 %T", err)
		}
		if appErr.HTTP != 404 {
			t.Errorf("期望 HTTP=404，实际 %d", appErr.HTTP)
		}
	})

	t.Run("其他错误_原样返回", func(t *testing.T) {
		orig := errors.New("some db error")
		err := translateRepoErr(orig, "CONFIG_NOT_FOUND", "配置不存在")
		// 应返回原 error，不包装为 AppError
		if err != orig {
			t.Errorf("期望原样返回，实际 %v (type %T)", err, err)
		}
	})

	t.Run("nil_返回nil", func(t *testing.T) {
		err := translateRepoErr(nil, "CONFIG_NOT_FOUND", "配置不存在")
		if err != nil {
			t.Errorf("期望 nil，实际 %v", err)
		}
	})
}

// ============================================================================
// 测试辅助函数
// ============================================================================

func intPtr(v int) *int             { return &v }
func float64Ptr(v float64) *float64 { return &v }
func boolPtr(v bool) *bool          { return &v }

// assertAppErrCode 断言 err 是 AppError 且 Code 匹配。
func assertAppErrCode(t *testing.T, err error, wantCode string) {
	t.Helper()
	if err == nil {
		t.Fatalf("期望 error，实际 nil")
	}
	appErr, ok := err.(*apperrors.AppError)
	if !ok {
		t.Fatalf("期望 *AppError，实际 %T: %v", err, err)
	}
	if appErr.Code != wantCode {
		t.Errorf("期望 Code=%s，实际 %s", wantCode, appErr.Code)
	}
	if appErr.HTTP != 422 {
		t.Errorf("期望 HTTP=422（Validation），实际 %d", appErr.HTTP)
	}
}
