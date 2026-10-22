package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sashabaranov/go-openai"

	"health-nexus/internal/config"
)

// newTestClient 构造指向 mock 服务的单 provider 客户端（无备用）。
func newTestClient(t *testing.T, handler http.HandlerFunc, model string) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, err := NewClient(config.LLMConfig{BaseURL: server.URL, APIKey: "test-key", ChatModel: model})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// TestStreamChat_FailoverBeforeFirstToken 主 provider 5xx 时切换到备用，患者只看到备用答案。
func TestStreamChat_FailoverBeforeFirstToken(t *testing.T) {
	primary := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"upstream down"}}`))
	}, "primary-model")

	var fallbackHits atomic.Int32
	fallback := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		fallbackHits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"备用\"},\"finish_reason\":null}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}, "fallback-model")

	chain := NewFailoverClient(primary, fallback)
	if chain.ProviderCount() != 2 {
		t.Fatalf("ProviderCount() = %d, want 2", chain.ProviderCount())
	}

	ch, err := chain.StreamChat(context.Background(), ChatRequest{UserMessage: "怎么降压"})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	var got string
	completed := false
	for part := range ch {
		if part.Err != nil {
			t.Fatalf("不应返回错误: %v", part.Err)
		}
		got += part.Token
		if part.Done && !part.Truncated {
			completed = true
		}
	}
	if got != "备用" {
		t.Errorf("答案 = %q, want %q", got, "备用")
	}
	if !completed {
		t.Error("应收到正常完成信号")
	}
	if fallbackHits.Load() == 0 {
		t.Error("备用 provider 未被调用")
	}
}

// TestStreamChat_NoFailoverAfterToken 已投递首个 token 后不得切换备用（否则答案由两个模型拼接）。
func TestStreamChat_NoFailoverAfterToken(t *testing.T) {
	primary := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"主用\"},\"finish_reason\":null}]}\n\n")
		// 无 finish_reason 直接断流
	}, "primary-model")

	var fallbackHits atomic.Int32
	fallback := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		fallbackHits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"备用\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}, "fallback-model")

	ch, err := NewFailoverClient(primary, fallback).StreamChat(
		context.Background(), ChatRequest{UserMessage: "question"})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	var got string
	for part := range ch {
		if part.Err != nil {
			t.Fatalf("不应返回错误: %v", part.Err)
		}
		got += part.Token
	}
	if got != "主用" {
		t.Errorf("答案 = %q, want %q（不得混入备用内容）", got, "主用")
	}
	if fallbackHits.Load() != 0 {
		t.Errorf("已投递 token 后仍切换了备用 provider（%d 次）", fallbackHits.Load())
	}
}

// TestCompleteJSON_Failover 结构化审查在主 provider 失败时切换备用。
func TestCompleteJSON_Failover(t *testing.T) {
	primary := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}, "primary-model")

	fallback := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"ok\":true}"}}]}`))
	}, "fallback-model")

	got, err := NewFailoverClient(primary, fallback).CompleteJSON(
		context.Background(), "system", "user", 5*time.Second)
	if err != nil {
		t.Fatalf("CompleteJSON: %v", err)
	}
	if got != `{"ok":true}` {
		t.Errorf("content = %q", got)
	}
}

// TestRerank_Failover 重排在主 provider 5xx 时切换备用。
func TestRerank_Failover(t *testing.T) {
	primary := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}, "rerank-model")

	fallback := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"t","results":[{"index":0,"relevance_score":0.9}]}`))
	}, "rerank-model")

	results, err := NewFailoverClient(primary, fallback).Rerank(
		context.Background(), "query", []string{"doc"}, 1)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(results) != 1 || results[0].Index != 0 {
		t.Errorf("results = %+v", results)
	}
}

// TestEmbed_Failover 向量化在主 provider 失败时切换备用（备用须同模型同维度，由装配层保证）。
func TestEmbed_Failover(t *testing.T) {
	primary := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}, "embed-model")
	primary.cfg.EmbeddingModel = "bge-m3"

	fallback := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2],"index":0}],"model":"bge-m3"}`))
	}, "embed-model")
	fallback.cfg.EmbeddingModel = "bge-m3"

	chain := NewFailoverClient(primary, fallback)
	embs, err := chain.Embed(context.Background(), []string{"高血压"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(embs) != 1 || len(embs[0]) != 2 {
		t.Errorf("embeddings = %+v", embs)
	}
}

// TestIsRetryable 错误分类：权限/参数错误不重试，限流/5xx/网络错误重试。
func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"未配置", ErrNotConfigured, false},
		{"客户端取消", context.Canceled, false},
		{"401", &openai.APIError{HTTPStatusCode: 401}, false},
		{"403", &openai.APIError{HTTPStatusCode: 403}, false},
		{"400", &openai.APIError{HTTPStatusCode: 400}, false},
		{"429", &openai.APIError{HTTPStatusCode: 429}, true},
		{"500", &openai.APIError{HTTPStatusCode: 500}, true},
		{"网络错误", &openai.RequestError{HTTPStatusCode: 0, Err: errors.New("connection refused")}, true},
		{"rerank 500", &apiStatusError{Code: 500}, true},
		{"rerank 400", &apiStatusError{Code: 400}, false},
		{"未知错误", errors.New("bad response"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryable(tt.err); got != tt.want {
				t.Errorf("isRetryable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
