//go:build debug

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
	rewriteKey := os.Getenv("HEALTH_NEXUS_LLM_REWRITE_API_KEY")
	if apiKey == "" || embedKey == "" || rewriteKey == "" {
		t.Skip("LLM API keys not set via environment (HEALTH_NEXUS_LLM_API_KEY / HEALTH_NEXUS_LLM_EMBEDDING_API_KEY / HEALTH_NEXUS_LLM_REWRITE_API_KEY)")
	}

	cfg := config.LLMConfig{
		BaseURL:        "https://apihub.agnes-ai.com/v1",
		APIKey:         apiKey,
		ChatModel:      "agnes-2.0-flash",
		RewriteModel:   "agnes-2.0-flash",
		EmbeddingModel: "text-embedding-3-small",
		Timeout:        30 * time.Second,
		Embedding: config.ProviderConfig{
			BaseURL: "https://api.siliconflow.cn/v1",
			APIKey:  embedKey,
			Model:   "BAAI/bge-m3",
			Timeout: 30 * time.Second,
		},
		Rewrite: config.ProviderConfig{
			BaseURL: "https://open.bigmodel.cn/api/paas/v4",
			APIKey:  rewriteKey,
			Model:   "glm-4.7-flash",
			Timeout: 30 * time.Second,
		},
	}

	// 1. Rewrite LLM (智谱)
	rewriteClient, err := llm.NewRewriteClient(cfg)
	if err != nil {
		t.Fatalf("NewRewriteClient: %v", err)
	}
	if rewriteClient == nil {
		t.Fatal("rewrite client is nil")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	rewritten, err := rewriteClient.ToStandaloneQuestion(ctx, "高血压患者日常管理要点有哪些？", nil)
	if err != nil {
		fmt.Printf("Rewrite ERR: %v\n", err)
	} else {
		fmt.Printf("Rewrite result: %q (len=%d)\n", rewritten, len(rewritten))
		// Test if BM25 matches the rewritten query
		if e2ePool != nil {
			var matchCount int
			ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel2()
			err = e2ePool.QueryRow(ctx2, `
				SELECT count(*) FROM article_chunks c
				JOIN articles a ON a.id = c.article_id
				WHERE c.is_active = true AND a.is_deleted = false AND a.status = 'published'
				  AND c.tsv @@ bigram_tsquery($1)`, rewritten).Scan(&matchCount)
			if err != nil {
				fmt.Printf("  BM25 match check ERR: %v\n", err)
			} else {
				fmt.Printf("  BM25 matches for rewritten query: %d\n", matchCount)
			}
		}
	}

	// 2. Safety check (agnes)
	safetyClient, _ := llm.NewClient(cfg)
	safetyChecker := llm.NewLLMSafetyChecker(safetyClient)
	if safetyChecker == nil {
		fmt.Println("SafetyChecker is nil (LLM client not ready)")
	} else {
		safe, err := safetyChecker.IsInputSafe(ctx, "高血压患者日常管理要点有哪些？")
		fmt.Printf("Safety check: safe=%v err=%v\n", safe, err)
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

// TestRawHTTPRewrite calls 智谱 API raw to see exact response.
func TestRawHTTPRewrite(t *testing.T) {
	rewriteKey := os.Getenv("HEALTH_NEXUS_LLM_REWRITE_API_KEY")
	if rewriteKey == "" {
		t.Skip("rewrite API key not set via environment (HEALTH_NEXUS_LLM_REWRITE_API_KEY)")
	}
	body := `{"model":"glm-4.7-flash","messages":[{"role":"system","content":"你是一个问题改写助手。根据对话历史，把用户最新追问改写为一个独立、完整、不含代词的问题。规则：1. 只输出改写后的问题，不要任何解释或前后缀 2. 若无需改写（首问或无歧义），原样返回用户问题 3. 将指代词替换为历史中的具体对象"},{"role":"user","content":"高血压患者日常管理要点有哪些？"}],"temperature":0.0}`
	req, _ := http.NewRequest("POST", "https://open.bigmodel.cn/api/paas/v4/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+rewriteKey)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	fmt.Printf("Raw rewrite HTTP %d: %s\n", resp.StatusCode, string(b))
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}
