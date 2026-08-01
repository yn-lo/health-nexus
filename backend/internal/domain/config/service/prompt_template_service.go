package service

import (
	"context"
	"fmt"
	"slices"

	"health-nexus/internal/domain/config/entity"
	"health-nexus/internal/platform/postgres"
	"health-nexus/internal/shared/constants"
	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/pagination"
)

// ============ Prompt Template ============

// ListPromptTemplates 列出 Prompt 模板，可选 type 和 is_active 过滤，分页。
func (s *ConfigService) ListPromptTemplates(
	ctx context.Context, promptType string, isActive *bool, p pagination.Params,
) ([]PromptTemplateResponse, int64, error) {
	if promptType != "" && !slices.Contains(promptTypes, promptType) {
		return nil, 0, apperrors.Validation("CONFIG_INVALID_PROMPT_TYPE", "type 无效")
	}
	items, total, err := s.promptTemplateRepo.List(ctx, promptType, isActive, p)
	if err != nil {
		return nil, 0, err
	}
	out := make([]PromptTemplateResponse, 0, len(items))
	for _, item := range items {
		out = append(out, toPromptTemplateResponse(item))
	}
	return out, total, nil
}

// CreatePromptTemplate 创建 Prompt 模板。Version 由 repo 自动递增。
// IsActive=true 时同 type + department_id 其他版本自动失活（REQ-CONFIG-009），
// 由 repo 层 CTE 单语句原子完成（FIX-2/FIX-4）。
func (s *ConfigService) CreatePromptTemplate(
	ctx context.Context, req CreatePromptTemplateRequest,
) (*PromptTemplateResponse, error) {
	if !slices.Contains(promptTypes, req.Type) {
		return nil, apperrors.Validation("CONFIG_INVALID_PROMPT_TYPE", "type 无效")
	}
	if req.Content == "" {
		return nil, apperrors.Validation("CONFIG_CONTENT_REQUIRED", "content 不能为空")
	}
	isActive := false
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	t := &entity.PromptTemplate{
		Type:         req.Type,
		Content:      req.Content,
		IsActive:     isActive,
		Description:  req.Description,
		DepartmentID: req.DepartmentID,
	}
	if err := s.promptTemplateRepo.Create(ctx, t); err != nil {
		if postgres.IsUniqueViolation(err) {
			return nil, apperrors.Conflict("CONFIG_PROMPT_VERSION_CONFLICT", "版本冲突，请重试")
		}
		return nil, fmt.Errorf("create prompt_template: %w", err)
	}
	s.audit(ctx, entity.AuditActionCreate, entity.AuditEntityPromptTemplate,
		t.ID, "type", t.Type, "is_active", t.IsActive)
	resp := toPromptTemplateResponse(t)
	return &resp, nil
}

// UpdatePromptTemplate 更新 Prompt 模板的 content 和/或 is_active（契约 §6.6.3）。
// is_active 由 false→true 时同 type+department_id 下其他版本自动失活。
func (s *ConfigService) UpdatePromptTemplate(
	ctx context.Context, id int64, req UpdatePromptTemplateRequest,
) (*PromptTemplateResponse, error) {
	if req.Content == nil && req.IsActive == nil {
		return nil, apperrors.Validation("CONFIG_EMPTY_UPDATE", "至少需要一个字段：content 或 is_active")
	}
	if req.Content != nil && *req.Content == "" {
		return nil, apperrors.Validation("CONFIG_CONTENT_REQUIRED", "content 不能为空")
	}
	t, err := s.promptTemplateRepo.UpdateContentAndActive(ctx, id, req.Content, req.IsActive)
	if err != nil {
		return nil, translateRepoErr(err, "CONFIG_PROMPT_NOT_FOUND", "Prompt 模板不存在")
	}
	s.audit(ctx, entity.AuditActionUpdate, entity.AuditEntityPromptTemplate,
		t.ID, "type", t.Type, "is_active", t.IsActive)
	resp := toPromptTemplateResponse(t)
	return &resp, nil
}

// DeletePromptTemplate 删除 Prompt 模板。即使 is_active=true 也可删除——系统有硬编码兜底。
func (s *ConfigService) DeletePromptTemplate(ctx context.Context, id int64) error {
	if err := s.promptTemplateRepo.Delete(ctx, id); err != nil {
		return translateRepoErr(err, "CONFIG_PROMPT_NOT_FOUND", "Prompt 模板不存在")
	}
	s.audit(ctx, entity.AuditActionDelete, entity.AuditEntityPromptTemplate, id)
	return nil
}

// GetEffectiveSystemPrompt 返回当前生效的系统提示词。
// DB 中有 active system prompt 时返回数据库内容；否则返回硬编码兜底 DefaultSystemPrompt。
func (s *ConfigService) GetEffectiveSystemPrompt(ctx context.Context) (*EffectivePromptResponse, error) {
	isActive := true
	list, _, err := s.promptTemplateRepo.List(ctx, constants.PromptTypeSystem, &isActive,
		pagination.Params{Page: 1, PageSize: 1})
	if err != nil {
		return nil, fmt.Errorf("list prompt templates: %w", err)
	}
	if len(list) > 0 && list[0].DepartmentID == nil {
		// 全局 active system prompt 存在
		return &EffectivePromptResponse{Content: list[0].Content, Source: "database"}, nil
	}
	// 无全局 active system prompt 或只有科室级 → 返回硬编码兜底
	return &EffectivePromptResponse{Content: constants.DefaultSystemPrompt, Source: "default"}, nil
}
