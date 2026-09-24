// Package tests LLM 集成测试——验证真实 LLM/Embedding/Rerank API 调用。
//
// 运行前设置环境变量（或在 config.yaml 中配置）：
//
//	set HEALTH_NEXUS_LLM_API_KEY=<your-api-key>        # agnes chat
//	set HEALTH_NEXUS_EMBEDDING_API_KEY=<your-api-key>   # 硅基流动 embedding+rerank
//	set HEALTH_NEXUS_LLM_REWRITE_API_KEY=<your-api-key> # 智谱 rewrite
//
// 运行：cd backend && go test ./tests/... -run TestLLM -v -count=1 -tags e2e
//
// 注意：API key 有限速，测试间已加 2s delay。
// 涉及真实外部 API 调用，仅在显式指定 -tags e2e 时编译，默认 go test ./tests/... 不运行。
//
//go:build e2e

package tests_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"health-nexus/internal/config"
	"health-nexus/internal/platform/llm"
)

// skipIfNoKey 环境变量未设置时跳过测试。
func skipIfNoKey(t *testing.T, envKey string) string {
	t.Helper()
	val := os.Getenv(envKey)
	if val == "" {
		t.Skipf("环境变量 %s 未设置，跳过集成测试", envKey)
	}
	return val
}

// ============================================================================
// LLM 流式对话（Agnes API）— 主 chat client
// ============================================================================

func TestLLM_StreamChat(t *testing.T) {
	apiKey := skipIfNoKey(t, "HEALTH_NEXUS_LLM_API_KEY")

	client, err := llm.NewClient(config.LLMConfig{
		BaseURL:   "https://apihub.agnes-ai.com/v1",
		APIKey:    apiKey,
		ChatModel: "agnes-2.0-flash",
		Timeout:   30 * time.Second,
	})
	if err != nil {
		t.Fatalf("创建 LLM 客户端失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ch, err := client.StreamChat(ctx, llm.ChatRequest{
		SystemPrompt: "你是一个健康宣教助手，用简洁的中文回答。",
		UserMessage:  "高血压患者应该注意什么？请简要回答。",
	})
	if err != nil {
		t.Fatalf("StreamChat 调用失败: %v", err)
	}

	tokens := make([]string, 0)
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("流式响应错误: %v", chunk.Err)
		}
		if chunk.Done {
			break
		}
		tokens = append(tokens, chunk.Token)
	}

	fullResponse := strings.Join(tokens, "")
	if fullResponse == "" {
		t.Fatal("LLM 返回空响应")
	}
	t.Logf("LLM 流式响应（%d tokens）: %s", len(tokens), truncate(fullResponse, 200))
}

// ============================================================================
// Embedding 向量生成（硅基流动 API）— NewEmbeddingClient + Embedding 子配置
// ============================================================================

