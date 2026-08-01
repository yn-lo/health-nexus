package rag

import (
	"context"
	"strings"
	"unicode"

	"health-nexus/internal/shared/constants"
)

// Decision 输入安全审查决策。
type Decision int

const (
	// DecisionAllow 放行（规则层未命中）。
	DecisionAllow Decision = iota
	// DecisionBlock 拦截（规则层命中：危机 / 注入）。
	DecisionBlock
)

// Crisis 命中危机关键词后的上下文，用于创建 CrisisEvent。
type Crisis struct {
	Keywords []string
	Level    string // constants.CrisisLevel*
}

// SensitiveWords 三类敏感词，对应 sensitive_words 表的 category。
type SensitiveWords struct {
	Suicide   []string // 自杀/自残/想死（→ crisis 事件）
	Emergency []string // 胸痛/呼吸困难/大出血（→ 追加紧急就医提醒）
	Injection []string // Prompt 注入（→ 拦截）
}

// SafetyRuleProvider 跨域：config 域实现（阶段 2 注入）。
// 提供敏感词与各安全话术；阶段 1 由 DefaultInputSafetyFilter 内置默认值兜底。
// 已废弃：CrisisHotline（合并到 CrisisResponse）/ MedicationDisclaimer（合并到 SafetyWarningMessage）。
type SafetyRuleProvider interface {
	SensitiveWords(ctx context.Context) (SensitiveWords, error)
	RejectionMessage() string
	NoKnowledgeMessage() string
	SystemErrorMessage() string
	EmergencyMessage() string
	SafetyWarningMessage() string
	CrisisResponse() string
	OutputSafetyRules(ctx context.Context) ([]OutputSafetyRule, error)      // 输出审查规则
	OutputSafetyMessages(ctx context.Context) (OutputSafetyMessages, error) // 输出审查话术
}

// SystemPromptProvider 跨域：config 域适配器实现。
// 提供当前生效的 RAG 系统提示词（从 prompt_templates 表 type='system' 且 is_active=true 读取）。
// ISP 原则：消费者（chat 域）定义接口，config 域不得反向依赖 chat 域。
type SystemPromptProvider interface {
	// GetSystemPrompt 返回当前生效的系统提示词。
	// 实现应从 prompt_templates 表读取 is_active=true 的 SYSTEM 类型模板（全局/无科室优先）。
	// 任何错误或无数据时返回空字符串 + nil，让调用方降级为硬编码默认提示词（D-HIGH-01 修复）。
	GetSystemPrompt(ctx context.Context) (string, error)
}

// LLMSafetyChecker 跨域：由 platform/llm 适配器实现。
// 用于输入侧 LLM 深度审查：规则层未命中时，对疑似风险输入做 LLM 二次确认（REQ-CHAT-007）。
// 简化策略——只暴露"是否安全"的 bool 判定，避免 chat 域耦合具体 prompt/响应结构。
type LLMSafetyChecker interface {
	// IsInputSafe 调用 LLM 判定输入是否安全。
	// 任何错误（LLM 不可用/超时/解析失败）应返回 (true, nil)——fail-open 让流程继续，避免 LLM 故障阻断所有问答。
	IsInputSafe(ctx context.Context, message string) (bool, error)
}

