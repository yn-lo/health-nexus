package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"health-nexus/internal/config"
)

// TestSanitizeChatRequest 流式问答出站脱敏：患者来源文本（当前问题 + 历史）被脱敏，
// 系统提示词与知识库切片保持原样，且不修改入参。
func TestSanitizeChatRequest(t *testing.T) {
	const (
		rawMessage = "我的电话是13812345678，怎么降压"
		rawIDCard  = "身份证110101199001011234"
		rawSystem  = "你是健康宣教助手"
		rawChunk   = "高血压患者应低盐饮食"
		rawAssist  = "请咨询医生"
	)
	req := ChatRequest{
		SystemPrompt:  rawSystem,
		UserMessage:   rawMessage,
		ContextChunks: []string{rawChunk},
		History: []Message{
			{Role: "user", Content: rawIDCard},
			{Role: "assistant", Content: rawAssist},
		},
	}

	got := sanitizeChatRequest(req)

	if got.UserMessage != "我的电话是[手机号]，怎么降压" {
		t.Errorf("UserMessage 未脱敏: %q", got.UserMessage)
	}
	if got.History[0].Content != "身份证[身份证]" {
		t.Errorf("历史消息未脱敏: %q", got.History[0].Content)
	}
	if got.History[1].Content != rawAssist {
		t.Errorf("无 PII 的历史消息被意外修改: %q", got.History[1].Content)
	}
	if got.SystemPrompt != rawSystem {
		t.Errorf("SystemPrompt 不应参与脱敏: %q", got.SystemPrompt)
	}
	if got.ContextChunks[0] != rawChunk {
		t.Errorf("ContextChunks 不应参与脱敏: %q", got.ContextChunks[0])
	}
	if req.UserMessage != rawMessage || req.History[0].Content != rawIDCard {
		t.Errorf("不应修改入参: %q / %q", req.UserMessage, req.History[0].Content)
	}
}

// TestRerank_SanitizesQuery 出站请求体中 query 已脱敏；documents 为知识库切片，保持原样。
func TestRerank_SanitizesQuery(t *testing.T) {
	var gotBody rerankRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("解析出站请求体失败: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"t","results":[{"index":0,"relevance_score":1.0}]}`))
	}))
	defer server.Close()

	chat, hc := newOpenAIClient(server.URL, "test-key", 0, false)
	client := &Client{
		chat:       chat,
		httpClient: hc,
		cfg:        config.LLMConfig{BaseURL: server.URL + "/v1", ChatModel: "rerank-model"},
	}

	docs := []string{"高血压患者应低盐饮食"}
	if _, err := client.Rerank(context.Background(), "我电话13812345678，怎么降压", docs, 1); err != nil {
		t.Fatalf("Rerank() error: %v", err)
	}

	if gotBody.Query != "我电话[手机号]，怎么降压" {
		t.Errorf("出站 query 未脱敏: %q", gotBody.Query)
	}
	if len(gotBody.Documents) != 1 || gotBody.Documents[0] != docs[0] {
		t.Errorf("documents 不应被修改: %v", gotBody.Documents)
	}
}

// TestCompleteJSON_SanitizesUserContent 出站请求体中 user 消息已脱敏，system 提示词保持原样。
// 该路径承载统一理解与审查（Assessor）与生成后语义审核（OutputReviewer）的输入。
func TestCompleteJSON_SanitizesUserContent(t *testing.T) {
	var gotBody struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("解析出站请求体失败: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{}"}}]}`))
	}))
	defer server.Close()

	client, err := NewClient(config.LLMConfig{
		BaseURL:   server.URL,
		APIKey:    "test-key",
		ChatModel: "chat-model",
	})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	if _, err := client.CompleteJSON(context.Background(), "系统提示词", "我的电话13812345678", 0); err != nil {
		t.Fatalf("CompleteJSON() error: %v", err)
	}

	if len(gotBody.Messages) != 2 {
		t.Fatalf("期望 2 条出站消息，实际 %d 条: %v", len(gotBody.Messages), gotBody.Messages)
	}
	if gotBody.Messages[0].Content != "系统提示词" {
		t.Errorf("system 消息不应参与脱敏: %q", gotBody.Messages[0].Content)
	}
	if gotBody.Messages[1].Content != "我的电话[手机号]" {
		t.Errorf("user 消息未脱敏: %q", gotBody.Messages[1].Content)
	}
}
