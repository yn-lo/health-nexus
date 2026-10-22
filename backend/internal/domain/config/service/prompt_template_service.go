package service

import (
	"context"
	"fmt"
	"slices"
	"time"
	"unicode/utf8"

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
// IsActive=true 时同 type 其他版本自动失活（REQ-CONFIG-009）。
// Content 只允许承载可配置人格（护栏由代码独占），故按 rune 上限校验长度。
func (s *ConfigService) CreatePromptTemplate(
	ctx context.Context, req CreatePromptTemplateRequest,
) (*PromptTemplateResponse, error) {
	if !slices.Contains(promptTypes, req.Type) {
		return nil, apperrors.Validation("CONFIG_INVALID_PROMPT_TYPE", "type 无效")
	}
	if err := validatePromptContent(req.Content); err != nil {
		return nil, err
	}
	isActive := false
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	t := &entity.PromptTemplate{
		Type:        req.Type,
		Content:     req.Content,
		IsActive:    isActive,
		Description: req.Description,
	}
	if err := s.promptTemplateRepo.Create(ctx, t); err != nil {
		if postgres.IsUniqueViolation(err) {
			return nil, apperrors.Conflict("CONFIG_PROMPT_VERSION_CONFLICT", "版本号冲突，请重新提交")
		}
		return nil, fmt.Errorf("create prompt_template: %w", err)
	}
	s.invalidatePromptCache()
	s.audit(ctx, entity.AuditActionCreate, entity.AuditEntityPromptTemplate,
		t.ID, "type", t.Type, "is_active", t.IsActive)
	resp := toPromptTemplateResponse(t)
	return &resp, nil
}

// UpdatePromptTemplate 更新 Prompt 模板的 content 和/或 is_active（契约 §6.6.3）。
// is_active 由 false→true 时同 type 下其他版本自动失活。
func (s *ConfigService) UpdatePromptTemplate(
	ctx context.Context, id int64, req UpdatePromptTemplateRequest,
) (*PromptTemplateResponse, error) {
	if req.Content == nil && req.IsActive == nil {
		return nil, apperrors.Validation("CONFIG_EMPTY_UPDATE", "至少需要一个字段：content 或 is_active")
	}
	if req.Content != nil {
		if err := validatePromptContent(*req.Content); err != nil {
			return nil, err
		}
	}
	t, err := s.promptTemplateRepo.UpdateContentAndActive(ctx, id, req.Content, req.IsActive)
	if err != nil {
		return nil, translateRepoErr(err, "CONFIG_PROMPT_NOT_FOUND", "Prompt 模板不存在")
	}
	s.invalidatePromptCache()
	s.audit(ctx, entity.AuditActionUpdate, entity.AuditEntityPromptTemplate,
		t.ID, "type", t.Type, "is_active", t.IsActive)
	resp := toPromptTemplateResponse(t)
	return &resp, nil
}

// DeletePromptTemplate 删除 Prompt 模板（含生效版本）。
// 删除后运行时回落到代码内置默认人格 + 代码护栏，不存在"无提示词"状态，故不禁止删除。
func (s *ConfigService) DeletePromptTemplate(ctx context.Context, id int64) error {
	if err := s.promptTemplateRepo.Delete(ctx, id); err != nil {
		return translateRepoErr(err, "CONFIG_PROMPT_NOT_FOUND", "Prompt 模板不存在")
	}
	s.invalidatePromptCache()
	s.audit(ctx, entity.AuditActionDelete, entity.AuditEntityPromptTemplate, id)
	return nil
}

// validatePromptContent 校验可配置人格声明：非空且在长度上限内。
// 上限防止运营填入整篇规范把生成侧上下文预算吃掉（护栏由代码独占，无需在此重复规则）。
func validatePromptContent(content string) error {
	if content == "" {
		return apperrors.Validation("CONFIG_CONTENT_REQUIRED", "content 不能为空")
	}
	if utf8.RuneCountInString(content) > constants.MaxSystemPromptPersonaRunes {
		return apperrors.Validation("CONFIG_CONTENT_TOO_LONG",
			fmt.Sprintf("content 不能超过 %d 个字", constants.MaxSystemPromptPersonaRunes))
	}
	return nil
}

// EffectiveSystemPrompt 返回当前生效的系统提示词（人格 + 代码护栏）。
// 管理端 GET /prompts/effective 与 chat 域实时调用共用此方法——「管理端看到的」
// 与「患者实际生效的」必须是同一份内容，不允许两套选择逻辑。
//
// 进程内 cache-aside（TTL 同其它单例配置）：系统提示词读取频率远高于变更频率；
// 写入路径（Create/Update/Delete）直接失效本实例缓存，因此管理端改完立刻生效。
// 多实例部署下其他实例仍由 TTL 收敛（与敏感词/安全规则缓存语义一致，不引入通知机制）。
// ponytail: 回源在锁内执行——相位只发生在每 5min 一次的首次请求，简单性优先于吞吐；
// 升级路径：singleflight 解耦锁与回源（当前 QPS 不需要）。
func (s *ConfigService) EffectiveSystemPrompt(ctx context.Context) (*EffectivePromptResponse, error) {
	s.promptMu.Lock()
	defer s.promptMu.Unlock()
	if s.promptLoaded && time.Since(s.promptCacheAt) < cacheTTL {
		return s.promptCache, nil
	}
	tpl, err := s.promptTemplateRepo.GetActive(ctx, constants.PromptTypeSystem)
	if err != nil {
		// 回源失败：返回错误让调用方降级（chat 用默认人格），但保留上次成功值以免抖动。
		if s.promptLoaded {
			return s.promptCache, nil
		}
		return nil, fmt.Errorf("get active prompt: %w", err)
	}
	resp := buildEffectivePrompt(tpl)
	s.promptCache = resp
	s.promptLoaded = true
	s.promptCacheAt = time.Now()
	return resp, nil
}

// invalidatePromptCache 失效本实例的系统提示词缓存（写入路径调用）。
func (s *ConfigService) invalidatePromptCache() {
	s.promptMu.Lock()
	defer s.promptMu.Unlock()
	s.promptLoaded = false
	s.promptCache = nil
}

// buildEffectivePrompt 把生效模板拼装为响应：无模板时回落默认人格 + 代码护栏。
// 来源信息（template_id / version）一并返回，供管理端展示与 chat 落库复盘。
func buildEffectivePrompt(tpl *entity.PromptTemplate) *EffectivePromptResponse {
	resp := &EffectivePromptResponse{
		Persona:    constants.SystemPromptPersona,
		Guardrails: constants.SystemPromptGuardrails,
		Source:     PromptSourceDefault,
	}
	if tpl != nil {
		resp.Persona = tpl.Content
		resp.Source = PromptSourceDatabase
		id, version := tpl.ID, tpl.Version
		resp.TemplateID = &id
		resp.Version = &version
	}
	resp.Content = constants.ComposeSystemPrompt(resp.Persona)
	return resp
}

// 生效来源取值（EffectivePromptResponse.Source）。
const (
	PromptSourceDatabase = "database"
	PromptSourceDefault  = "default"
)
