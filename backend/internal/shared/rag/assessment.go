package rag

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// 本轮问答前的统一理解与审查（REQ-CHAT-007 重构）。
//
// 设计原则：AI 负责判断，后端负责执行。
//   - 模型输出结构化判定（意图 + 各类风险 + 是否缺关键信息 + 检索改写），不输出长篇推理；
//   - 后端严格校验字段与枚举，并按**固定优先级**决定最终动作（不直接采信模型建议）；
//   - 任何解析失败/超时/字段非法 → ErrAssessmentInvalid，按"无法判断"降级，绝不视为 SAFE。

// 意图取值（Assessment.Intent）。
const (
	IntentPatientEducation = "patient_education"        // 普通宣教（知识库可答）
	IntentProcessInquiry   = "process_inquiry"          // 就诊流程/科室/预约等事务咨询
	IntentIndividualized   = "individualized_diagnosis" // 个体化诊断请求（"我这是不是XX病"）
	IntentMedicationChange = "medication_change"        // 用药调整请求（加量/减量/停药/换药）
	IntentOther            = "other"
)

// 风险判定状态。允许 uncertain——not_detected 仅表示本次未识别到风险，不等于已证明安全。
const (
	RiskNotDetected = "not_detected"
	RiskSuspected   = "suspected"
	RiskConfirmed   = "confirmed"
	RiskUncertain   = "uncertain"
)

// 最终处理动作（由 Assessment.Action() 依固定优先级计算，模型建议仅作参考）。
const (
	ActionRetrieve   = "retrieve"   // 普通宣教：检索 → 生成
	ActionClarify    = "clarify"    // 关键信息不足：先澄清，不检索不生成
	ActionRestricted = "restricted" // 个体化诊疗/用药调整：固定边界说明，不生成
	ActionEmergency  = "emergency"  // 疑似急症：固定急救指引，不生成普通宣教
	ActionCrisis     = "crisis"     // 自伤风险：危机流程（记录 + 热线）
	ActionReject     = "reject"     // 注入/医疗滥用：拒答
)

// 字段长度与数量上限（校验用，防止模型输出异常长内容污染日志与上下文）。
const (
	assessmentMaxQueryRunes   = 500
	assessmentMaxEvidence     = 10
	assessmentMaxEvidenceRune = 200
	assessmentMaxMissing      = 10
	assessmentMaxQuestionRune = 200
)

// AssessTurn 审查输入的一轮历史（避免 rag 依赖 platform 层的消息类型）。
type AssessTurn struct {
	Role    string // "user" / "assistant"
	Content string
}

// Assessment 统一的每轮理解与审查结果。
type Assessment struct {
	Intent                string   `json:"intent"`
	EmergencyRisk         string   `json:"emergency_risk"`
	SelfHarmRisk          string   `json:"self_harm_risk"`
	MedicalAbuse          bool     `json:"medical_abuse"`
	PromptInjection       bool     `json:"prompt_injection"`
	IndividualizedDx      bool     `json:"individualized_diagnosis"`
	MedicationChange      bool     `json:"medication_change_request"`
	OutOfDomain           bool     `json:"out_of_domain"`
	ContextSufficient     bool     `json:"context_sufficient"`
	MissingCritical       []string `json:"missing_critical_information"`
	StandaloneQuery       string   `json:"standalone_query"`
	MustPreserve          []string `json:"must_preserve_constraints"`
	ClarificationQuestion string   `json:"clarification_question"`
	RiskEvidence          []string `json:"risk_evidence"`
	// RecommendedAction 模型建议动作：仅用于日志与冲突观测，最终动作由 Action() 决定。
	RecommendedAction string `json:"recommended_action"`
}

// ErrAssessmentInvalid 审查结果不可用（超时 / 网络失败 / 缺字段 / 枚举非法 / 无法解析）。
// 调用方必须按"无法判断"降级（受限流程），不得回退为 SAFE 放行。
var ErrAssessmentInvalid = errors.New("rag: assessment unavailable")