// InputSafetyFilter 输入侧安全审查：规则层（零延迟）+ LLM 层（疑似复核）。
// REQ-CHAT-007~011。
// 已废弃：CrisisHotline（合并到 CrisisResponse）/ MedicationDisclaimer（合并到 SafetyWarningMessage）。
type InputSafetyFilter interface {
	// CheckRules 规则层审查。返回决策与（命中危机时的）Crisis 上下文。
	CheckRules(ctx context.Context, message string) (Decision, *Crisis)
	// LLMCheck LLM 层深度审查。规则层未命中时调用，疑似风险才拒绝（REQ-CHAT-007）。
	// 未注入 LLMSafetyChecker 或 LLM 故障时 fail-open 返回 true。
	LLMCheck(ctx context.Context, message string) bool
	// EmergencyCheck 检测紧急症状关键词命中（不拦截，仅作为推送 safety_warning 的信号）。
	// 由 Service 在 token 流之前调用，决定是否推送紧急就医提醒（REQ-CHAT-010）。
	EmergencyCheck(ctx context.Context, message string) []string
	// CrisisResponse 危机话术（命中自杀/自残关键词时返回给患者），包含心理援助热线。
	CrisisResponse() string
	// RejectionMessage 拒答话术（注入 / LLM 审查拒绝）。
	RejectionMessage() string
	// NoKnowledgeMessage 无知识话术（检索 0 结果 / 检索失败）。
	NoKnowledgeMessage() string
	// SystemErrorMessage 系统异常话术（LLM 空输出 / 流中断）。
	SystemErrorMessage() string
	// EmergencyMessage 紧急就医话术（命中紧急症状关键词时追加）。
	EmergencyMessage() string
	// SafetyWarningMessage 安全警告话术（涉及用药时追加，合并了原 medication_disclaimer）。
	SafetyWarningMessage() string
}

// 默认安全话术。对应 SafetyMessages 表的内置兜底值（REQ-NFR-017）。
const (
	defaultRejection      = "抱歉，我无法回答这个问题，建议您咨询您的主治医生。"
	defaultEmergency      = "您描述的症状需要紧急就医，请立即前往最近的医院急诊科或拨打 120。"
	defaultSafetyWarning  = "请注意：以上信息仅供参考，不能替代专业医疗诊断和治疗。用药请严格遵照医嘱，如有疑问请咨询您的主治医生或药师。"
	defaultCrisisResponse = "如果您感到痛苦或绝望，请立即联系心理援助热线 400-161-9995 或前往最近医院精神科就诊。您不是一个人，专业的帮助一直都在。"
	defaultNoKnowledge    = "抱歉，知识库中暂无与您问题相关的内容，建议您咨询主治医生或换个问法试试。"
	defaultSystemError    = "抱歉，系统暂时繁忙未能生成回答，请稍后重试。"
)

// defaultSensitiveWords 阶段 1 内置敏感词。阶段 2 由 config 域 SafetyRuleProvider 覆盖。
// ponytail: 硬编码关键词集合——覆盖医疗宣教场景常见表达，命中率高，折中；
// 升级路径：阶段 2 由 config 域 SafetyRuleProvider 注入 DB 配置。
var defaultSensitiveWords = SensitiveWords{
	Suicide: []string{
		"自杀", "自残", "想死", "寻死", "轻生", "结束生命", "不想活", "割腕",
		"了结自己", "了结此生", "活不下去", "了结余生",
		"s1", "离开这个世", "去另一个世界", "解脱",
		"kill myself", "suicide", "suicidal", "end my life",
		"want to die", "self-harm", "self harm",
	},
	Emergency: []string{
		"胸痛", "呼吸困难", "大出血", "咯血", "呕血", "昏迷", "休克", "抽搐",
		"持续高烧", "剧烈头痛", "意识不清",
	},
	Injection: []string{
		"忽略之前指令", "忽略上面的", "忽略以上", "忘记之前的", "忽略前文",
		"dan模式", "dan 模式", "jailbreak", "越狱",
		"prompt injection", "system prompt", "你是ai", "你是一个ai",
		"ignore previous", "ignore above", "ignore prior",
		"disregard", "forget your", "override",
		"act as", "roleplay", "pretend to be", "as an ai",
		"新的指令", "new instructions",
	},
}

// negationWords 中文否定词，用于 Suicide 关键词的二次过滤（REQ-CHAT-011）。
// ponytail: 不引入分词依赖——前 12 rune 窗口扫描已覆盖常见"我没有自杀想法"等表述，简化；
// 上限——超长前缀的复杂否定（如"经过长期思考我确定并非要自杀"中"并非"距关键词 >12 rune）会漏判，
// 升级路径：引入中文分词后改用依存句法分析。
// 未覆盖"不"单字——"不想死"/"不愿寻死"这类短否定会被误判为 Block；
// ponytail: 单字"不"会与"不时"/"不过"等非否定副词误匹配导致漏报，救命场景宁可误报不可漏报，折中。
var negationWords = []string{"没有", "不曾", "不会", "不想", "并非", "并未", "无"}

