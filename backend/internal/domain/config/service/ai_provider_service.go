package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"health-nexus/internal/domain/config/entity"
	"health-nexus/internal/domain/config/repository"
	"health-nexus/internal/platform/crypto"
	"health-nexus/internal/platform/llm"
	"health-nexus/internal/platform/postgres"
	"health-nexus/internal/shared/constants"
	apperrors "health-nexus/internal/shared/errors"
)

// ============ AI Provider ============

// ListAIProviders 列出 AI 提供商，可选 provider_type 和 is_active 过滤。
func (s *ConfigService) ListAIProviders(
	ctx context.Context, providerType string, isActive *bool,
) ([]AIProviderResponse, error) {
	if providerType != "" && !slices.Contains(providerTypes, providerType) {
		return nil, apperrors.Validation("CONFIG_INVALID_PROVIDER_TYPE", "provider_type 无效")
	}
	list, err := s.aiProviderRepo.List(ctx, providerType, isActive)
	if err != nil {
		return nil, err
	}
	out := make([]AIProviderResponse, 0, len(list))
	for _, p := range list {
		out = append(out, toAIProviderResponse(p))
	}
	return out, nil
}

// GetAIProvider 按 ID 获取单个 AI 提供商。
func (s *ConfigService) GetAIProvider(ctx context.Context, id int64) (*AIProviderResponse, error) {
	p, err := s.aiProviderRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	resp := toAIProviderResponse(p)
	return &resp, nil
}