// Action 按固定优先级计算最终动作。
// 优先级：自伤 > 急症 > 注入/滥用 > 个体化诊疗·用药调整 > 域外问题(跳过澄清) > 信息不足 > 正常检索。
// 同一句可能同时命中多个分类（如"胸痛还能不能加药"），固定优先级保证不会因模型只选了一个主意图
// 而忽略其他风险；不确定状态按保守方向处理（救命场景宁可误报不可漏报）。
func (a Assessment) Action() string {
	if riskIsPresent(a.SelfHarmRisk) {
		return ActionCrisis
	}
	if riskIsPresent(a.EmergencyRisk) {
		return ActionEmergency
	}
	if a.PromptInjection || a.MedicalAbuse {
		return ActionReject
	}
	if a.IndividualizedDx || a.MedicationChange ||
		a.Intent == IntentIndividualized || a.Intent == IntentMedicationChange {
		return ActionRestricted
	}
	// 域外问题：只跳过"澄清"，**不跳过检索**。
	// 原因：本院业务边界由知识库定义，不由模型的"健康"概念定义——知识库允许收录院区地图、
	// 就医引导、挂号流程、探视规定等非医学内容，若按模型的域外判定直接拒答，
	// 会把"你们医院怎么走"这类本院能答的问题误拦（误拦代价远高于多跑一次向量检索）。
	// 因此"能不能答"交由客观检索命中决定；模型的 out_of_domain 只用于 0 命中时选择话术
	// （见 chat 域 prepareRAGContext 的 0 命中分支）。
	if a.OutOfDomain {
		return ActionRetrieve
	}
	if !a.ContextSufficient {
		return ActionClarify
	}
	return ActionRetrieve
}

// riskIsPresent 判定风险状态是否应触发保守处置：suspected/confirmed/uncertain 均触发。
func riskIsPresent(state string) bool {
	switch state {
	case RiskSuspected, RiskConfirmed, RiskUncertain:
		return true
	default:
		return false
	}
}

// Validate 校验模型输出的必填字段与枚举。任一非法即判定审查不可用。
func (a Assessment) Validate() error {
	if !validIntent(a.Intent) {
		return fmt.Errorf("%w: invalid intent %q", ErrAssessmentInvalid, a.Intent)
	}
	if !validRisk(a.EmergencyRisk) {
		return fmt.Errorf("%w: invalid emergency_risk %q", ErrAssessmentInvalid, a.EmergencyRisk)
	}
	if !validRisk(a.SelfHarmRisk) {
		return fmt.Errorf("%w: invalid self_harm_risk %q", ErrAssessmentInvalid, a.SelfHarmRisk)
	}
	if utf8.RuneCountInString(a.StandaloneQuery) > assessmentMaxQueryRunes {
		return fmt.Errorf("%w: standalone_query too long", ErrAssessmentInvalid)
	}
	if utf8.RuneCountInString(a.ClarificationQuestion) > assessmentMaxQuestionRune {
		return fmt.Errorf("%w: clarification_question too long", ErrAssessmentInvalid)
	}
	if len(a.MissingCritical) > assessmentMaxMissing {
		return fmt.Errorf("%w: too many missing_critical_information", ErrAssessmentInvalid)
	}
	if len(a.RiskEvidence) > assessmentMaxEvidence {
		return fmt.Errorf("%w: too many risk_evidence", ErrAssessmentInvalid)
	}
	for _, ev := range a.RiskEvidence {
		if utf8.RuneCountInString(ev) > assessmentMaxEvidenceRune {
			return fmt.Errorf("%w: risk_evidence too long", ErrAssessmentInvalid)
		}
	}
	// 需要检索时必须给出可检索的改写问题（否则检索会退化为空查询）。
	// 域外问题例外：模型不会为它产出检索改写，检索侧退回用患者原话（见 chat 域 prepareRAGContext）。
	if a.Action() == ActionRetrieve && !a.OutOfDomain && strings.TrimSpace(a.StandaloneQuery) == "" {
		return fmt.Errorf("%w: standalone_query required for retrieve", ErrAssessmentInvalid)
	}
	return nil
}

func validIntent(v string) bool {
	switch v {
	case IntentPatientEducation, IntentProcessInquiry, IntentIndividualized, IntentMedicationChange, IntentOther:
		return true
	default:
		return false
	}
}

func validRisk(v string) bool {
	switch v {
	case RiskNotDetected, RiskSuspected, RiskConfirmed, RiskUncertain:
		return true
	default:
		return false
	}
}

// Assessor 统一理解与审查能力（消费者定义，由 adapter 适配 platform/llm 实现）。
// 每轮调用一次：输入患者原话 + 必要历史，输出结构化判定与检索改写。
type Assessor interface {
	// AssessAndRewrite 执行统一理解、风险审查与查询改写。
	// 失败（超时/解析失败/字段非法）必须返回包装 ErrAssessmentInvalid 的错误。
	AssessAndRewrite(ctx context.Context, message string, history []AssessTurn) (Assessment, error)
}

