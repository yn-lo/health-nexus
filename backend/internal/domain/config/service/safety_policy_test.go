package service

import (
	"context"
	"testing"

	"health-nexus/internal/domain/config/entity"
)

func TestGetSafetyPolicyReturnsActiveRulesAndSensitiveWordSources(t *testing.T) {
	words := newMockSensitiveWordRepo()
	words.items[1] = &entity.SensitiveWord{ID: 1, Category: "suicide", Word: "自定义危机", IsActive: true}
	words.items[2] = &entity.SensitiveWord{ID: 2, Category: "suicide", Word: "停用危机", IsActive: false}
	rules := newMockSafetyRuleRepo()
	rules.items[1] = &entity.SafetyRule{ID: 1, Category: "diagnosis", Pattern: "诊断", Action: "replace", Replacement: "替换", IsActive: true}
	rules.items[2] = &entity.SafetyRule{ID: 2, Category: "prescription", Pattern: "处方", Action: "block", IsActive: false}
	messages := newMockSafetyMessageRepo()
	messages.items = []*entity.SafetyMessage{{Type: entity.SafetyMessageTypeSafetyWarning, Content: "自定义安全警告"}}
	svc := newTestService(nil, words, rules, nil, nil, messages, nil)

	policy, err := svc.GetSafetyPolicy(context.Background())
	if err != nil {
		t.Fatalf("GetSafetyPolicy: %v", err)
	}
	if got := policy.InputSensitiveWords.Suicide; got.Source != "database" || len(got.Words) != 1 || got.Words[0] != "自定义危机" {
		t.Fatalf("输入敏感词应仅包含启用数据库值：%+v", got)
	}
	if len(policy.OutputRules) != 1 || policy.OutputRules[0].Category != "diagnosis" {
		t.Fatalf("输出规则应过滤 inactive：%+v", policy.OutputRules)
	}
	if policy.OutputRules[0].Source != "database" {
		t.Errorf("DB 规则 source 应为 database，实际: %s", policy.OutputRules[0].Source)
	}
	if policy.Messages.SafetyWarningMessage != "自定义安全警告" || policy.Messages.RejectionMessage == "" {
		t.Fatalf("最终话术应包含数据库值和回退：%+v", policy.Messages)
	}
}

func TestGetSafetyPolicyFallbackHardcodedRules(t *testing.T) {
	// 空 DB：敏感词使用默认值，输出规则使用硬编码 fallback，话术使用默认值
	svc := newTestService(nil, newMockSensitiveWordRepo(), newMockSafetyRuleRepo(), nil, nil, newMockSafetyMessageRepo(), nil)

	policy, err := svc.GetSafetyPolicy(context.Background())
	if err != nil {
		t.Fatalf("GetSafetyPolicy: %v", err)
	}
	// 敏感词应使用默认值
	if policy.InputSensitiveWords.Suicide.Source != "default" {
		t.Errorf("空 DB 时敏感词 source 应为 default，实际: %s", policy.InputSensitiveWords.Suicide.Source)
	}
	if len(policy.InputSensitiveWords.Suicide.Words) == 0 {
		t.Error("默认敏感词不应为空")
	}
	// 输出规则应使用硬编码 fallback
	if len(policy.OutputRules) == 0 {
		t.Fatal("空 DB 时应返回硬编码 fallback 输出规则")
	}
	if policy.OutputRules[0].Source != "hardcoded" {
		t.Errorf("fallback 规则 source 应为 hardcoded，实际: %s", policy.OutputRules[0].Source)
	}
	// 硬编码规则应包含4个类别
	categories := map[string]bool{}
	for _, r := range policy.OutputRules {
		categories[r.Category] = true
	}
	for _, cat := range []string{"stop_medication", "prescription", "diagnosis", "delay_medical"} {
		if !categories[cat] {
			t.Errorf("fallback 规则缺少 %s 类别", cat)
		}
	}
	// 话术应使用默认值
	if policy.Messages.RejectionMessage != DefaultSafetyMessages.RejectionMessage {
		t.Errorf("空 DB 时话术应使用默认值")
	}
}

func TestGetSafetyPolicyInactiveWordsExcluded(t *testing.T) {
	words := newMockSensitiveWordRepo()
	words.items[1] = &entity.SensitiveWord{ID: 1, Category: "emergency", Word: "活跃词", IsActive: true}
	words.items[2] = &entity.SensitiveWord{ID: 2, Category: "emergency", Word: "停用词", IsActive: false}
	svc := newTestService(nil, words, newMockSafetyRuleRepo(), nil, nil, newMockSafetyMessageRepo(), nil)

	policy, err := svc.GetSafetyPolicy(context.Background())
	if err != nil {
		t.Fatalf("GetSafetyPolicy: %v", err)
	}
	if policy.InputSensitiveWords.Emergency.Source != "database" {
		t.Errorf("有活跃词时 source 应为 database")
	}
	for _, w := range policy.InputSensitiveWords.Emergency.Words {
		if w == "停用词" {
			t.Error("停用词不应出现在结果中")
		}
	}
}
