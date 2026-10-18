package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"health-nexus/internal/domain/config/entity"
	"health-nexus/internal/domain/config/repository"
)

// ============ Brand Config ============

// GetBrandConfig 获取品牌配置。缺失时自愈重建默认行（与 RAGConfig 同策略）。
// 单例配置走 cache-aside——先查 Redis，miss 回源 DB 并回填（TTL 5min）。
// Redis 不可用或反序列化失败时静默回源 DB（ponytail：缓存降级不影响正确性）。
func (s *ConfigService) GetBrandConfig(ctx context.Context) (*BrandConfigResponse, error) {
	if s.redis != nil {
		if data, err := s.redis.Get(ctx, cacheKeyBrandConfig).Bytes(); err == nil {
			var c entity.BrandConfig
			if json.Unmarshal(data, &c) == nil {
				resp := toBrandConfigResponse(&c)
				return &resp, nil
			}
		}
	}
	c, err := s.brandConfigRepo.Get(ctx)
	if err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("get brand_config: %w", err)
		}
		// 自愈：误删后首次 Get 自动重建默认行（id=1，CHECK 约束保证仅此一行）。
		def := entity.DefaultBrandConfig
		if uErr := s.brandConfigRepo.Upsert(ctx, &def); uErr != nil {
			return nil, fmt.Errorf("self-heal brand_config: %w", uErr)
		}
		c = &def
	}
	if s.redis != nil {
		if data, mErr := json.Marshal(c); mErr == nil {
			s.redis.Set(ctx, cacheKeyBrandConfig, data, cacheTTL)
		}
	}
	resp := toBrandConfigResponse(c)
	return &resp, nil
}

// UpdateBrandConfig 更新品牌配置。字段校验失败返回 422。
// 更新成功后失效 Redis 缓存，并写审计日志。
func (s *ConfigService) UpdateBrandConfig(
	ctx context.Context, req UpdateBrandConfigRequest,
) (*BrandConfigResponse, error) {
	if err := validateBrandConfig(req); err != nil {
		return nil, err
	}
	existing, err := s.brandConfigRepo.Get(ctx)
	if err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("get brand_config: %w", err)
		}
		def := entity.DefaultBrandConfig
		existing = &def
	}
	applyBrandPatch(existing, req)
	if err := s.brandConfigRepo.Upsert(ctx, existing); err != nil {
		return nil, fmt.Errorf("upsert brand_config: %w", err)
	}
	s.invalidateCache(ctx, cacheKeyBrandConfig)
	s.audit(ctx, entity.AuditActionUpdate, entity.AuditEntityBrandConfig, nil)
	resp := toBrandConfigResponse(existing)
	return &resp, nil
}
