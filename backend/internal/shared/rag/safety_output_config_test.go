package rag

import (
	"context"
	"testing"
)

type configuredOutputSafetyProvider struct {
	rules    []OutputSafetyRule
	messages OutputSafetyMessages
	err      error
}

func (p configuredOutputSafetyProvider) SensitiveWords(context.Context) (SensitiveWords, error) {
	return SensitiveWords{}, nil
}
func (p configuredOutputSafetyProvider) OutputSafetyRules(context.Context) ([]OutputSafetyRule, error) {
	return p.rules, p.err
}
func (p configuredOutputSafetyProvider) OutputSafetyMessages(context.Context) (OutputSafetyMessages, error) {
	return p.messages, p.err
}
func (p configuredOutputSafetyProvider) RejectionMessage() string   { return p.messages.RejectionMessage }
func (p configuredOutputSafetyProvider) NoKnowledgeMessage() string { return "" }
func (p configuredOutputSafetyProvider) SystemErrorMessage() string { return "" }
func (p configuredOutputSafetyProvider) EmergencyMessage() string   { return "" }
func (p configuredOutputSafetyProvider) SafetyWarningMessage() string {
	return p.messages.SafetyWarningMessage
}
func (p configuredOutputSafetyProvider) CrisisResponse() string { return "" }

func TestOutputSafetyFilterUsesConfiguredRulesAndMessages(t *testing.T) {
	filter := NewDefaultOutputSafetyFilter(configuredOutputSafetyProvider{
		rules: []OutputSafetyRule{
			{Category: "diagnosis", Pattern: "自定义诊断", Action: "replace", Replacement: "自定义替换"},
			{Category: "prescription", Pattern: "自定义处方", Action: "block"},
		},
		messages: OutputSafetyMessages{
			RejectionMessage:     "自定义拒答",
			SafetyWarningMessage: "自定义安全提示（含用药免责）",
		},
	})

	if got := filter.Validate(context.Background(), "自定义诊断"); got.Final != "自定义替换" || !got.Blocked {
		t.Fatalf("replace 规则未生效：%+v", got)
	}
	if got := filter.Validate(context.Background(), "自定义处方"); got.Final != "自定义拒答" || !got.Blocked {
		t.Fatalf("block 规则未使用拒答话术：%+v", got)
	}
	if got := filter.Validate(context.Background(), "每天 100mg"); got.Final != "每天 100mg\n\n自定义安全提示（含用药免责）" {
		t.Fatalf("安全警告未生效：%q", got.Final)
	}
}

func TestOutputSafetyFilterPreservesCategoryPriorityForConfiguredRules(t *testing.T) {
	filter := NewDefaultOutputSafetyFilter(configuredOutputSafetyProvider{
		rules: []OutputSafetyRule{
			{Category: "diagnosis", Pattern: "风险", Action: "replace", Replacement: "诊断替换"},
			{Category: "stop_medication", Pattern: "风险", Action: "replace", Replacement: "停药替换"},
		},
		messages: OutputSafetyMessages{RejectionMessage: "拒答", SafetyWarningMessage: "安全警告"},
	})

	if got := filter.Validate(context.Background(), "风险"); got.Final != "停药替换" {
		t.Fatalf("应按停药优先：%+v", got)
	}
}
