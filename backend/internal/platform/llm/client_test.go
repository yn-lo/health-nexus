package llm

import (
	"testing"
)

func TestNormalizeBaseURL_AppendsV1(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"already v1", "https://api.openai.com/v1", "https://api.openai.com/v1"},
		{"trailing slash v1", "https://api.openai.com/v1/", "https://api.openai.com/v1"},
		{"missing v1", "https://api.siliconflow.cn", "https://api.siliconflow.cn/v1"},
		{"missing v1 trailing slash", "https://api.siliconflow.cn/", "https://api.siliconflow.cn/v1"},
		{"with path no v1", "https://api.example.com/llm", "https://api.example.com/llm/v1"},
		{"with path trailing slash", "https://api.example.com/llm/", "https://api.example.com/llm/v1"},
		{"v1beta not matched", "https://api.example.com/v1beta", "https://api.example.com/v1beta/v1"},
		// 完整路径截断：用户粘贴了带 /chat/completions 的完整 URL
		{"full chat completions path", "https://apihub.agnes-ai.com/v1/chat/completions", "https://apihub.agnes-ai.com/v1"},
		{"full chat completions trailing slash", "https://apihub.agnes-ai.com/v1/chat/completions/", "https://apihub.agnes-ai.com/v1"},
		{"full embeddings path", "https://api.openai.com/v1/embeddings", "https://api.openai.com/v1"},
		{"full completions path", "https://api.openai.com/v1/completions", "https://api.openai.com/v1"},
		{"full rerank path", "https://api.example.com/v1/rerank", "https://api.example.com/v1"},
		// 无 /v1 但有完整路径
		{"no v1 but chat completions", "https://api.example.com/chat/completions", "https://api.example.com/v1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeBaseURL(tt.in)
			if got != tt.want {
				t.Errorf("normalizeBaseURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