// hasNegationBeforeKeyword 检查关键词的所有出现位置前 12 rune 窗口内是否均含否定词。
// 仅当所有出现都有否定前缀时才返回 true（排除该关键词）；任一出现无否定即确认危机。
// 救命场景宁可误报不可漏报——多次出现时只要有一处无否定就视为真实意图。
// ponytail: 12 rune 窗口约覆盖 12 个中文字符；
// 上限——超长前缀的复杂否定（"并非"距关键词 >12 rune）仍会漏判，升级路径：引入中文分词后改用依存句法分析。
func hasNegationBeforeKeyword(message, keyword string) bool {
	if keyword == "" {
		return false
	}
	searchFrom := 0
	found := false
	for {
		idx := strings.Index(message[searchFrom:], keyword)
		if idx < 0 {
			return found
		}
		found = true
		absIdx := searchFrom + idx
		if !hasNegationAtPosition(message, absIdx) {
			return false
		}
		searchFrom = absIdx + len(keyword)
	}
}

// hasNegationAtPosition 检查 message 中 byteOffset 位置前的 12 rune 窗口内是否含否定词。
func hasNegationAtPosition(message string, byteOffset int) bool {
	if byteOffset <= 0 {
		return false
	}
	runes := []rune(message[:byteOffset])
	window := 12
	start := len(runes) - window
	if start < 0 {
		start = 0
	}
	prefix := string(runes[start:])
	for _, n := range negationWords {
		if strings.Contains(prefix, n) {
			return true
		}
	}
	return false
}

// DefaultInputSafetyFilter 默认实现：内置关键词 + 内置话术 + 可选 LLM 深度审查。
// 当 provider 非 nil 时优先使用 provider 的关键词（话术始终走内置默认，避免空值）。
// llmChecker 可为 nil：nil 时 LLMCheck 降级为始终放行（与原阶段 1 行为一致）。
type DefaultInputSafetyFilter struct {
	provider   SafetyRuleProvider
	llmChecker LLMSafetyChecker
}

// NewDefaultInputSafetyFilter 构造默认输入安全过滤器。
// provider 可为 nil：nil 时使用内置默认关键词。
// llmChecker 可为 nil：nil 时 LLMCheck 降级为始终放行（D-HIGH-03 降级策略）。
func NewDefaultInputSafetyFilter(provider SafetyRuleProvider, llmChecker LLMSafetyChecker) *DefaultInputSafetyFilter {
	return &DefaultInputSafetyFilter{provider: provider, llmChecker: llmChecker}
}

// CheckRules 规则层审查。流程：
//  1. 危机关键词命中 → Block + Crisis(high)
//  2. Prompt 注入命中 → Block
//  3. 紧急症状命中 → Allow（不拦截，由 Service 在最终回答中追加紧急就医提醒）
//
// Suicide 关键词走否定词二次过滤（REQ-CHAT-011）："我没有自杀想法"不应被拦截。
// hasNegationBeforeKeyword 仅扫描关键词前的字符窗口，不会因关键词自身含否定字（如"不想活"以"不想"开头）而误判——
// prefix 切片不包含关键词本身，不存在"自否定过滤"问题。
// 自杀场景的漏报代价（不推送心理援助热线，患者可能自杀）远高于误报代价（多余援助推送），
// 与 EmergencyCheck 同理——救命场景宁可误报不可漏报。
func (f *DefaultInputSafetyFilter) CheckRules(ctx context.Context, message string) (Decision, *Crisis) {
	words := f.words(ctx)

	if hit := matchAny(message, words.Suicide); len(hit) > 0 {
		// 否定词二次过滤：关键词前 12 rune 窗口内含否定词时跳过该关键词。
		var confirmed []string
		for _, kw := range hit {
			if !hasNegationBeforeKeyword(message, kw) {
				confirmed = append(confirmed, kw)
			}
		}
		if len(confirmed) > 0 {
			return DecisionBlock, &Crisis{Keywords: confirmed, Level: constants.CrisisLevelHigh}
		}
	}

	if hit := matchAny(message, words.Injection); len(hit) > 0 {
		return DecisionBlock, nil
	}

	// 紧急症状仅作信号，不拦截：由 Service 在 done 事件追加 emergency_message。
	// 这里通过返回 DecisionAllow 让流程继续；Service 可独立调用 EmergencyCheck 获取命中词。
	return DecisionAllow, nil
}

