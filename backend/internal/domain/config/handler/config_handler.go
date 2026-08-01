// Package handler 实现 config 域的 HTTP 端点（18 个端点，5 个子模块）。
package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"health-nexus/internal/domain/config/service"
	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/pagination"
	"health-nexus/internal/shared/response"
)

// ConfigHandler 处理 /api/staff/config/* 端点。
type ConfigHandler struct {
	svc *service.ConfigService
}

// NewConfigHandler 创建 ConfigHandler。
func NewConfigHandler(svc *service.ConfigService) *ConfigHandler {
	return &ConfigHandler{svc: svc}
}

// SuccessResponse 动作类端点（删除/激活）的成功响应。
type SuccessResponse struct {
	Success bool `json:"success"`
}

func ok(w http.ResponseWriter) {
	response.WriteOK(w, SuccessResponse{Success: true})
}

// ============ AI Provider ============

// GetConfigStatus GET /api/staff/config/status
// 返回各 LLM 模块的配置状态，供管理端工作台展示黄色预警。
func (h *ConfigHandler) GetConfigStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.svc.GetConfigStatus(r.Context())
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, status)
}

// ListAIProviders GET /api/staff/config/ai-providers
func (h *ConfigHandler) ListAIProviders(w http.ResponseWriter, r *http.Request) {
	providerType := r.URL.Query().Get("provider_type")
	isActive, err := parseBoolQuery(r, "is_active")
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	list, err := h.svc.ListAIProviders(r.Context(), providerType, isActive)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	out := make([]service.AIProviderResponse, 0, len(list))
	for _, p := range list {
		out = append(out, p)
	}
	response.WriteOK(w, out)
}

// GetAIProvider GET /api/staff/config/ai-providers/{id}
func (h *ConfigHandler) GetAIProvider(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	p, err := h.svc.GetAIProvider(r.Context(), id)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, p)
}

// CreateAIProvider POST /api/staff/config/ai-providers
func (h *ConfigHandler) CreateAIProvider(w http.ResponseWriter, r *http.Request) {
	var req service.CreateAIProviderRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	p, err := h.svc.CreateAIProvider(r.Context(), req)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteCreated(w, p)
}

// UpdateAIProvider PUT /api/staff/config/ai-providers/{id}
func (h *ConfigHandler) UpdateAIProvider(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	var req service.UpdateAIProviderRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	p, err := h.svc.UpdateAIProvider(r.Context(), id, req)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, p)
}

// DeleteAIProvider DELETE /api/staff/config/ai-providers/{id}
func (h *ConfigHandler) DeleteAIProvider(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	if err := h.svc.DeleteAIProvider(r.Context(), id); err != nil {
		response.WriteError(w, r, err)
		return
	}
	ok(w)
}

// TestAIProvider POST /api/staff/config/ai-providers/{id}/test
// 方案 C：按 provider_type 调用一次最小请求验证连通性，返回 success/latency/detail。
func (h *ConfigHandler) TestAIProvider(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	result, err := h.svc.TestAIProvider(r.Context(), id)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, result)
}

// ============ Sensitive Word ============

// ListSensitiveWords GET /api/staff/config/sensitive-words
func (h *ConfigHandler) ListSensitiveWords(w http.ResponseWriter, r *http.Request) {
	p, err := pagination.Parse(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	category := r.URL.Query().Get("category")
	list, total, err := h.svc.ListSensitiveWords(r.Context(), category, p)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	out := make([]service.SensitiveWordResponse, 0, len(list))
	for _, w := range list {
		out = append(out, w)
	}
	response.WriteOK(w, pagination.NewResult(out, total, p))
}

// CreateSensitiveWord POST /api/staff/config/sensitive-words
func (h *ConfigHandler) CreateSensitiveWord(w http.ResponseWriter, r *http.Request) {
	var req service.CreateSensitiveWordRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	w2, err := h.svc.CreateSensitiveWord(r.Context(), req)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteCreated(w, w2)
}

// DeleteSensitiveWord DELETE /api/staff/config/sensitive-words/{id}
func (h *ConfigHandler) DeleteSensitiveWord(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	if err := h.svc.DeleteSensitiveWord(r.Context(), id); err != nil {
		response.WriteError(w, r, err)
		return
	}
	ok(w)
}

// UpdateSensitiveWord PUT /api/staff/config/sensitive-words/{id}（契约 §6.2.3）
func (h *ConfigHandler) UpdateSensitiveWord(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	var req service.UpdateSensitiveWordRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	w2, err := h.svc.UpdateSensitiveWord(r.Context(), id, req)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, w2)
}

// ============ Safety Rule ============

// ListSafetyRules GET /api/staff/config/safety-rules
func (h *ConfigHandler) ListSafetyRules(w http.ResponseWriter, r *http.Request) {
	p, err := pagination.Parse(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	category := r.URL.Query().Get("category")
	list, total, err := h.svc.ListSafetyRules(r.Context(), category, p)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	out := make([]service.SafetyRuleResponse, 0, len(list))
	for _, rule := range list {
		out = append(out, rule)
	}
	response.WriteOK(w, pagination.NewResult(out, total, p))
}

// CreateSafetyRule POST /api/staff/config/safety-rules
func (h *ConfigHandler) CreateSafetyRule(w http.ResponseWriter, r *http.Request) {
	var req service.CreateSafetyRuleRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	rule, err := h.svc.CreateSafetyRule(r.Context(), req)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteCreated(w, rule)
}

// UpdateSafetyRule PUT /api/staff/config/safety-rules/{id}
func (h *ConfigHandler) UpdateSafetyRule(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	var req service.UpdateSafetyRuleRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	rule, err := h.svc.UpdateSafetyRule(r.Context(), id, req)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, rule)
}

