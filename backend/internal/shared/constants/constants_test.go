// constants 包单元测试：验证 DefaultSystemPrompt 包含必要约束规则。
package constants

import "testing"

// TestDefaultSystemPrompt_Constraints 验证系统提示词包含关键行为约束。
// 规则 6) 禁止客套话开头；规则 7) 无关资料不得强行作答。
func TestDefaultSystemPrompt_Constraints(t *testing.T) {
	t.Run("包含禁止客套话开头约束", func(t *testing.T) {
		// 规则 6：直接回答问题，不要以"好的"等客套话开头
		if !contains(DefaultSystemPrompt, "不要以") {
			t.Errorf("DefaultSystemPrompt 应包含 \"不要以\" 约束（禁止客套话开头），实际: %s", DefaultSystemPrompt)
		}
	})

	t.Run("包含无关资料不得作答约束", func(t *testing.T) {
		// 规则 7：参考资料与用户问题无关时不得强行作答
		if !contains(DefaultSystemPrompt, "无关") {
			t.Errorf("DefaultSystemPrompt 应包含 \"无关\" 约束（无关资料不得作答），实际: %s", DefaultSystemPrompt)
		}
	})
}

// contains 判断 s 是否包含 substr，避免引入 strings 包依赖。
func contains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