// LLMCheck LLM 层深度审查（REQ-CHAT-007）。
// 仅疑似风险输入才触发 LLM 复核，避免每条消息都过 LLM 增加延迟和成本。
// 未注入 llmChecker 时降级为放行（与阶段 1 行为一致）。
// ponytail: fail-open 策略——LLM 故障/超时/解析失败时放行，依赖规则层（CheckRules）兜底，折中；
// 已知上限——LLM 服务故障时输入全放行，仅规则层关键词拦截生效；
// 升级路径：在 di 层包装断路器，连续失败时熔断并降级到规则层 + 告警。
func (f *DefaultInputSafetyFilter) LLMCheck(ctx context.Context, message string) bool {
	if f.llmChecker == nil {
		return true // 降级：未注入 LLM 时放行（与原行为一致）
	}
	// REQ-CHAT-007：规则层未命中时不触发 LLM 审查（仅疑似风险才复核）。
	if !f.isSuspiciousInput(message) {
		return true // 无疑似信号，跳过 LLM，节省延迟和成本
	}
	safe, err := f.llmChecker.IsInputSafe(ctx, message)
	if err != nil {
		return true // fail-open
	}
	return safe
}

// suspiciousFragments 部分风险信号片段——规则层完整关键词未命中时，
// 含这些子串视为"疑似"，触发 LLM 复核。
// ponytail: 硬编码少量高频片段——覆盖"去死/想死/割腕"（自杀类）、"忽略/忘记/指令"（注入类）、
// "过量/毒"（医疗风险类），命中率与误触率的折中；
// 单字"死"已替换为更具体的"去死""想死"，避免"困死了""笑死"等正常口语误触 LLM 调用；
// 上限——片段仍可能在少数正常语境中误触，但 LLM 会判定为安全并放行，代价仅是一次额外 LLM 调用；
// 升级路径：阶段 2 由 config 域 SafetyRuleProvider 注入可配置片段列表，或引入轻量分类模型替代。
var suspiciousFragments = []string{
	"去死", "想死", "割腕", // 自杀/自残类部分信号（避免单字"死"误触）
	"忽略", "忘记", "指令", // Prompt 注入类部分信号
	"过量", "毒", // 医疗风险类部分信号
}

// isSuspiciousInput 检查消息是否含部分风险信号（未触发规则层完整匹配但值得 LLM 复核）。
// 归一化后匹配，与 matchAny 同路，避免"忽 略"等插入字符绕过疑似检测。
func (f *DefaultInputSafetyFilter) isSuspiciousInput(message string) bool {
	norm := normalizeForMatch(message)
	for _, frag := range suspiciousFragments {
		if strings.Contains(norm, frag) {
			return true
		}
	}
	return false
}

// EmergencyCheck 检查是否命中紧急症状关键词。返回命中的关键词切片。
// 由 Service 在 done 事件前调用，决定是否追加 emergency_message。
// 用 matchAny——否定词"避免/防止/拒绝"对紧急症状语义反转：
// "避免胸痛发作""防止大出血"暗示患者已有该症状，若被否定过滤则漏报急救，后果比误报严重得多。
// 救命场景（Emergency/Suicide 短关键词）均宁可误报不可漏报；
// Suicide 长关键词走 hasNegationBeforeKeyword 二次过滤，Emergency 不走（紧急症状无关键词自否定问题）。
func (f *DefaultInputSafetyFilter) EmergencyCheck(ctx context.Context, message string) []string {
	return matchAny(message, f.words(ctx).Emergency)
}

