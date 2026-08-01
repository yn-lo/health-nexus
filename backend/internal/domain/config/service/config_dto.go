package service

import (
	"encoding/json"
	"time"

	"health-nexus/internal/domain/config/entity"
	"health-nexus/internal/shared/mask"
)

// ============ AI Provider DTOs ============

// CreateAIProviderRequest 创建 AI 提供商请求。APIKey 为明文，service 层加密。
type CreateAIProviderRequest struct {
	ProviderType string         `json:"provider_type"`
	Name         string         `json:"name"`
	APIBase      string         `json:"api_base"`
	APIKey       string         `json:"api_key"`
	ModelName    string         `json:"model_name"`
	Dimension    *int           `json:"dimension,omitempty"`
	Params       map[string]any `json:"params,omitempty"`
	IsActive     *bool          `json:"is_active,omitempty"`
	IsFullURL    bool           `json:"is_full_url,omitempty"` // true 时后端原样使用 api_base，不自动拼接 /v1
}

// UpdateAIProviderRequest 更新 AI 提供商请求。所有字段可选，nil 表示不更新。
type UpdateAIProviderRequest struct {
	Name       *string         `json:"name,omitempty"`
	APIBase    *string         `json:"api_base,omitempty"`
	APIKey     *string         `json:"api_key,omitempty"` // 明文传入，service 层加密
	ModelName  *string         `json:"model_name,omitempty"`
	Dimension  *int            `json:"dimension,omitempty"`
	Params     *map[string]any `json:"params,omitempty"`
	IsActive   *bool           `json:"is_active,omitempty"`
	IsFullURL  *bool           `json:"is_full_url,omitempty"`
}

// AIProviderResponse AI 提供商响应。APIKey 字段返回掩码（REQ-CONFIG-002）。
type AIProviderResponse struct {
	ID           int64          `json:"id"`
	Name         string         `json:"name"`
	ProviderType string         `json:"provider_type"`
	APIBase      string         `json:"api_base"`
	APIKey       string         `json:"api_key"` // 掩码
	ModelName    string         `json:"model_name"`
	Dimension    *int           `json:"dimension"`
	Params       map[string]any `json:"params"`
	IsActive     bool           `json:"is_active"`
	IsFullURL    bool           `json:"is_full_url"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

func toAIProviderResponse(p *entity.AIProvider) AIProviderResponse {
	if p == nil {
		return AIProviderResponse{}
	}
	params := p.Parameters
	if params == nil {
		params = map[string]any{}
	}
	return AIProviderResponse{
		ID:           p.ID,
		Name:         p.Name,
		ProviderType: p.ProviderType,
		APIBase:      p.APIURL,
		APIKey:       p.APIKeyMasked,
		ModelName:    p.ModelName,
		Dimension:    p.Dimension,
		Params:       params,
		IsActive:     p.IsActive,
		IsFullURL:    p.IsFullURL,
		CreatedAt:    p.CreatedAt,
		UpdatedAt:    p.UpdatedAt,
	}
}

// ============ Sensitive Word DTOs ============

// CreateSensitiveWordRequest 创建敏感词请求。
type CreateSensitiveWordRequest struct {
	Word     string `json:"word"`
	Category string `json:"category"`
	IsActive *bool  `json:"is_active,omitempty"`
}

// UpdateSensitiveWordRequest 更新敏感词请求。所有字段可选（契约 §6.2.3）。
type UpdateSensitiveWordRequest struct {
	Word     *string `json:"word,omitempty"`
	Category *string `json:"category,omitempty"`
	IsActive *bool   `json:"is_active,omitempty"`
}

// SensitiveWordResponse 敏感词响应。
type SensitiveWordResponse struct {
	ID        int64     `json:"id"`
	Word      string    `json:"word"`
	Category  string    `json:"category"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

func toSensitiveWordResponse(w *entity.SensitiveWord) SensitiveWordResponse {
	if w == nil {
		return SensitiveWordResponse{}
	}
	return SensitiveWordResponse{
		ID:        w.ID,
		Word:      w.Word,
		Category:  w.Category,
		IsActive:  w.IsActive,
		CreatedAt: w.CreatedAt,
	}
}

// ============ Safety Rule DTOs ============

// CreateSafetyRuleRequest 创建安全规则请求。
type CreateSafetyRuleRequest struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Pattern     string `json:"pattern"`
	Action      string `json:"action"`
	Replacement string `json:"replacement,omitempty"`
	IsActive    *bool  `json:"is_active,omitempty"`
	Description string `json:"description,omitempty"`
}

// UpdateSafetyRuleRequest 更新安全规则请求。所有字段可选。
type UpdateSafetyRuleRequest struct {
	Name        *string `json:"name,omitempty"`
	Category    *string `json:"category,omitempty"`
	Pattern     *string `json:"pattern,omitempty"`
	Action      *string `json:"action,omitempty"`
	Replacement *string `json:"replacement,omitempty"`
	IsActive    *bool   `json:"is_active,omitempty"`
	Description *string `json:"description,omitempty"`
}