// 固定处置话术（临床审核的固定指引，代码级常量，不依赖 DB 配置）。
//
// ponytail: 与 crisis/emergency 话术一致走代码常量——新增 safety_messages 类型需要
// config 域 schema/前端同步改造，折中；
// 升级路径：需要科室级差异时迁移到 safety_messages 表由配置下发。
const (
	// defaultRestrictedCare 个体化诊疗/用药调整请求的边界说明（含可用的求助渠道）。
	defaultRestrictedCare = "抱歉，我不能为您提供个体化的诊疗判断或用药调整建议（包括加量、减量、停药、换药）。" +
		"这类判断必须由了解您病情的主治医生做出，请携带现有用药清单尽快咨询您的主治医生或药师。" +
		"如果症状突然加重或出现胸痛、呼吸困难、意识不清等情况，请立即前往急诊或拨打 120。"
	// defaultEmergencyGuidance 疑似急症的固定急救指引（不使用生成内容，避免与提醒冲突）。
	defaultEmergencyGuidance = "您描述的情况可能属于需要紧急处理的情况，请立即停止自行处理，" +
		"马上前往最近的医院急诊科，或拨打 120 呼叫急救。" +
		"在等待或前往医院途中，请保持安静休息，不要自行加减药物；如已昏迷或无法呼吸，请立即呼叫身边人协助。"
	// defaultClarificationPrompt 关键信息不足时的澄清话术前缀（后接具体澄清问题）。
	defaultClarificationPrompt = "为了给您更准确的宣教信息，还需要您补充一点情况："
	// defaultClarificationFallback 判定为"信息不足需澄清"但模型未给出具体问题时的兜底话术。
	// 不得复用"审查失败"话术：审查其实成功，误报会让患者以为系统故障并掩盖真实状态。
	defaultClarificationFallback = "为了给您更准确的宣教信息，请补充更具体的健康问题" +
		"（例如症状、持续时间或您想了解的内容），我再为您解答。"
	// defaultOutOfDomain 域外问题（与健康宣教完全无关）的固定边界话术。
	// 需要同时做到两件事：说清能力边界 + 引导用户提出真正想问的健康问题。
	// 不得复用注入/滥用的拒答话术——对"如何写爬虫"这类问题"建议咨询主治医生"答非所问。
	// 固定文案而非让 LLM 现生成：LLM 会默认假设用户在问健康问题，容易生成"请描述症状"这种错误框架的澄清。
	defaultOutOfDomain = "我是医院健康宣教助手，只能回答健康相关的问题。" +
		"如果您有健康方面的疑问（比如症状、用药、检查、康复或生活方式），直接告诉我，我再为您解答。"
	// defaultAssessmentFailed 审查不可用时的固定兜底话术（按"无法判断"降级，不等于安全放行）。
	defaultAssessmentFailed = "为确保回答安全，本次未能完成必要的安全审查，暂时无法回答这个问题。" +
		"请稍后重试，或直接咨询您的主治医生。"
	// defaultAnonymousCrisisNote 匿名会话的危机处置边界说明（P1）。
	// 匿名会话不记录危机、不通知医护，必须在界面上让患者明确这一点，
	// 避免患者误以为"医护已经收到并正在处理"而放弃自救。
	defaultAnonymousCrisisNote = "（当前为匿名咨询，本条内容不会转交医护人员，也不会留下记录；" +
		"如情况紧急请直接拨打上面的热线或前往急诊。）"
)

// AnonymousCrisisNote 匿名会话的危机处置边界说明。
func AnonymousCrisisNote() string { return defaultAnonymousCrisisNote }

// RestrictedCareMessage 个体化诊疗/用药调整的固定边界说明。
func RestrictedCareMessage() string { return defaultRestrictedCare }

// EmergencyGuidance 疑似急症的固定急救指引。
func EmergencyGuidance() string { return defaultEmergencyGuidance }

// ClarificationPrompt 澄清话术前缀。
func ClarificationPrompt() string { return defaultClarificationPrompt }

// ClarificationFallback 需澄清但模型未给出具体问题时的兜底话术。
func ClarificationFallback() string { return defaultClarificationFallback }

// OutOfDomainMessage 域外问题（与健康宣教完全无关）的固定边界话术。
func OutOfDomainMessage() string { return defaultOutOfDomain }

// AssessmentFailedMessage 审查不可用时的固定兜底话术。
func AssessmentFailedMessage() string { return defaultAssessmentFailed }
