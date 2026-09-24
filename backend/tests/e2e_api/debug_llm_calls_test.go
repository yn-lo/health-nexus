//go:build e2e

package e2e_api_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"health-nexus/internal/config"
	"health-nexus/internal/platform/llm"
)

// TestDebugLLMCalls directly invokes each LLM provider to verify connectivity.
func TestDebugLLMCalls(t *testing.T) {
	// 密钥必须通过环境变量注入（仓库禁止存储真实 API Key）。
	apiKey := os.Getenv("HEALTH_NEXUS_LLM_API_KEY")
	embedKey := os.Getenv("HEALTH_NEXUS_LLM_EMBEDDING_API_KEY")
	if apiKey == "" || embedKey == "" {
		t.Skip("LLM API keys not set via environment (HEALTH_NEXUS_LLM_API_KEY / HEALTH_NEXUS_LLM_EMBEDDING_API_KEY)")
	}

	cfg := config.LLMConfig{
		BaseURL:        "https://apihub.agnes-ai.com/v1",
		APIKey:         apiKey,
		ChatModel:      "agnes-2.0-flash",
		EmbeddingModel: "text-embedding-3-small",
		Timeout:        30 * time.Second,
		Embedding: config.ProviderConfig{
			BaseURL: "https://api.siliconflow.cn/v1",
			APIKey:  embedKey,
			Model:   "BAAI/bge-m3",
			Timeout: 30 * time.Second,
		},
	}

	// 1. 统一理解与审查（结构化 JSON 输出，agnes）
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	safetyClient, _ := llm.NewClient(cfg)
	if safetyClient == nil {
		fmt.Println("Assessor client is nil (LLM client not ready)")
	} else {
		raw, err := safetyClient.CompleteJSON(ctx,
			"只输出一个 JSON 对象：{\"ok\":true}", "连通性检查", 10*time.Second)
		if err != nil {
			fmt.Printf("Assessor ERR: %v\n", err)
		} else {
			fmt.Printf("Assessor result: %q\n", raw)
		}
	}

	// 3. Embedding (siliconflow BAAI/bge-m3)
	embedClient, _ := llm.NewEmbeddingClient(cfg)
	if embedClient == nil {
		fmt.Println("Embed client is nil")
	} else {
		embeds, err := embedClient.Embed(ctx, []string{"高血压患者日常管理要点有哪些？"})
		if err != nil {
			fmt.Printf("Embed ERR: %v\n", err)
		} else if len(embeds) > 0 {
			fmt.Printf("Embed result: dim=%d first5=%v\n", len(embeds[0]), embeds[0][:5])
		}
	}

	// 4. Chat stream (agnes)
	streamClient, _ := llm.NewClient(cfg)
	streamCtx, cancelStream := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelStream()
	sysPrompt := "你是健康宣教助手。基于参考资料回答患者问题。"
	userMsg := "高血压患者日常管理要点有哪些？参考资料：高血压 日常 管理 要点 用药 监测 饮食 运动"
	streamCh, streamErr := streamClient.StreamChat(streamCtx, llm.ChatRequest{
		SystemPrompt: sysPrompt,
		UserMessage:  userMsg,
	})
	if streamErr != nil {
		fmt.Printf("Stream ERR: %v\n", streamErr)
	} else {
		fmt.Println("\nStream tokens (first 30):")
		count := 0
		var sb strings.Builder
		for chunk := range streamCh {
			if chunk.Err != nil {
				fmt.Printf("  chunk err: %v\n", chunk.Err)
				break
			}
			if chunk.Done {
				fmt.Println("  [DONE]")
				break
			}
			sb.WriteString(chunk.Token)
			count++
			if count <= 30 {
				fmt.Printf("  token[%d]: %q\n", count, chunk.Token)
			}
		}
		fmt.Printf("\n  total tokens: %d, total chars: %d\n", count, sb.Len())
		fmt.Printf("  full text: %s\n", sb.String()[:min2(300, sb.Len())])
	}
}

// TestRawHTTPStructuredOutput 直接以 raw HTTP 调用，检查结构化输出的原始响应形态。
func TestRawHTTPStructuredOutput(t *testing.T) {
	key := os.Getenv("HEALTH_NEXUS_ZHIPU_API_KEY")
	if key == "" {
		t.Skip("API key not set via environment (HEALTH_NEXUS_ZHIPU_API_KEY)")
	}
	body := `{"model":"glm-4.7-flash","messages":[{"role":"system","content":"只输出一个 JSON 对象，不要解释"},{"role":"user","content":"输出 {\"ok\": true}"}],"temperature":0.0}`
	req, _ := http.NewRequest("POST", "https://open.bigmodel.cn/api/paas/v4/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	fmt.Printf("Raw structured output HTTP %d: %s\n", resp.StatusCode, string(b))
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}
