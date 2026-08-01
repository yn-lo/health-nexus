package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"health-nexus/internal/domain/config/entity"
	"health-nexus/internal/domain/config/repository"
	apperrors "health-nexus/internal/shared/errors"
)

// ============ RAG Config ============

// GetRAGConfig 获取 RAG 配置。缺失时自愈重建默认行（REQ-NFR-016 降级 + D-LOW-07 自愈）。
// FIX-6: 单例配置走 cache-aside——先查 Redis，miss 回源 DB 并回填（TTL 5min）。
// Redis 不可用或反序列化失败时静默回源 DB（ponytail：缓存降级不影响正确性）。
func (s *ConfigService) GetRAGConfig(ctx context.Context) (*RAGConfigResponse, error) {
	if s.redis != nil {
		if data, err := s.redis.Get(ctx, cacheKeyRAGConfig).Bytes(); err == nil {
			var c entity.RAGConfig
			if json.Unmarshal(data, &c) == nil {
				resp := toRAGConfigResponse(&c)
				return &resp, nil
			}
		}
	}
	c, err := s.ragConfigRepo.Get(ctx)
	if err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("get rag_config: %w", err)
		}
		// 自愈机制：误删后首次 Get 自动重建默认 RAGConfig 行（id=1，CHECK 约束保证仅此一行）。
		// ponytail: 仅 Upsert 默认行，不二次 Get——ON CONFLICT 幂等且 RETURNING updated_at 已回填字段，简化。
		// 上限：并发 Get 竞态下可能多次 Upsert（幂等无副作用）； Upsert 自身失败则向上抛错（不静默，避免掩盖 DB 故障）。
		def := entity.DefaultRAGConfig
		if uErr := s.ragConfigRepo.Upsert(ctx, &def); uErr != nil {
			return nil, fmt.Errorf("self-heal rag_config: %w", uErr)
		}
		c = &def
	}
	if s.redis != nil {
		if data, mErr := json.Marshal(c); mErr == nil {
			s.redis.Set(ctx, cacheKeyRAGConfig, data, cacheTTL)
		}
	}
	resp := toRAGConfigResponse(c)
	return &resp, nil
}

// UpdateRAGConfig 更新 RAG 配置。范围校验失败返回 422（REQ-CONFIG-007）。
// FIX-6: 更新成功后失效 Redis 缓存。
func (s *ConfigService) UpdateRAGConfig(ctx context.Context, req UpdateRAGConfigRequest) (*RAGConfigResponse, error) {
	if err := validateRAGConfig(req); err != nil {
		return nil, err
	}
	existing, err := s.ragConfigRepo.Get(ctx)
	if err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("get rag_config: %w", err)
		}
		def := entity.DefaultRAGConfig
		existing = &def
	}
	applyRAGPatch(existing, req)
	// Medium 2: 合并 patch 后跨字段校验——chunk_overlap 必须 < chunk_size。
	// 仅在请求显式传入两个字段时无法判断单边更新，需在合并后用最终值校验。
	if existing.ChunkOverlap >= existing.ChunkSize {
		return nil, apperrors.Validation("CONFIG_RAG_OVERLAP_TOO_LARGE", "chunk_overlap 必须小于 chunk_size")
	}
	if err := s.ragConfigRepo.Upsert(ctx, existing); err != nil {
		return nil, fmt.Errorf("upsert rag_config: %w", err)
	}
	s.invalidateCache(ctx, cacheKeyRAGConfig)
	s.audit(ctx, entity.AuditActionUpdate, entity.AuditEntityRAGConfig, nil)
	resp := toRAGConfigResponse(existing)
	return &resp, nil
}
