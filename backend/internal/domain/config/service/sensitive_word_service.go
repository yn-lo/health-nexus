package service

import (
	"context"
	"fmt"
	"slices"

	"health-nexus/internal/domain/config/entity"
	"health-nexus/internal/platform/postgres"
	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/pagination"
)

// ============ Sensitive Word ============

// ListSensitiveWords 列出敏感词，可选 category 过滤，分页。
func (s *ConfigService) ListSensitiveWords(
	ctx context.Context, category string, p pagination.Params,
) ([]SensitiveWordResponse, int64, error) {
	return listCategoryCatalog(ctx, category, sensitiveCategories, s.sensitiveWordRepo.List, toSensitiveWordResponse, p)
}

// CreateSensitiveWord 创建敏感词。
func (s *ConfigService) CreateSensitiveWord(
	ctx context.Context, req CreateSensitiveWordRequest,
) (*SensitiveWordResponse, error) {
	if req.Word == "" {
		return nil, apperrors.Validation("CONFIG_WORD_REQUIRED", "word 不能为空")
	}
	if !slices.Contains(sensitiveCategories, req.Category) {
		return nil, apperrors.Validation("CONFIG_INVALID_CATEGORY", "category 无效")
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	w := &entity.SensitiveWord{Word: req.Word, Category: req.Category, IsActive: isActive}
	if err := s.sensitiveWordRepo.Create(ctx, w); err != nil {
		if postgres.IsUniqueViolation(err) {
			return nil, apperrors.Conflict("CONFIG_SENSITIVE_WORD_DUPLICATE", "同类别下敏感词已存在")
		}
		return nil, fmt.Errorf("create sensitive_word: %w", err)
	}
	s.audit(ctx, entity.AuditActionCreate, entity.AuditEntitySensitiveWord, w.ID, "category", w.Category)
	resp := toSensitiveWordResponse(w)
	return &resp, nil
}

// UpdateSensitiveWord 更新敏感词。nil 字段不更新。
func (s *ConfigService) UpdateSensitiveWord(
	ctx context.Context, id int64, req UpdateSensitiveWordRequest,
) (*SensitiveWordResponse, error) {
	existing, err := s.sensitiveWordRepo.Get(ctx, id)
	if err != nil {
		return nil, translateRepoErr(err, "CONFIG_SENSITIVE_WORD_NOT_FOUND", "敏感词不存在")
	}
	if req.Word != nil {
		existing.Word = *req.Word
	}
	if req.Category != nil {
		if !slices.Contains(sensitiveCategories, *req.Category) {
			return nil, apperrors.Validation("CONFIG_INVALID_CATEGORY", "category 无效")
		}
		existing.Category = *req.Category
	}
	if req.IsActive != nil {
		existing.IsActive = *req.IsActive
	}
	if err := s.sensitiveWordRepo.Update(ctx, existing); err != nil {
		if postgres.IsUniqueViolation(err) {
			return nil, apperrors.Conflict("CONFIG_SENSITIVE_WORD_DUPLICATE", "同类别下敏感词已存在")
		}
		return nil, translateRepoErr(err, "CONFIG_SENSITIVE_WORD_NOT_FOUND", "敏感词不存在")
	}
	s.audit(ctx, entity.AuditActionUpdate, entity.AuditEntitySensitiveWord, existing.ID, "category", existing.Category)
	resp := toSensitiveWordResponse(existing)
	return &resp, nil
}

// DeleteSensitiveWord 按 ID 删除敏感词。
func (s *ConfigService) DeleteSensitiveWord(ctx context.Context, id int64) error {
	if err := s.sensitiveWordRepo.Delete(ctx, id); err != nil {
		return translateRepoErr(err, "CONFIG_SENSITIVE_WORD_NOT_FOUND", "敏感词不存在")
	}
	s.audit(ctx, entity.AuditActionDelete, entity.AuditEntitySensitiveWord, id)
	return nil
}
