package llm

import (
	"testing"
)

func TestNormalizeBaseURL_AppendsV1(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		isFullURL bool
		want      string
	}{
		{"empty", "", false, ""},
		{"already v1", "https://api.openai.com/v1", false, "https://api.openai.com/v1"},
		{"trailing slash v1", "https://api.openai.com/v1/", false, "https://api.openai.com/v1"},
		{"missing v1", "https://api.siliconflow.cn", false, "https://api.siliconflow.cn/v1"},
		{"missing v1 trailing slash", "https://api.siliconflow.cn/", false, "https://api.siliconflow.cn/v1"},
		{"with path no v1", "https://api.example.com/llm", false, "https://api.example.com/llm/v1"},
		{"with path trailing slash", "https://api.example.com/llm/", false, "https://api.example.com/llm/v1"},
		{"v1beta not matched", "https://api.example.com/v1beta", false, "https://api.example.com/v1beta/v1"},
		// 已含版本层的 base URL（如智谱 /api/paas/v4）不再追加 /v1，避免拼出 /v4/v1 404
		{"zhipu v4 no full url", "https://open.bigmodel.cn/api/paas/v4", false, "https://open.bigmodel.cn/api/paas/v4"},
		{"zhipu v4 trailing slash", "https://open.bigmodel.cn/api/paas/v4/", false, "https://open.bigmodel.cn/api/paas/v4"},
		{"api v2", "https://api.example.com/v2", false, "https://api.example.com/v2"},
		// 完整路径截断：用户粘贴了带 /chat/completions 的完整 URL
		{"full chat completions path", "https://apihub.agnes-ai.com/v1/chat/completions", false, "https://apihub.agnes-ai.com/v1"},
		{"full chat completions trailing slash", "https://apihub.agnes-ai.com/v1/chat/completions/", false, "https://apihub.agnes-ai.com/v1"},
		{"full embeddings path", "https://api.openai.com/v1/embeddings", false, "https://api.openai.com/v1"},
		{"full completions path", "https://api.openai.com/v1/completions", false, "https://api.openai.com/v1"},
		{"full rerank path", "https://api.example.com/v1/rerank", false, "https://api.example.com/v1"},
		// 无 /v1 但有完整路径
		{"no v1 but chat completions", "https://api.example.com/chat/completions", false, "https://api.example.com/v1"},
		// 完整链接开关：用户声明填到版本层，原样使用
		{"full url v1", "https://api.openai.com/v1", true, "https://api.openai.com/v1"},
		{"full url zhipu v4", "https://open.bigmodel.cn/api/paas/v4", true, "https://open.bigmodel.cn/api/paas/v4"},
		{"full url v4 trailing slash", "https://open.bigmodel.cn/api/paas/v4/", true, "https://open.bigmodel.cn/api/paas/v4"},
		{"full url host only kept as-is", "https://api.example.com", true, "https://api.example.com"},
		{"full url host with path kept as-is", "https://api.example.com/llm", true, "https://api.example.com/llm"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeBaseURL(tt.in, tt.isFullURL)
			if got != tt.want {
				t.Errorf("normalizeBaseURL(%q, %v) = %q, want %q", tt.in, tt.isFullURL, got, tt.want)
			}
		})
	}
}