// DeleteSafetyRule DELETE /api/staff/config/safety-rules/{id}
func (h *ConfigHandler) DeleteSafetyRule(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	if err := h.svc.DeleteSafetyRule(r.Context(), id); err != nil {
		response.WriteError(w, r, err)
		return
	}
	ok(w)
}

// ============ RAG Config ============

// GetRAGConfig GET /api/staff/config/rag
func (h *ConfigHandler) GetRAGConfig(w http.ResponseWriter, r *http.Request) {
	c, err := h.svc.GetRAGConfig(r.Context())
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, c)
}

// UpdateRAGConfig PUT /api/staff/config/rag
func (h *ConfigHandler) UpdateRAGConfig(w http.ResponseWriter, r *http.Request) {
	var req service.UpdateRAGConfigRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	c, err := h.svc.UpdateRAGConfig(r.Context(), req)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, c)
}

// ============ Prompt Template ============

// ListPromptTemplates GET /api/staff/config/prompts
func (h *ConfigHandler) ListPromptTemplates(w http.ResponseWriter, r *http.Request) {
	p, err := pagination.Parse(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	promptType := r.URL.Query().Get("type")
	isActive, err := parseBoolQuery(r, "is_active")
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	list, total, err := h.svc.ListPromptTemplates(r.Context(), promptType, isActive, p)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	out := make([]service.PromptTemplateResponse, 0, len(list))
	for _, t := range list {
		out = append(out, t)
	}
	response.WriteOK(w, pagination.NewResult(out, total, p))
}

// CreatePromptTemplate POST /api/staff/config/prompts
func (h *ConfigHandler) CreatePromptTemplate(w http.ResponseWriter, r *http.Request) {
	var req service.CreatePromptTemplateRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	t, err := h.svc.CreatePromptTemplate(r.Context(), req)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteCreated(w, t)
}

// UpdatePromptTemplate PUT /api/staff/config/prompts/{id}（契约 §6.6.3）
// 请求体 {content?, is_active?}；is_active 由 false→true 时同 type+department_id 下其他自动失活。
func (h *ConfigHandler) UpdatePromptTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	var req service.UpdatePromptTemplateRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	t, err := h.svc.UpdatePromptTemplate(r.Context(), id, req)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, t)
}

// DeletePromptTemplate DELETE /api/staff/config/prompts/{id}（契约 §6.6.4）
// 当前生效版本不可删除，返回 409。
func (h *ConfigHandler) DeletePromptTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	if err := h.svc.DeletePromptTemplate(r.Context(), id); err != nil {
		response.WriteError(w, r, err)
		return
	}
	ok(w)
}

// GetEffectivePrompt GET /api/staff/config/prompts/effective
// 返回当前生效的系统提示词，不论来源（DB 或硬编码兜底）。
func (h *ConfigHandler) GetEffectivePrompt(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.GetEffectiveSystemPrompt(r.Context())
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, p)
}

// ============ Safety Message ============

// GetSafetyMessages GET /api/staff/config/safety-messages
func (h *ConfigHandler) GetSafetyMessages(w http.ResponseWriter, r *http.Request) {
	m, err := h.svc.GetSafetyMessages(r.Context())
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, m)
}

// GetSafetyPolicy GET /api/staff/config/safety-policy
func (h *ConfigHandler) GetSafetyPolicy(w http.ResponseWriter, r *http.Request) {
	policy, err := h.svc.GetSafetyPolicy(r.Context())
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, policy)
}

// UpdateSafetyMessages PUT /api/staff/config/safety-messages
func (h *ConfigHandler) UpdateSafetyMessages(w http.ResponseWriter, r *http.Request) {
	var req service.UpdateSafetyMessagesRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	m, err := h.svc.UpdateSafetyMessages(r.Context(), req)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, m)
}

// ============ Audit Log ============

// ListAuditLogs GET /api/staff/config/audit-logs
// 查询参数：entity_type（可选）、entity_id（可选 int64，0/缺省按 IS NULL 过滤）、page、page_size
func (h *ConfigHandler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	p, err := pagination.Parse(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	entityType := r.URL.Query().Get("entity_type")
	entityID, _ := strconv.ParseInt(r.URL.Query().Get("entity_id"), 10, 64)
	if entityID < 0 {
		entityID = 0
	}
	list, total, err := h.svc.ListAuditLogs(r.Context(), entityType, entityID, p)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	out := make([]service.ConfigAuditLogResponse, 0, len(list))
	for _, l := range list {
		out = append(out, l)
	}
	response.WriteOK(w, pagination.NewResult(out, total, p))
}

// ============ helpers ============

// parseID 从 chi URL 路径参数 "id" 解析 int64。
func parseID(r *http.Request) (int64, error) {
	s := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id < 1 {
		return 0, apperrors.Validation("CONFIG_INVALID_ID", "id 无效")
	}
	return id, nil
}

// parseBoolQuery 解析 bool 查询参数。空字符串返回 nil（不过滤）。
func parseBoolQuery(r *http.Request, key string) (*bool, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return nil, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return nil, apperrors.Validation("CONFIG_INVALID_BOOL", key+" 参数无效")
	}
	return &b, nil
}

// decodeJSON 解析请求体。空 body 或格式错误返回 422。
func decodeJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return apperrors.Validation("CONFIG_EMPTY_BODY", "请求体不能为空")
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return apperrors.Validation("CONFIG_INVALID_JSON", "请求体格式错误")
	}
	return nil
}