// SafetyRuleResponse 安全规则响应。
type SafetyRuleResponse struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Category    string    `json:"category"`
	Pattern     string    `json:"pattern"`
	Action      string    `json:"action"`
	Replacement string    `json:"replacement"`
	IsActive    bool      `json:"is_active"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func toSafetyRuleResponse(r *entity.SafetyRule) SafetyRuleResponse {
	if r == nil {
		return SafetyRuleResponse{}
	}
	return SafetyRuleResponse{
		ID:          r.ID,
		Name:        r.Name,
		Category:    r.Category,
		Pattern:     r.Pattern,
		Action:      r.Action,
		Replacement: r.Replacement,
		IsActive:    r.IsActive,
		Description: r.Description,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

// ============ RAG Config DTOs ============

// UpdateRAGConfigRequest 更新 RAG 配置请求。所有字段可选。
type UpdateRAGConfigRequest struct {
	ChunkSize           *int     `json:"chunk_size,omitempty"`
	ChunkOverlap        *int     `json:"chunk_overlap,omitempty"`
	MaxChunks           *int     `json:"max_chunks,omitempty"`
	TopK                *int     `json:"top_k,omitempty"`
	SimilarityThreshold *float64 `json:"similarity_threshold,omitempty"`
	RerankEnabled       *bool    `json:"rerank_enabled,omitempty"`
	RerankThreshold     *float64 `json:"rerank_threshold,omitempty"`
	DiversityFactor     *float64 `json:"diversity_factor,omitempty"`
	OODThreshold        *float64 `json:"ood_threshold,omitempty"`
}

// RAGConfigResponse RAG 配置响应。
type RAGConfigResponse struct {
	ChunkSize           int       `json:"chunk_size"`
	ChunkOverlap        int       `json:"chunk_overlap"`
	MaxChunks           int       `json:"max_chunks"`
	TopK                int       `json:"top_k"`
	SimilarityThreshold float64   `json:"similarity_threshold"`
	RerankEnabled       bool      `json:"rerank_enabled"`
	RerankThreshold     float64   `json:"rerank_threshold"`
	DiversityFactor     float64   `json:"diversity_factor"`
	OODThreshold        float64   `json:"ood_threshold"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func toRAGConfigResponse(c *entity.RAGConfig) RAGConfigResponse {
	if c == nil {
		return RAGConfigResponse{}
	}
	return RAGConfigResponse{
		ChunkSize:           c.ChunkSize,
		ChunkOverlap:        c.ChunkOverlap,
		MaxChunks:           c.MaxChunks,
		TopK:                c.TopK,
		SimilarityThreshold: c.SimilarityThreshold,
		RerankEnabled:       c.RerankEnabled,
		RerankThreshold:     c.RerankThreshold,
		DiversityFactor:     c.DiversityFactor,
		OODThreshold:        c.OODThreshold,
		UpdatedAt:           c.UpdatedAt,
	}
}

// ============ Prompt Template DTOs ============

// CreatePromptTemplateRequest 创建 Prompt 模板请求。Version 由 service 层自动递增。
type CreatePromptTemplateRequest struct {
	Type         string `json:"type"`
	Content      string `json:"content"`
	IsActive     *bool  `json:"is_active,omitempty"`
	Description  string `json:"description,omitempty"`
	DepartmentID *int64 `json:"department_id,omitempty"`
}

// UpdatePromptTemplateRequest 更新 Prompt 模板请求（契约 §6.6.3）。
type UpdatePromptTemplateRequest struct {
	Content  *string `json:"content,omitempty"`
	IsActive *bool   `json:"is_active,omitempty"`
}

