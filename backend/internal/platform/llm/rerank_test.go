package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"health-nexus/internal/config"
)

func TestRerank_API(t *testing.T) {
	// mock rerank server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/rerank" {
			t.Errorf("expected /v1/rerank, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "test",
			"results": [
				{"index": 1, "relevance_score": 0.95},
				{"index": 0, "relevance_score": 0.3}
			]
		}`))
	}))
	defer server.Close()

	chat, hc := newOpenAIClient(server.URL, "test-key", 0)
	client := &Client{
		chat:       chat,
		httpClient: hc,
		cfg:        config.LLMConfig{BaseURL: server.URL + "/v1", ChatModel: "BAAI/bge-reranker-v2-m3"},
	}

	results, err := client.Rerank(context.Background(), "query", []string{"doc0", "doc1"}, 2)
	if err != nil {
		t.Fatalf("Rerank() error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Index != 1 || results[0].Score != 0.95 {
		t.Errorf("result[0] = {Index:%d, Score:%f}, want {Index:1, Score:0.95}", results[0].Index, results[0].Score)
	}
	if results[1].Index != 0 || results[1].Score != 0.3 {
		t.Errorf("result[1] = {Index:%d, Score:%f}, want {Index:0, Score:0.3}", results[1].Index, results[1].Score)
	}
}

func TestRerank_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":20012,"message":"Model does not exist"}`))
	}))
	defer server.Close()

	chat2, hc2 := newOpenAIClient(server.URL, "test-key", 0)
	client := &Client{
		chat:       chat2,
		httpClient: hc2,
		cfg:        config.LLMConfig{BaseURL: server.URL + "/v1", ChatModel: "bad-model"},
	}

	_, err := client.Rerank(context.Background(), "query", []string{"doc0"}, 1)
	if err == nil {
		t.Fatal("expected error for bad model, got nil")
	}
}

func TestPingRerank(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/rerank" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"test","results":[{"index":0,"relevance_score":1.0}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	chat3, hc3 := newOpenAIClient(server.URL, "test-key", 0)
	client := &Client{
		chat:       chat3,
		httpClient: hc3,
		cfg:        config.LLMConfig{BaseURL: server.URL + "/v1", ChatModel: "BAAI/bge-reranker-v2-m3"},
	}

	if err := client.Ping(context.Background(), "rerank"); err != nil {
		t.Fatalf("Ping rerank error: %v", err)
	}
}
