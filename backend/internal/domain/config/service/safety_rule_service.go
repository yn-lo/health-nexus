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

// ============ Safety Rule ============

// listCategoryCatalog 按 category 过滤 + 分页 + 转换响应的通用列表流程，
// 抽离 ListSafetyRules/ListSensitiveWords 的重复样板（两域目录接口结构一致）。
func listCategoryCatalog[T any, R any](
	ctx context.Context, category string, validCategories []string,
	list func(context.Context, string, pagination.Params) ([]*T, int64, error),
	conv func(*T) R, p pagination.Params,
) (out []R, total int64, err error) {
	if category != "" && !slices.Contains(validCategories, category) {
		return nil, 0, apperrors.Validation("CONFIG_INVALID_CATEGORY", "category 无效")
	}
	var items []*T
	items, total, err = list(ctx, category, p)
	if err != nil {
		return nil, 0, err
	}
	out = make([]R, 0, len(items))
	for _, item := range items {
		out = append(out, conv(item))
	}
	return out, total, nil
}

// ListSafetyRules 列出安全规则，可选 category 过滤，分页。
func (s *ConfigService) ListSafetyRules(
	ctx context.Context, category string, p pagination.Params,
) ([]SafetyRuleResponse, int64, error) {
	return listCategoryCatalog(ctx, category, safetyCategories, s.safetyRuleRepo.List, toSafetyRuleResponse, p)
}

// CreateSafetyRule 创建安全规则。Pattern 必须是合法正则（REQ-CONFIG-004）。
// action=replace 时 replacement 必填（FIX-11）。
func (s *ConfigService) CreateSafetyRule(
	ctx context.Context, req CreateSafetyRuleRequest,
) (*SafetyRuleResponse, error) {
	if err := validateSafetyRuleFields(req.Name, req.Category, req.Pattern, req.Action, req.Replacement); err != nil {
		return nil, err
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	rule := &entity.SafetyRule{
		Name:        req.Name,
		Category:    req.Category,
		Pattern:     req.Pattern,
		Action:      req.Action,
		Replacement: req.Replacement,
		IsActive:    isActive,
		Description: req.Description,
	}
	if err := s.safetyRuleRepo.Create(ctx, rule); err != nil {
		if postgres.IsUniqueViolation(err) {
			return nil, apperrors.Conflict("CONFIG_SAFETY_RULE_DUPLICATE", "安全规则名称已存在")
		}
		return nil, fmt.Errorf("create safety_rule: %w", err)
	}
	s.audit(ctx, entity.AuditActionCreate, entity.AuditEntitySafetyRule,
		rule.ID, "category", rule.Category, "action", rule.Action)
	resp := toSafetyRuleResponse(rule)
	return &resp, nil
}

// UpdateSafetyRule 更新安全规则。nil 字段不更新。
func (s *ConfigService) UpdateSafetyRule(
	ctx context.Context, id int64, req UpdateSafetyRuleRequest,
) (*SafetyRuleResponse, error) {
	existing, err := s.safetyRuleRepo.Get(ctx, id)
	if err != nil {
		return nil, translateRepoErr(err, "CONFIG_SAFETY_RULE_NOT_FOUND", "安全规则不存在")
	}
	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Category != nil {
		if !slices.Contains(safetyCategories, *req.Category) {
			return nil, apperrors.Validation("CONFIG_INVALID_CATEGORY", "category 无效")
		}
		existing.Category = *req.Category
	}
	if req.Pattern != nil {
		existing.Pattern = *req.Pattern
	}
	if req.Action != nil {
		existing.Action = *req.Action
	}
	if req.Replacement != nil {
		existing.Replacement = *req.Replacement
	}
	if req.IsActive != nil {
		existing.IsActive = *req.IsActive
	}
	if req.Description != nil {
		existing.Description = *req.Description
	}
	if err := validateSafetyRuleFields(
		existing.Name, existing.Category, existing.Pattern,
		existing.Action, existing.Replacement,
	); err != nil {
		return nil, err
	}
	if err := s.safetyRuleRepo.Update(ctx, existing); err != nil {
		if postgres.IsUniqueViolation(err) {
			return nil, apperrors.Conflict("CONFIG_SAFETY_RULE_DUPLICATE", "安全规则名称已存在")
		}
		return nil, translateRepoErr(err, "CONFIG_SAFETY_RULE_NOT_FOUND", "安全规则不存在")
	}
	s.audit(ctx, entity.AuditActionUpdate, entity.AuditEntitySafetyRule,
		existing.ID, "category", existing.Category, "action", existing.Action)
	resp := toSafetyRuleResponse(existing)
	return &resp, nil
}

// DeleteSafetyRule 按 ID 删除安全规则。
func (s *ConfigService) DeleteSafetyRule(ctx context.Context, id int64) error {
	if err := s.safetyRuleRepo.Delete(ctx, id); err != nil {
		return translateRepoErr(err, "CONFIG_SAFETY_RULE_NOT_FOUND", "安全规则不存在")
	}
	s.audit(ctx, entity.AuditActionDelete, entity.AuditEntitySafetyRule, id)
	return nil
}