// PromptTemplateResponse Prompt 模板响应。
type PromptTemplateResponse struct {
	ID           int64     `json:"id"`
	Type         string    `json:"type"`
	Version      int       `json:"version"`
	Content      string    `json:"content"`
	IsActive     bool      `json:"is_active"`
	Description  string    `json:"description"`
	DepartmentID *int64    `json:"department_id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func toPromptTemplateResponse(p *entity.PromptTemplate) PromptTemplateResponse {
	if p == nil {
		return PromptTemplateResponse{}
	}
	return PromptTemplateResponse{
		ID:           p.ID,
		Type:         p.Type,
		Version:      p.Version,
		Content:      p.Content,
		IsActive:     p.IsActive,
		Description:  p.Description,
		DepartmentID: p.DepartmentID,
		CreatedAt:    p.CreatedAt,
		UpdatedAt:    p.UpdatedAt,
	}
}

// EffectivePromptResponse 当前生效的系统提示词响应。
// DB 中有 active system prompt 时返回 DB 内容；否则返回硬编码兜底 DefaultSystemPrompt。
type EffectivePromptResponse struct {
	Content string `json:"content"`
	Source  string `json:"source"` // "database" 或 "default"
}

// ============ Safety Message DTOs ============

// UpdateSafetyMessagesRequest 更新安全话术请求。所有字段可选，nil 表示不更新。
type UpdateSafetyMessagesRequest struct {
	RejectionMessage     *string `json:"rejection_message,omitempty"`
	EmergencyMessage     *string `json:"emergency_message,omitempty"`
	SafetyWarningMessage *string `json:"safety_warning_message,omitempty"`
	CrisisResponse       *string `json:"crisis_response,omitempty"`
	NoKnowledgeMessage   *string `json:"no_knowledge_message,omitempty"`
	SystemErrorMessage   *string `json:"system_error_message,omitempty"`
}

// SafetyMessagesResponse 安全话术响应（聚合 6 个 type 的单例视图）。
type SafetyMessagesResponse struct {
	RejectionMessage     string    `json:"rejection_message"`
	EmergencyMessage     string    `json:"emergency_message"`
	SafetyWarningMessage string    `json:"safety_warning_message"`
	CrisisResponse       string    `json:"crisis_response"`
	NoKnowledgeMessage   string    `json:"no_knowledge_message"`
	SystemErrorMessage   string    `json:"system_error_message"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// DefaultSafetyMessages 是缺失时的默认值（REQ-CONFIG-008 降级）。
// safety_warning_message 合并了原 medication_disclaimer 的语义：涉及用药时追加安全警告。
var DefaultSafetyMessages = SafetyMessagesResponse{
	RejectionMessage:     "抱歉，我无法回答这个问题，建议您咨询您的主治医生。",
	EmergencyMessage:     "您描述的症状需要紧急就医，请立即前往最近的医院急诊科或拨打 120。",
	SafetyWarningMessage: "请注意：以上信息仅供参考，不能替代专业医疗诊断和治疗。用药请严格遵照医嘱，如有疑问请咨询您的主治医生或药师。",
	CrisisResponse:       "如果您正在经历心理困扰或有自伤想法，请立即拨打心理援助热线 400-161-9995，或前往最近的医院急诊科。",
	NoKnowledgeMessage:   "抱歉，知识库中暂无与您问题相关的内容，建议您咨询主治医生或换个问法试试。",
	SystemErrorMessage:   "抱歉，系统暂时繁忙未能生成回答，请稍后重试。",
}

func applySafetyMessage(resp *SafetyMessagesResponse, m *entity.SafetyMessage) {
	switch m.Type {
	case entity.SafetyMessageTypeRejection:
		resp.RejectionMessage = m.Content
	case entity.SafetyMessageTypeEmergency:
		resp.EmergencyMessage = m.Content
	case entity.SafetyMessageTypeSafetyWarning:
		resp.SafetyWarningMessage = m.Content
	case entity.SafetyMessageTypeCrisisResponse:
		resp.CrisisResponse = m.Content
	case entity.SafetyMessageTypeNoKnowledge:
		resp.NoKnowledgeMessage = m.Content
	case entity.SafetyMessageTypeSystemError:
		resp.SystemErrorMessage = m.Content
	}
}

// ============ Config Status DTOs ============

// ProviderStatus 单个 LLM 模块的配置状态。
type ProviderStatus struct {
	Configured bool   `json:"configured"`
	Message    string `json:"message,omitempty"` // 未配置时的提示信息
}

// ConfigStatusResponse 系统配置状态响应，供管理端工作台展示黄色预警。
type ConfigStatusResponse struct {
	LLM       ProviderStatus `json:"llm"`
	Embedding ProviderStatus `json:"embedding"`
	Rerank    ProviderStatus `json:"rerank"`
	Rewrite   ProviderStatus `json:"rewrite"`
}

// ============ Mask helper ============

// MaskAPIKey 掩码 API Key（sk-****abcd 格式）。
func MaskAPIKey(key string) string {
	return mask.APIKey(key)
}

// ============ Config Audit Log DTOs ============

// ConfigAuditLogResponse 配置审计日志响应。
// Changes 是 repo 直接透传的 JSONB 字节，handler 用 json.RawMessage 返回给前端。
type ConfigAuditLogResponse struct {
	ID           int64           `json:"id"`
	Action       string          `json:"action"`
	EntityType   string          `json:"entity_type"`
	EntityID     *int64          `json:"entity_id"`
	OperatorID   int64           `json:"operator_id"`
	OperatorRole string          `json:"operator_role"`
	Changes      json.RawMessage `json:"changes,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

func toConfigAuditLogResponse(l *entity.ConfigAuditLog) ConfigAuditLogResponse {
	if l == nil {
		return ConfigAuditLogResponse{}
	}
	var changes json.RawMessage
	if len(l.Changes) > 0 {
		changes = json.RawMessage(l.Changes)
	}
	return ConfigAuditLogResponse{
		ID:           l.ID,
		Action:       l.Action,
		EntityType:   l.EntityType,
		EntityID:     l.EntityID,
		OperatorID:   l.OperatorID,
		OperatorRole: l.OperatorRole,
		Changes:      changes,
		CreatedAt:    l.CreatedAt,
	}
}
