package rag

import (
	"context"
	"regexp"
	"strings"
	"sync"
)

// OutputAction 输出审查动作。
type OutputAction int

const (
	OutputPass OutputAction = iota
	OutputReplace
	OutputAppendDisclaimer
)

// OutputResult 输出审查结果。
type OutputResult struct {
	Final   string
	Blocked bool
	Changed bool
	// Replaced 区分"替换越权内容为安全话术"与"完全拦截"：
	// Replaced=true 时内容被替换但仍可展示（如诊断→建议咨询医生），Blocked=true 仅表示内容被修改。
	// 完全拦截（block action）时 Replaced=false, Blocked=true。
	Replaced bool
}

// OutputSafetyRule 是输出安全审查的已启用规则。
type OutputSafetyRule struct {
	Category    string
	Pattern     string
	Action      string
	Replacement string
}

// OutputSafetyMessages 是输出审查实际使用的话术。
// MedicationDisclaimer 已合并到 SafetyWarningMessage。
type OutputSafetyMessages struct {
	RejectionMessage     string
	SafetyWarningMessage string
}

// OutputSafetyProvider 由 config 域适配器实现，避免 config 域依赖 rag。
type OutputSafetyProvider interface {
	OutputSafetyRules(ctx context.Context) ([]OutputSafetyRule, error)
	OutputSafetyMessages(ctx context.Context) (OutputSafetyMessages, error)
}

// OutputSafetyFilter 输出侧安全审查（REQ-CHAT-012~014）。
type OutputSafetyFilter interface {
	Validate(ctx context.Context, answer string) OutputResult
}

var (
	diagnosisRe          = regexp.MustCompile(`(?:(?:确诊|诊断)为|(?:初步)?(?:诊断|确诊)[是为：:]\s*[^，。；\s]+)`)
	prescriptionRe       = regexp.MustCompile(`(?:建议(?:服|用|服用|吃)\s*[^，。；\s]+\s*(?:片|丸|胶囊|药|mg|克))`)
	stopMedicationRe     = regexp.MustCompile(`(?:建议(?:立即)?(?:停[药服止]|停止服药))`)
	delayMedicalRe       = regexp.MustCompile(`(?:不建议(?:去|到)?(?:医院|就医)|不需要(?:去)?(?:医院|就医))`)
	medicationMentionRe  = regexp.MustCompile(`(?:\d+\s*(?:mg|克|片|丸|胶囊)|剂量|用量|每日.*次)`)
	diagnosisExceptionRe = regexp.MustCompile(`(?:无法确诊|不能确诊|建议.*确诊|请.*医生.*确诊|疑似|诊断为准)`)
)

const (
	diagnosisReplacement      = "（具体诊断请咨询您的主治医生，AI 不能替代临床诊断。）"
	prescriptionReplacement   = "（用药建议请遵医嘱，切勿自行用药。）"
	stopMedicationReplacement = "（是否停药请咨询您的主治医生，切勿自行停药。）"
	delayMedicalReplacement   = "（如症状持续或加重，请及时就医，避免延误病情。）"
	// sentenceEndChars 诊断例外检查的句子边界（与 chat 域流式审查的分句边界一致）。
	sentenceEndChars = "。！？!?；;\n"
)

var fallbackOutputRules = []OutputSafetyRule{
	{
		Category:    "stop_medication",
		Pattern:     stopMedicationRe.String(),
		Action:      "replace",
		Replacement: stopMedicationReplacement,
	},
	{
		Category:    "prescription",
		Pattern:     prescriptionRe.String(),
		Action:      "replace",
		Replacement: prescriptionReplacement,
	},
	{
		Category:    "diagnosis",
		Pattern:     diagnosisRe.String(),
		Action:      "replace",
		Replacement: diagnosisReplacement,
	},
	{
		Category:    "delay_medical",
		Pattern:     delayMedicalRe.String(),
		Action:      "replace",
		Replacement: delayMedicalReplacement,
	},
}

// FallbackOutputRules 返回硬编码的输出审查 fallback 规则。
// 供 config 域 GetSafetyPolicy 在 DB 无活跃规则时展示给前端。
func FallbackOutputRules() []OutputSafetyRule {
	return fallbackOutputRules
}

// compiledOutputRule 预编译的输出审查规则，避免每次 Validate 调用时重新编译正则。
type compiledOutputRule struct {
	Category    string
	Re          *regexp.Regexp
	Action      string
	Replacement string
}

// DefaultOutputSafetyFilter 阶段 1 默认实现：正则模式匹配 + 内置话术替换。
// provider 非 nil 时，使用 provider 的 RejectionMessage 和 SafetyWarningMessage；
// provider 为 nil 时降级为硬编码默认值（与原阶段 1 行为一致）。
type DefaultOutputSafetyFilter struct {
	provider      SafetyRuleProvider
	fallbackRules []compiledOutputRule

	mu          sync.Mutex
	cachedRules []compiledOutputRule
	cachedRaw   []OutputSafetyRule
}

// NewDefaultOutputSafetyFilter 构造默认输出安全过滤器。
// provider 可为 nil：nil 时使用内置默认话术。
func NewDefaultOutputSafetyFilter(provider SafetyRuleProvider) *DefaultOutputSafetyFilter {
	return &DefaultOutputSafetyFilter{
		provider:      provider,
		fallbackRules: compileRules(fallbackOutputRules),
	}
}

