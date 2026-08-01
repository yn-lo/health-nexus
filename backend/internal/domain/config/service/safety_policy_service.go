package service

import (
	"context"

	"health-nexus/internal/shared/constants"
	"health-nexus/internal/shared/pagination"
	"health-nexus/internal/shared/rag"
)

// sensitiveWordsPageSize 敏感词/安全规则分页查询的单页大小。
const sensitiveWordsPageSize = 100

var defaultSafetyPolicyWords = map[string][]string{
	constants.SensitiveCategorySuicide: {
		"自杀", "自残", "想死", "寻死", "轻生", "结束生命", "不想活", "割腕", "了结自己", "了结此生", "活不下去", "了结余生",
	},
	constants.SensitiveCategoryEmergency: {
		"胸痛", "呼吸困难", "大出血", "咯血", "呕血", "昏迷", "休克", "抽搐", "持续高烧", "剧烈头痛", "意识不清",
	},
	constants.SensitiveCategoryInjection: {
		"忽略之前指令", "忽略上面的", "忽略以上", "忘记之前的", "忽略前文", "dan模式", "dan 模式", "jailbreak", "越狱",
		"prompt injection", "system prompt", "你是ai", "你是一个ai", "ignore previous", "ignore above", "ignore prior",
		"disregard", "forget your", "override", "act as", "roleplay", "pretend to be", "as an ai",
		"新的指令", "new instructions",
	},
}

type SafetyPolicyWords struct {
	Source string   `json:"source"`
	Words  []string `json:"words"`
}

type SafetyPolicyInputWords struct {
	Suicide   SafetyPolicyWords `json:"suicide"`
	Emergency SafetyPolicyWords `json:"emergency"`
	Injection SafetyPolicyWords `json:"injection"`
}

type SafetyPolicyOutputRule struct {
	Category    string `json:"category"`
	Pattern     string `json:"pattern"`
	Action      string `json:"action"`
	Replacement string `json:"replacement"`
	Source      string `json:"source"` // "database" 或 "hardcoded"
}

type SafetyPolicyResponse struct {
	InputSensitiveWords SafetyPolicyInputWords   `json:"input_sensitive_words"`
	OutputRules         []SafetyPolicyOutputRule `json:"output_rules"`
	Messages            SafetyMessagesResponse   `json:"messages"`
}

// GetSafetyPolicy 返回实际运行的安全策略：各类别空配置使用默认敏感词，inactive 内容不生效。
// 输出规则：DB 有活跃规则时使用 DB 规则（source=database），否则使用硬编码 fallback 规则（source=hardcoded）。
func (s *ConfigService) GetSafetyPolicy(ctx context.Context) (*SafetyPolicyResponse, error) {
	wordSets := make(map[string]SafetyPolicyWords, 3)
	for _, category := range []string{
		constants.SensitiveCategorySuicide,
		constants.SensitiveCategoryEmergency,
		constants.SensitiveCategoryInjection,
	} {
		words := []string{}
		for page := 1; ; page++ {
			list, total, err := s.ListSensitiveWords(
				ctx, category, pagination.Params{Page: page, PageSize: sensitiveWordsPageSize},
			)
			if err != nil {
				return nil, err
			}
			for _, word := range list {
				if word.IsActive {
					words = append(words, word.Word)
				}
			}
			if int64(page*sensitiveWordsPageSize) >= total || len(list) == 0 {
				break
			}
		}
		if len(words) == 0 {
			wordSets[category] = SafetyPolicyWords{Source: "default", Words: defaultSafetyPolicyWords[category]}
		} else {
			wordSets[category] = SafetyPolicyWords{Source: "database", Words: words}
		}
	}
	rules, _, err := s.ListSafetyRules(ctx, "", pagination.Params{Page: 1, PageSize: sensitiveWordsPageSize})
	if err != nil {
		return nil, err
	}
	var outputRules []SafetyPolicyOutputRule
	for _, rule := range rules {
		if rule.IsActive {
			outputRules = append(outputRules, SafetyPolicyOutputRule{
				Category: rule.Category, Pattern: rule.Pattern, Action: rule.Action,
				Replacement: rule.Replacement, Source: "database",
			})
		}
	}
	if len(outputRules) == 0 {
		for _, r := range rag.FallbackOutputRules() {
			outputRules = append(outputRules, SafetyPolicyOutputRule{
				Category: r.Category, Pattern: r.Pattern, Action: r.Action,
				Replacement: r.Replacement, Source: "hardcoded",
			})
		}
	}
	messages, err := s.GetSafetyMessages(ctx)
	if err != nil {
		return nil, err
	}
	return &SafetyPolicyResponse{
		InputSensitiveWords: SafetyPolicyInputWords{
			Suicide:   wordSets[constants.SensitiveCategorySuicide],
			Emergency: wordSets[constants.SensitiveCategoryEmergency],
			Injection: wordSets[constants.SensitiveCategoryInjection],
		},
		OutputRules: outputRules,
		Messages:    *messages,
	}, nil
}