func TestLLM_Embed(t *testing.T) {
	apiKey := skipIfNoKey(t, "HEALTH_NEXUS_EMBEDDING_API_KEY")

	// 主配置用占位（agnes 不支持 embedding）；Embedding 子配置指向硅基流动。
	cfg := config.LLMConfig{
		BaseURL:        "https://apihub.agnes-ai.com/v1",
		APIKey:         "placeholder-main-not-used",
		EmbeddingModel: "text-embedding-3-small",
		Timeout:        30 * time.Second,
		Embedding: config.ProviderConfig{
			BaseURL: "https://api.siliconflow.cn/v1",
			APIKey:  apiKey,
			Model:   "BAAI/bge-m3",
			Timeout: 30 * time.Second,
		},
	}
	client, err := llm.NewEmbeddingClient(cfg)
	if err != nil {
		t.Fatalf("创建 Embedding 客户端失败: %v", err)
	}
	if client == nil {
		t.Fatal("NewEmbeddingClient 返回 nil（API key 未解析）")
	}

	// 间隔 2s 避免限速
	time.Sleep(2 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	texts := []string{
		"高血压患者应该低盐饮食",
		"糖尿病患者需要控制血糖",
	}
	embeddings, err := client.Embed(ctx, texts)
	if err != nil {
		t.Fatalf("Embed 调用失败: %v", err)
	}

	if len(embeddings) != len(texts) {
		t.Fatalf("期望 %d 个向量，实际 %d", len(texts), len(embeddings))
	}
	for i, emb := range embeddings {
		if len(emb) == 0 {
			t.Fatalf("第 %d 个向量为空", i)
		}
		t.Logf("文本 %d: 维度=%d, 前5维=%v", i, len(emb), emb[:5])
	}
}

// ============================================================================
// Rerank 文档重排（硅基流动 Rerank API）— NewRerankClient 直接调用 /v1/rerank
// ============================================================================

func TestLLM_Rerank(t *testing.T) {
	apiKey := skipIfNoKey(t, "HEALTH_NEXUS_EMBEDDING_API_KEY") // 硅基流动 embedding+rerank 共用 key

	// 主配置占位；Rerank 子配置指向硅基流动 bge-reranker。
	cfg := config.LLMConfig{
		BaseURL: "https://apihub.agnes-ai.com/v1",
		APIKey:  "placeholder-main-not-used",
		Timeout: 30 * time.Second,
		Rerank: config.ProviderConfig{
			BaseURL: "https://api.siliconflow.cn/v1",
			APIKey:  apiKey,
			Model:   "BAAI/bge-reranker-v2-m3",
			Timeout: 30 * time.Second,
		},
	}
	client, err := llm.NewRerankClient(cfg)
	if err != nil {
		t.Fatalf("创建 Rerank 客户端失败: %v", err)
	}
	if client == nil {
		t.Fatal("NewRerankClient 返回 nil（API key/model 未解析）")
	}

	// 间隔 2s
	time.Sleep(2 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	docs := []string{
		"高血压患者应该低盐饮食，每天摄入盐不超过6克。",
		"感冒是一种常见的呼吸道疾病，由病毒引起。",
		"降压药物需要遵医嘱服用，不能自行停药。",
		"运动有助于控制血压，建议每天30分钟有氧运动。",
	}
	results, err := client.Rerank(ctx, "高血压怎么控制", docs, 2)
	if err != nil {
		t.Fatalf("Rerank 调用失败: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Rerank 返回空结果")
	}
	t.Logf("Rerank 结果（top %d）: %v", len(results), results)
	for _, r := range results {
		if r.Index < 0 || r.Index >= len(docs) {
			t.Errorf("索引 %d 越界（文档数 %d）", r.Index, len(docs))
		}
	}
}

// ============================================================================
// 结构化输出（统一理解与审查所用的 JSON 调用路径）
// ============================================================================

// TestLLM_CompleteJSON 验证 JSON 输出模式的连通性（Assessor/OutputReviewer 依赖此路径）。
func TestLLM_CompleteJSON(t *testing.T) {
	apiKey := skipIfNoKey(t, "HEALTH_NEXUS_ZHIPU_API_KEY")

	cfg := config.LLMConfig{
		BaseURL:   "https://open.bigmodel.cn/api/paas/v4",
		APIKey:    apiKey,
		ChatModel: "glm-4.7-flash",
		Timeout:   30 * time.Second,
	}
	client, err := llm.NewClient(cfg)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}
	if client == nil {
		t.Fatal("NewClient 返回 nil（API key 未解析）")
	}

	// 间隔 2s
	time.Sleep(2 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	raw, err := client.CompleteJSON(ctx,
		`只输出一个 JSON 对象：{"ok": true, "note": "字符串"}`, "连通性检查", 30*time.Second)
	if err != nil {
		t.Fatalf("CompleteJSON 调用失败: %v", err)
	}
	if !strings.Contains(raw, "{") {
		t.Fatalf("期望返回 JSON 对象，实际: %s", raw)
	}
	t.Logf("结构化输出: %s", raw)
}

// ============================================================================
// 辅助函数
// ============================================================================

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
