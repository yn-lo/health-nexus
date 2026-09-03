// Package constants 定义全局业务常量，避免魔法值（golangci-lint goconst 规则）。
package constants

// 用户角色。
const (
	RoleSuperAdmin = "SUPER_ADMIN"
	RoleDeptAdmin  = "DEPT_ADMIN"
	RoleDoctor     = "DOCTOR"
	RoleNurse      = "NURSE"
	RolePatient    = "PATIENT"
)

// IsStaff 判断是否为医护角色。
func IsStaff(role string) bool {
	switch role {
	case RoleSuperAdmin, RoleDeptAdmin, RoleDoctor, RoleNurse:
		return true
	}
	return false
}

// IsAdmin 判断是否为管理员角色（可访问 config 端点）。
func IsAdmin(role string) bool {
	switch role {
	case RoleSuperAdmin, RoleDeptAdmin:
		return true
	}
	return false
}

// 文章状态。
const (
	ArticleStatusDraft     = "draft"
	ArticleStatusPending   = "pending"
	ArticleStatusPublished = "published"
	ArticleStatusArchived  = "archived"
	ArticleStatusDeleted   = "deleted"
)

// 引用授权状态。
const (
	ReferenceStatusPending  = "pending"
	ReferenceStatusApproved = "approved"
	ReferenceStatusRejected = "rejected"
	ReferenceStatusRevoked  = "revoked"
)

// 引用授权查询方向（ListFilter.Direction）。
const (
	ReferenceDirectionOutgoing = "outgoing" // 源科室为当前科室
	ReferenceDirectionIncoming = "incoming" // 目标科室为当前科室
)

// 聊天消息角色。
const (
	MessageRoleUser      = "user"
	MessageRoleAssistant = "assistant"
)

// 聊天消息 result_code。
const (
	ResultAnswered    = "ANSWERED"
	ResultPartial     = "PARTIAL" // LLM 超时/中断，答案不完整
	ResultRejected    = "REJECTED"
	ResultIntercepted = "INTERCEPTED"
	ResultCrisis      = "CRISIS"
	ResultRateLimited = "RATE_LIMITED"
)

// 危机事件级别。
const (
	CrisisLevelHigh   = "high"
	CrisisLevelMedium = "medium"
	CrisisLevelLow    = "low"
)

// AI Provider 类型。
const (
	ProviderTypeLLM       = "llm"
	ProviderTypeEmbedding = "embedding"
	ProviderTypeRerank    = "rerank"
	ProviderTypeRewrite   = "rewrite"
)

// 敏感词类别。
const (
	SensitiveCategorySuicide   = "suicide"
	SensitiveCategoryEmergency = "emergency"
	SensitiveCategoryInjection = "injection"
)

// 安全规则类别（与 internal/di/schema.sql 的 CHECK 约束对齐）。
const (
	SafetyCategoryDiagnosis      = "diagnosis"
	SafetyCategoryPrescription   = "prescription"
	SafetyCategoryStopMedication = "stop_medication"
	SafetyCategoryDelayMedical   = "delay_medical"
	SafetyCategoryOther          = "other"
)

// Prompt 模板类型。仅 system 类型在运行时被注入 LLM 调用；
// rejection/emergency/safety_warning 话术由 safety_messages 表管理。
const (
	PromptTypeSystem = "system"
)

// 安全话术类型（与 SQL CHECK 约束对齐）。
// crisis_hotline 已废弃合并到 crisis_response，medication_disclaimer 已废弃合并到 safety_warning。
const (
	SafetyMessageRejection     = "rejection"
	SafetyMessageEmergency     = "emergency"
	SafetyMessageSafetyWarning = "safety_warning"
)

// 默认值。
const (
	MaxMessageLength    = 2000 // 单条用户消息最大字符数（REQ-CHAT-005）
	DefaultTopK         = 5
	DefaultChunkSize    = 500
	DefaultChunkOverlap = 50
	HistoryTurns        = 5 // 保留最近 N 轮历史（REQ-CHAT-005）
)

// DefaultSystemPrompt 系统提示词硬编码兜底。DB 无 active system prompt 时使用。
// 由 config handler GetEffectivePrompt 和 chat_send_service.buildSystemPrompt 共同引用。
const DefaultSystemPrompt = "你是一个医院健康宣教助手，只能基于以下参考资料回答患者问题。" +
	"规则：1) 不得诊断、开处方或建议停药；2) 若患者描述紧急症状，提醒立即就医；" +
	"3) 信息不足以回答时坦诚告知；4) 不得凭空生成，所有结论需基于参考资料；" +
	"5) 使用 Markdown 格式组织回答，增强内容可读性；" +
	"6) 直接回答问题，不要以\"好的\"\"收到\"\"很高兴为您服务\"等客套话开头，不要重复用户问题；" +
	"7) 若参考资料与用户问题无关或无实质信息，不得基于参考资料作答，应坦诚告知暂无相关资料。不得将无关内容强行关联作答。" +
	"格式规范（手机端阅读优先）：使用 CommonMark Markdown 语法；" +
	"列表最多一层，禁止嵌套子列表；需要分类时用**加粗小标题**+冒号引出，下接平铺要点；" +
	"顺序/步骤用有序列表(1.)，并列要点用无序列表(-)；" +
	"不要在有序列表内再嵌套无序列表；每条要点尽量一行，超长就拆条。"

// Token 预算阈值（REQ-CHAT-006-A）。
const (
	TokenBudgetRewrite  = 4000  // 改写场景 token 上限
	TokenBudgetGenerate = 16000 // 生成场景 token 上限
)
