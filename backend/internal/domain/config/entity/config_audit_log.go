package entity

import "time"

// ConfigAuditLog 配置变更审计日志（对应 config_audit_logs 表）。
// EntityID 为 nil 表示单例配置（RAGConfig / SafetyMessages）。
type ConfigAuditLog struct {
	ID           int64
	Action       string // create/update/delete
	EntityType   string // ai_provider/sensitive_word/safety_rule/rag_config/safety_message/prompt_template
	EntityID     *int64 // 单例配置为 nil
	OperatorID   int64
	OperatorRole string
	Changes      []byte // JSON
	CreatedAt    time.Time
}

// ConfigAuditLog 实体类型与 action 取值。
const (
	AuditEntityAIProvider     = "ai_provider"
	AuditEntitySensitiveWord  = "sensitive_word"
	AuditEntitySafetyRule     = "safety_rule"
	AuditEntityRAGConfig      = "rag_config"
	AuditEntitySafetyMessage  = "safety_message"
	AuditEntityPromptTemplate = "prompt_template"

	AuditActionCreate = "create"
	AuditActionUpdate = "update"
	AuditActionDelete = "delete"
)