// CreateAIProvider 创建 AI 提供商，API Key 加密存储（REQ-CONFIG-002）。
//
// 方案 C：当 provider_type=embedding 且 dimension 非空时，触发 article_chunks.embedding
// 列维度对齐（ALTER COLUMN TYPE vector(N) + 重建 hnsw 索引）。已有向量化切片时拒绝。
func (s *ConfigService) CreateAIProvider(
	ctx context.Context, req CreateAIProviderRequest,
) (*AIProviderResponse, error) {
	if err := validateAIProviderFields(req.ProviderType, req.Name, req.APIBase, req.ModelName, req.APIKey); err != nil {
		return nil, err
	}
	// 方案 C：embedding provider 必须提供 dimension（向量维度，bge-m3=1024, text-embedding-3-small=1536 等）。
	if req.ProviderType == constants.ProviderTypeEmbedding && (req.Dimension == nil || *req.Dimension <= 0) {
		return nil, apperrors.Validation("CONFIG_EMBEDDING_DIM_REQUIRED",
			"embedding 类型的 provider 必须提供 dimension（向量维度）")
	}
	// 方案 C：embedding 维度变化时对齐 DB 列（DDL）。
	if req.ProviderType == constants.ProviderTypeEmbedding {
		if err := s.alignEmbeddingDimension(ctx, *req.Dimension); err != nil {
			return nil, err
		}
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	params := req.Params
	if params == nil {
		params = map[string]any{}
	}
	encStr, err := crypto.Encrypt(req.APIKey, s.aesKey)
	if err != nil {
		return nil, apperrors.Internal("encrypt api key", err)
	}
	p := &entity.AIProvider{
		Name:            req.Name,
		ProviderType:    req.ProviderType,
		APIURL:          req.APIBase,
		IsFullURL:       req.IsFullURL,
		APIKeyEncrypted: []byte(encStr),
		APIKeyMasked:    MaskAPIKey(req.APIKey),
		ModelName:       req.ModelName,
		Dimension:       req.Dimension,
		Parameters:      params,
		IsActive:        isActive,
	}
	if err := s.aiProviderRepo.Create(ctx, p); err != nil {
		if postgres.IsUniqueViolation(err) {
			return nil, apperrors.Conflict("CONFIG_AI_PROVIDER_DUPLICATE", "AI 提供商名称已存在")
		}
		return nil, fmt.Errorf("create ai_provider: %w", err)
	}
	s.audit(ctx, entity.AuditActionCreate, entity.AuditEntityAIProvider, p.ID,
		"provider_type", p.ProviderType, "dimension", p.Dimension)
	s.notifyLLMReload(ctx)
	resp := toAIProviderResponse(p)
	return &resp, nil
}

// UpdateAIProvider 更新 AI 提供商。nil 字段不更新；APIKey 非空时重新加密。
//
// 方案 C：当 provider_type=embedding 且 dimension 实际变化时，触发 article_chunks.embedding
// 列维度对齐。已有向量化切片时拒绝（409 CONFIG_EMBEDDING_DIM_CHANGE_BLOCKED）。
func (s *ConfigService) UpdateAIProvider(
	ctx context.Context, id int64, req UpdateAIProviderRequest,
) (*AIProviderResponse, error) {
	existing, err := s.aiProviderRepo.Get(ctx, id)
	if err != nil {
		return nil, translateRepoErr(err, "CONFIG_AI_PROVIDER_NOT_FOUND", "AI 提供商不存在")
	}
	dimChanged, err := s.resolveDimensionChange(ctx, existing, req.Dimension)
	if err != nil {
		return nil, err
	}
	if err := s.applyAIProviderPatch(existing, req); err != nil {
		return nil, err
	}
	apiKeyForValidation := existing.APIKeyMasked
	if req.APIKey != nil && !looksLikeMaskedAPIKey(*req.APIKey) {
		apiKeyForValidation = *req.APIKey
	}
	if err := validateAIProviderFields(
		existing.ProviderType, existing.Name, existing.APIURL,
		existing.ModelName, apiKeyForValidation,
	); err != nil {
		return nil, err
	}
	if err := s.aiProviderRepo.Update(ctx, existing); err != nil {
		if postgres.IsUniqueViolation(err) {
			return nil, apperrors.Conflict("CONFIG_AI_PROVIDER_DUPLICATE", "AI 提供商名称已存在")
		}
		return nil, translateRepoErr(err, "CONFIG_AI_PROVIDER_NOT_FOUND", "AI 提供商不存在")
	}
	s.audit(ctx, entity.AuditActionUpdate, entity.AuditEntityAIProvider, existing.ID,
		"provider_type", existing.ProviderType, "dimension_changed", dimChanged)
	s.notifyLLMReload(ctx)
	resp := toAIProviderResponse(existing)
	return &resp, nil
}

func (s *ConfigService) resolveDimensionChange(
	ctx context.Context, existing *entity.AIProvider, reqDim *int,
) (bool, error) {
	if existing.ProviderType != constants.ProviderTypeEmbedding || reqDim == nil {
		return false, nil
	}
	oldDim := 0
	if existing.Dimension != nil {
		oldDim = *existing.Dimension
	}
	if *reqDim == oldDim {
		return false, nil
	}
	if *reqDim <= 0 {
		return false, apperrors.Validation("CONFIG_EMBEDDING_DIM_REQUIRED",
			"embedding 类型的 provider dimension 必须 > 0")
	}
	if err := s.alignEmbeddingDimension(ctx, *reqDim); err != nil {
		return false, err
	}
	return true, nil
}

func (s *ConfigService) applyAIProviderPatch(
	existing *entity.AIProvider, req UpdateAIProviderRequest,
) error {
	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.APIBase != nil {
		existing.APIURL = *req.APIBase
	}
	if req.ModelName != nil {
		existing.ModelName = *req.ModelName
	}
	if req.Dimension != nil {
		existing.Dimension = req.Dimension
	}
	if req.Params != nil {
		existing.Parameters = *req.Params
	}
	if req.IsActive != nil {
		existing.IsActive = *req.IsActive
	}
	if req.IsFullURL != nil {
		existing.IsFullURL = *req.IsFullURL
	}
	if req.APIKey != nil && *req.APIKey != "" && !looksLikeMaskedAPIKey(*req.APIKey) {
		encStr, encErr := crypto.Encrypt(*req.APIKey, s.aesKey)
		if encErr != nil {
			return apperrors.Internal("encrypt api key", encErr)
		}
		existing.APIKeyEncrypted = []byte(encStr)
		existing.APIKeyMasked = MaskAPIKey(*req.APIKey)
	}
	return nil
}

// alignEmbeddingDimension 方案 C 的核心：对齐 article_chunks.embedding 列到 dim 维。
// 已有向量化切片时返回 409（需先 DELETE article_chunks 或归档相关文章后重跑向量化）。
// 维度未变化时静默 no-op。
func (s *ConfigService) alignEmbeddingDimension(ctx context.Context, dim int) error {
	current, err := s.aiProviderRepo.CurrentEmbeddingDimension(ctx)
	if err != nil {
		return fmt.Errorf("get current embedding dimension: %w", err)
	}
	if current == dim {
		// 维度已对齐，无需 DDL。
		return nil
	}
	slog.InfoContext(ctx, "config: aligning embedding dimension",
		"from", current, "to", dim)
	has, err := s.aiProviderRepo.HasVectorizedChunks(ctx)
	if err != nil {
		return fmt.Errorf("check vectorized chunks: %w", err)
	}
	if has {
		return apperrors.Conflict("CONFIG_EMBEDDING_DIM_CHANGE_BLOCKED",
			"已有向量化切片，禁止更改 embedding 维度；请先清空 article_chunks 或重新审核文章触发重切片")
	}
	if err := s.aiProviderRepo.AlignEmbeddingDimension(ctx, dim); err != nil {
		if errors.Is(err, repository.ErrEmbeddingDimChangeBlocked) {
			return apperrors.Conflict("CONFIG_EMBEDDING_DIM_CHANGE_BLOCKED",
				"已有向量化切片，禁止更改 embedding 维度（并发竞态拦截）")
		}
		return apperrors.Internal("align embedding dimension", err)
	}
	return nil
}

// DeleteAIProvider 按 ID 删除 AI 提供商。
func (s *ConfigService) DeleteAIProvider(ctx context.Context, id int64) error {
	if err := s.aiProviderRepo.Delete(ctx, id); err != nil {
		return translateRepoErr(err, "CONFIG_AI_PROVIDER_NOT_FOUND", "AI 提供商不存在")
	}
	s.audit(ctx, entity.AuditActionDelete, entity.AuditEntityAIProvider, id)
	s.notifyLLMReload(ctx)
	return nil
}

const testProviderTimeoutSec = 60

// TestAIProviderResult 连通性测试结果（方案 C：每个 provider 都可测试）。
type TestAIProviderResult struct {
	Success   bool   `json:"success"`
	LatencyMS int64  `json:"latency_ms"`
	Detail    string `json:"detail,omitempty"`
}

// TestAIProvider 调用 provider 一次最小请求验证连通性（POST /api/staff/config/ai-providers/{id}/test）。
// 解密 API Key → 构造临时 llm.Client → 按 provider_type 调 Ping。
// ponytail: 不复用 di 装配的全局 client——测试时要验证 DB 中的最新配置，而非启动时缓存的 client。
// 上限：只验证 API 可达，不验证模型能力匹配；升级路径见 llm.Ping 注释。
func (s *ConfigService) TestAIProvider(ctx context.Context, id int64) (*TestAIProviderResult, error) {
	p, err := s.aiProviderRepo.Get(ctx, id)
	if err != nil {
		return nil, translateRepoErr(err, "CONFIG_AI_PROVIDER_NOT_FOUND", "AI 提供商不存在")
	}
	apiKey, err := crypto.Decrypt(string(p.APIKeyEncrypted), s.aesKey)
	if err != nil {
		return nil, apperrors.Internal("decrypt api key for test", err)
	}
	client := llm.NewClientFromProvider(
		p.ProviderType, p.APIURL, apiKey, p.ModelName, testProviderTimeoutSec*time.Second, p.Parameters, p.IsFullURL,
	)
	start := time.Now()
	pingErr := client.Ping(ctx, p.ProviderType)
	latency := time.Since(start).Milliseconds()

	if pingErr != nil {
		slog.WarnContext(ctx, "config: ai provider test failed",
			"provider_id", id, "provider_type", p.ProviderType, "err", pingErr, "latency_ms", latency)
		return &TestAIProviderResult{
			Success:   false,
			LatencyMS: latency,
			Detail:    pingErr.Error(),
		}, nil
	}
	return &TestAIProviderResult{
		Success:   true,
		LatencyMS: latency,
		Detail:    fmt.Sprintf("%s provider %q 连通正常", p.ProviderType, p.Name),
	}, nil
}
