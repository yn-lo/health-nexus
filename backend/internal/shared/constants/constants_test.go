// constants 包单元测试：验证系统提示词分层与护栏不被破坏。
package constants

import (
	"strings"
	"testing"
)

// TestSystemPromptGuardrails 验证护栏包含关键行为约束。
// 这些规则是代码独占的（管理端不可改），因此是硬性断言：任何一条被删都是护栏退化。
func TestSystemPromptGuardrails(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"禁止客套话开头", "不要以"},
		{"无关资料不得强行作答", "无关"},
		{"参考资料编号引用", "[资料"},
		{"禁用诊断/开方/停药", "不得诊断"},
		{"禁止建议推迟就医", "推迟或放弃就医"},
		{"手机端列表不嵌套", "禁止嵌套子列表"},
		{"面向患者解释术语", "通俗解释"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(SystemPromptGuardrails, c.want) {
				t.Errorf("SystemPromptGuardrails 缺少 %q 约束，实际: %s", c.want, SystemPromptGuardrails)
			}
		})
	}
}

// TestComposeSystemPrompt 验证拼装语义：人格在前、护栏恒存、空人格回落默认。
func TestComposeSystemPrompt(t *testing.T) {
	t.Run("空人格回落默认人格", func(t *testing.T) {
		got := ComposeSystemPrompt("")
		if !strings.HasPrefix(got, SystemPromptPersona) {
			t.Errorf("应以默认人格开头，实际: %s", got)
		}
		if !strings.Contains(got, SystemPromptGuardrails) {
			t.Error("必须包含代码护栏")
		}
	})

	t.Run("自定义人格替换默认人格但护栏不变", func(t *testing.T) {
		got := ComposeSystemPrompt("  你是中医科宣教助手。  ")
		if !strings.HasPrefix(got, "你是中医科宣教助手。") {
			t.Errorf("应以自定义人格开头，实际: %s", got)
		}
		if strings.Contains(got, SystemPromptPersona) {
			t.Error("自定义人格应替换默认人格，不得同时出现")
		}
		if !strings.Contains(got, SystemPromptGuardrails) {
			t.Error("自定义人格不得覆盖代码护栏")
		}
	})
}