func (f *DefaultInputSafetyFilter) words(ctx context.Context) SensitiveWords {
	if f.provider != nil {
		// provider 失败或某分类为空时降级使用默认（REQ-NFR-017）。
		// ponytail: 分类级降级——provider 配置了 Suicide 就用它的，没配 Emergency 就用默认，折中，
		// 避免管理员未配置某分类时该分类安全审查完全失效。
		if w, err := f.provider.SensitiveWords(ctx); err == nil {
			if len(w.Suicide) == 0 {
				w.Suicide = defaultSensitiveWords.Suicide
			}
			if len(w.Emergency) == 0 {
				w.Emergency = defaultSensitiveWords.Emergency
			}
			if len(w.Injection) == 0 {
				w.Injection = defaultSensitiveWords.Injection
			}
			return w
		}
	}
	return defaultSensitiveWords
}

// CrisisResponse 实现 InputSafetyFilter。
func (f *DefaultInputSafetyFilter) CrisisResponse() string {
	if f.provider != nil {
		return f.provider.CrisisResponse()
	}
	return defaultCrisisResponse
}

// RejectionMessage 实现 InputSafetyFilter。
func (f *DefaultInputSafetyFilter) RejectionMessage() string {
	if f.provider != nil {
		return f.provider.RejectionMessage()
	}
	return defaultRejection
}

// NoKnowledgeMessage 实现 InputSafetyFilter。
func (f *DefaultInputSafetyFilter) NoKnowledgeMessage() string {
	if f.provider != nil {
		return f.provider.NoKnowledgeMessage()
	}
	return defaultNoKnowledge
}

// SystemErrorMessage 实现 InputSafetyFilter。
func (f *DefaultInputSafetyFilter) SystemErrorMessage() string {
	if f.provider != nil {
		return f.provider.SystemErrorMessage()
	}
	return defaultSystemError
}

// EmergencyMessage 实现 InputSafetyFilter。
func (f *DefaultInputSafetyFilter) EmergencyMessage() string {
	if f.provider != nil {
		return f.provider.EmergencyMessage()
	}
	return defaultEmergency
}

// SafetyWarningMessage 实现 InputSafetyFilter（合并了原 medication_disclaimer 语义）。
func (f *DefaultInputSafetyFilter) SafetyWarningMessage() string {
	if f.provider != nil {
		return f.provider.SafetyWarningMessage()
	}
	return defaultSafetyWarning
}

// normalizeForMatch 安全关键词匹配前的输入归一化：小写 + 剥离空白（含全角空格）
// 与零宽字符，防御"自 杀""自\u200b杀"等插入字符绕过（E2E EDGE-SAFE-002/003 修复）。
// 关键词与消息同路归一化，含空格的英文短语（"kill myself"）在剥离后仍可匹配。
// ponytail: 仅覆盖空白与零宽字符两类最常见插入绕过；
// 上限——谐音/拼音/emoji 替代（"我想 s1"）仍依赖 LLM 层与系统提示词兜底；
// 升级路径：引入拼音转换或轻量分类模型。
func normalizeForMatch(s string) string {
	lower := strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(lower))
	for _, r := range lower {
		if unicode.IsSpace(r) || r == '\u200b' || r == '\u200c' || r == '\u200d' || r == '\ufeff' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// matchAny 在 message 中扫描 keywords，返回命中的关键词（去重）。
// 大小写不敏感（英文注入词匹配需要）；归一化后匹配，空格/零宽字符插入不绕过。
func matchAny(message string, keywords []string) []string {
	if len(keywords) == 0 {
		return nil
	}
	norm := normalizeForMatch(message)
	seen := map[string]struct{}{}
	var hit []string
	for _, kw := range keywords {
		if kw == "" {
			continue
		}
		if strings.Contains(norm, normalizeForMatch(kw)) {
			if _, ok := seen[kw]; !ok {
				hit = append(hit, kw)
				seen[kw] = struct{}{}
			}
		}
	}
	return hit
}
