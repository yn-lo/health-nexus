// Package constants 定义全局业务常量，避免魔法值（golangci-lint goconst 规则）。
package constants

import "strings"

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
// 注：原 rewrite（独立查询改写模型）已随统一理解与审查（Assessor）合并调用而下线。
const (
	ProviderTypeLLM       = "llm"
	ProviderTypeEmbedding = "embedding"
	ProviderTypeRerank    = "rerank"
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

// LLM 输入安全审查分类（platform/llm 输出，shared/rag 与 chat 域消费）。
// 布尔值不足以表达这条链路：自伤风险必须能触发危机记录与热线流程，
// 不能被压平成"普通拒答"。分类同时写入危机事件的命中关键词，供医护端追溯判定来源。
const (
	SafetyClassSafe            = "SAFE"             // 放行
	SafetyClassSelfHarm        = "SELF_HARM"        // 自伤/自杀倾向（→ 危机链路）
	SafetyClassPromptInjection = "PROMPT_INJECTION" // 越狱/注入
	SafetyClassMedicalAbuse    = "MEDICAL_ABUSE"    // 滥用剂量/违禁药物等有害医疗请求
	SafetyClassAbusive         = "ABUSIVE"          // 攻击性/辱骂/歧视
	SafetyClassUnsafe          = "UNSAFE"           // 无法细分类的兜底拦截
)

// Prompt 模板类型。仅 system 类型在运行时被注入 LLM 调用；
// rejection/emergency/safety_warning 话术由 safety_messages 表管理。
//
// 系统提示词分两层（见 ComposeSystemPrompt）：
//   - 可配置层（DB prompt_templates.type='system'）：人格/口吻/科室特色；
//   - 代码独占层（SystemPromptGuardrails）：医疗红线 + 引用规则 + 格式规范，
//     管理端无法覆盖或删除——护栏属于代码资产，不属于运营配置。
const (
	PromptTypeSystem = "system"
)

// MaxSystemPromptPersonaRunes 可配置人格声明长度上限（rune）。
// 超长内容会挤占生成侧上下文预算，且运营场景不需要长文。
const MaxSystemPromptPersonaRunes = 4096

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

// SystemPromptPersona 默认人格声明（可配置部分的兜底）。
// DB 无 active system prompt 时使用；管理员配置的内容只替换这一段，护栏永远由代码追加。
const SystemPromptPersona = "你是一个医院健康宣教助手，只能基于以下参考资料回答患者问题。"

// SystemPromptGuardrails 代码独占的系统护栏规则：管理端不可见、不可改、不可删。
// 与 DB 人格声明分离的原因——这份规则承载医疗红线（不得诊断/停药）、可溯源性（[资料N] 引用）
// 与手机端可读性（Markdown 列表约定），一旦允许运营覆盖，任何一次「简化提示词」都会静默
// 削弱全院的答案质量与合规性，而输出侧审核只校验越界与有据，不校验引用与格式。
const SystemPromptGuardrails = "规则：1) 不得诊断、开处方或建议停药；2) 若患者描述紧急症状，提醒立即就医；" +
	"3) 信息不足以回答时坦诚告知；4) 不得凭空生成，所有结论需基于参考资料；" +
	"5) 使用 Markdown 格式组织回答，增强内容可读性；" +
	"6) 直接回答问题，不要以\"好的\"\"收到\"\"很高兴为您服务\"等客套话开头，不要重复用户问题；" +
	"7) 若参考资料与用户问题无关或无实质信息，不得基于参考资料作答，应坦诚告知暂无相关资料。不得将无关内容强行关联作答。" +
	"8) 陈述关键结论时用 [资料N] 标注所依据的参考资料编号（每条参考资料对应一篇知识库文章，N 为其编号）；" +
	"每个关键结论都必须标注来源，且只能使用给定编号，不得编造；整篇回答若未引用任何参考资料视为不合规；无参考资料时应坦诚告知暂无相关资料。" +
	"9) 参考资料之间若表述矛盾，须并列说明差异，不得静默择一，也不得凭常识臆断。" +
	"10) 不得建议患者推迟或放弃就医，不得建议自行加量、减量、停药或换药。" +
	"格式规范（手机端阅读优先）：使用 CommonMark Markdown 语法；" +
	"列表最多一层，禁止嵌套子列表；需要分类时用**加粗小标题**+冒号引出，下接平铺要点；" +
	"顺序/步骤用有序列表(1.)，并列要点用无序列表(-)；" +
	"不要在有序列表内再嵌套无序列表；每条要点尽量一行，超长就拆条。" +
	"面向患者（非专业读者）：先给结论再给细节，专业术语首次出现时用括号给出通俗解释，" +
	"不整段罗列检验数值与专业缩写，不堆砌无法执行的空泛建议。"

// ComposeSystemPrompt 拼装最终下发给 LLM 的 system prompt：可配置人格 + 代码护栏。
// persona 为空时使用 SystemPromptPersona。config 域（管理端预览）与 chat 域（真实调用）
// 共用此函数，保证「管理端看到的」与「患者实际生效的」完全一致。
func ComposeSystemPrompt(persona string) string {
	p := strings.TrimSpace(persona)
	if p == "" {
		p = SystemPromptPersona
	}
	return p + "\n\n" + SystemPromptGuardrails
}

// Token 预算阈值（REQ-CHAT-006-A）。
const (
	TokenBudgetGenerate = 16000 // 生成场景 token 上限
)