// compileRules 预编译规则列表中的正则。
func compileRules(rules []OutputSafetyRule) []compiledOutputRule {
	out := make([]compiledOutputRule, 0, len(rules))
	for _, r := range rules {
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			continue
		}
		out = append(out, compiledOutputRule{
			Category:    r.Category,
			Re:          re,
			Action:      r.Action,
			Replacement: r.Replacement,
		})
	}
	return out
}

// rejectionText 返回当前生效的拒答话术。
func (f *DefaultOutputSafetyFilter) rejectionText() string {
	if f.provider != nil {
		if r := f.provider.RejectionMessage(); r != "" {
			return r
		}
	}
	return defaultRejection
}

// safetyWarningText 返回当前生效的安全警告话术。
func (f *DefaultOutputSafetyFilter) safetyWarningText() string {
	if f.provider != nil {
		if w := f.provider.SafetyWarningMessage(); w != "" {
			return w
		}
	}
	return defaultSafetyWarning
}

// outputCategoryPriority 输出审查类别优先级（停药 > 处方 > 诊断 > 延误就医 > 其他）。
var outputCategoryPriority = []string{"stop_medication", "prescription", "diagnosis", "delay_medical", "other"}

// Validate 输出侧安全审查。
// 两轮处理：先在**原文**上判定所有规则命中（含诊断例外判定），再按优先级依次应用替换。
//   - 遍历全部规则而非命中首条即返回：否则一条规则命中就返回会让其他违规内容
//     （如同时出现停药 + 诊断 + 延误就医）原样流出。
//   - 判定必须基于原文：边判定边替换时，先应用的替换话术（如"请咨询您的主治医生"）
//     会落进后续规则的例外窗口，把真实违规误判成例外而放行。
func (f *DefaultOutputSafetyFilter) Validate(ctx context.Context, answer string) OutputResult {
	if answer == "" {
		return OutputResult{Final: answer}
	}
	rules := f.compiledPolicy(ctx)

	matched := make([]compiledOutputRule, 0, len(rules))
	for _, category := range outputCategoryPriority {
		for _, rule := range rules {
			if rule.Category != category {
				continue
			}
			loc := rule.Re.FindStringIndex(answer)
			if loc == nil {
				continue
			}
			if category == "diagnosis" && hasDiagnosisExceptionNear(answer, loc) {
				continue
			}
			// block 为终态动作：整篇替换为拒答话术，不再检查后续规则。
			// 拒答话术按需读取（话术来自远端配置，流式逐句审查时不应无谓回源）。
			if rule.Action == "block" {
				return OutputResult{Final: f.rejectionText(), Blocked: true, Changed: true}
			}
			matched = append(matched, rule)
		}
	}

	final := answer
	for _, rule := range matched {
		if next := rule.Re.ReplaceAllString(final, rule.Replacement); next != final {
			final = next
		}
	}
	if final != answer {
		return OutputResult{Final: final, Blocked: true, Changed: true, Replaced: true}
	}
	// 仅提及用药剂量（无越权内容）：追加安全警告，不拦截。
	if medicationMentionRe.MatchString(final) {
		warning := f.safetyWarningText()
		if warning != "" && !strings.Contains(final, warning) {
			final += "\n\n" + warning
		}
		return OutputResult{Final: final, Changed: final != answer}
	}
	return OutputResult{Final: answer}
}

// hasDiagnosisExceptionNear 判断诊断例外模式是否出现在匹配所在**句子**内。
// 例外只在同句内判定：跨句判定会让远处无关表述（如"建议…确诊"横跨另一个分句）掩盖近处真实违规。
// 使用 rune 边界避免截断 UTF-8 多字节字符。调用方须传入原文（替换话术会污染判定）。
func hasDiagnosisExceptionNear(answer string, loc []int) bool {
	runes := []rune(answer)
	matchStart := len([]rune(answer[:loc[0]]))
	matchEnd := matchStart + len([]rune(answer[loc[0]:loc[1]]))
	start := 0
	for i := matchStart - 1; i >= 0; i-- {
		if strings.ContainsRune(sentenceEndChars, runes[i]) {
			start = i + 1
			break
		}
	}
	end := len(runes)
	for i := matchEnd; i < len(runes); i++ {
		if strings.ContainsRune(sentenceEndChars, runes[i]) {
			end = i
			break
		}
	}
	if start >= end {
		return false
	}
	return diagnosisExceptionRe.MatchString(string(runes[start:end]))
}

// compiledPolicy 返回预编译的规则列表。provider 有 DB 规则时缓存编译结果（规则内容不变则复用），
// 避免每次 Validate 调用都重新编译正则（CPU 密集）。
func (f *DefaultOutputSafetyFilter) compiledPolicy(ctx context.Context) []compiledOutputRule {
	if f.provider != nil {
		rawRules, err := f.provider.OutputSafetyRules(ctx)
		if err == nil && len(rawRules) > 0 {
			f.mu.Lock()
			defer f.mu.Unlock()
			if rulesEqual(f.cachedRaw, rawRules) {
				return f.cachedRules
			}
			f.cachedRaw = rawRules
			f.cachedRules = compileRules(rawRules)
			return f.cachedRules
		}
	}
	return f.fallbackRules
}

// rulesEqual 比较两组规则是否内容一致（用于缓存失效判断）。
func rulesEqual(a, b []OutputSafetyRule) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Category != b[i].Category || a[i].Pattern != b[i].Pattern ||
			a[i].Action != b[i].Action || a[i].Replacement != b[i].Replacement {
			return false
		}
	}
	return true
}
