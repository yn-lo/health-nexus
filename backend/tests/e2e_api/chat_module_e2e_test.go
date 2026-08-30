// Package e2e_api_test
//
// chat_module_e2e_test.go 对对话模块（chat module）共 8 个端点进行真实 HTTP e2e 测试。
// 覆盖 6 个患者端端点（SSE 流式问答 + 5 个会话管理）和 2 个医护端危机事件端点。
//
// SSE happy path 必须真实触发 LLM 调用（核心验证点：多 provider 配置生效）。
// 测试前 setupChatSeed 确保 article_chunks 表存在匹配 chunk——
// 因 asynq worker 未运行，系统不会自动向量化；本测试用 SQL 直接注入可被 BM25 命中的切片
// （embedding 维度由 TestSeedChunk 动态读取 DB 列定义，方案 C 后维度可变）。
//
// 输出：每个用例 t.Logf 输出 PASS/FAIL，最后 t.Logf 汇总。
//
// SSE happy path 会真实触发 LLM 调用，涉及外部 API，仅在显式指定 -tags e2e 时编译，
// 默认 go test ./tests/... 不运行（"手动"测试）。
//
//go:build e2e

package e2e_api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"health-nexus/internal/config"
	"health-nexus/internal/platform/llm"
)

// orDefault 返回 s 非空时的 s，否则返回 def。
func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// float32SliceLiteral 将 float32 切片格式化为 pgvector 文本字面量 "[a,b,c]"。
func float32SliceLiteral(v []float32) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = strconv.FormatFloat(float64(f), 'f', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// chatTestResult 收集每个用例的结果用于最终汇总。
type chatTestResult struct {
	Endpoint string
	Case     string
	Pass     bool
	Detail   string
}

// chatResults 全局收集，由 TestE2EChatModuleSummary 输出汇总。
var chatResults []chatTestResult

// recordChat 记录一条结果。
func recordChat(endpoint, testCase string, pass bool, detail string) {
	chatResults = append(chatResults, chatTestResult{
		Endpoint: endpoint, Case: testCase, Pass: pass, Detail: detail,
	})
}

// setupChatSeed 确保 SSE happy path 有可被 BM25 命中的切片。
// 关键发现：article 1 的 status 在前序测试中被改为 'deleted'，
// 而 SearchByFullText 的 SQL WHERE 子句要求 a.status='published' AND a.is_deleted=false，
// 故 seed chunk 必须关联到一个 published + 未删除的 article。
//
// 策略：动态查找第一个 published 且 dept_id=1 且 is_deleted=false 的 article_id，
// 将 seed chunk 关联到它。兼容多次运行：先按 content_hash 删除旧 seed，再插入。
//
// 同时确保 safety_messages.rejection_message 为默认值（前序测试可能已更新过），
// 让 SSE 拒答路径返回默认话术——便于结果断言。
func setupChatSeed(t *testing.T) int64 {
	t.Helper()
	if e2ePool == nil {
		t.Fatal("e2ePool nil, DB required for chat e2e")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. 清理旧 seed chunk（多次运行兼容）
	if _, err := e2ePool.Exec(ctx, `DELETE FROM article_chunks WHERE content_hash = 'e2e_chat_seed'`); err != nil {
		t.Fatalf("cleanup old seed: %v", err)
	}

	// 2. 查找第一个 published + 未删除 + dept_id=1 的 article_id（testpatient 的 dept_id=1 或 2 均可命中）
	//    article 1 的 status='deleted'（前序测试副作用），故用动态查找而非硬编码 id=1。
	var articleID int64
	err := e2ePool.QueryRow(ctx, `
		SELECT id FROM articles
		WHERE status = 'published' AND is_deleted = false AND department_id = 1
		ORDER BY id LIMIT 1`).Scan(&articleID)
	if err != nil {
		t.Fatalf("no published article in dept=1: %v", err)
	}
	t.Logf("setupChatSeed: using published article_id=%d for seed chunk", articleID)

	// 3. 注入 seed chunk（content 同时覆盖原查询与 split-token 改写后查询）
	//    bigram_tsvector(content) 使用 unigram + bigram 分词，中文召回率显著提升。
	//    同时生成真实 embedding 向量写入——SearchService 的 filterBySimilarity(threshold>0)
	//    会过滤 VecScore==0 的 BM25-only 命中，无向量 chunk 必然被拒答。
	content := "高血压患者日常管理要点有哪些 高血压 日常 管理 要点 患者 用药 监测 饮食 运动"

	// 从环境变量构造 embedding 客户端（与后端同款子配置：硅基流动 BAAI/bge-m3）。
	embedKey := os.Getenv("HEALTH_NEXUS_LLM_EMBEDDING_API_KEY")
	if embedKey == "" {
		t.Skip("HEALTH_NEXUS_LLM_EMBEDDING_API_KEY not set; cannot seed embedding vector")
	}
	embCfg := config.LLMConfig{
		Embedding: config.ProviderConfig{
			BaseURL: orDefault(os.Getenv("HEALTH_NEXUS_LLM_EMBEDDING_BASE_URL"), "https://api.siliconflow.cn/v1"),
			APIKey:  embedKey,
			Model:   orDefault(os.Getenv("HEALTH_NEXUS_LLM_EMBEDDING_MODEL"), "BAAI/bge-m3"),
			Timeout: 30 * time.Second,
		},
	}
	embClient, err := llm.NewEmbeddingClient(embCfg)
	if err != nil || embClient == nil {
		t.Skipf("embedding client unavailable: %v", err)
	}
	embeddings, err := embClient.Embed(ctx, []string{content})
	if err != nil {
		t.Skipf("embedding call failed: %v", err)
	}
	if len(embeddings) == 0 || len(embeddings[0]) == 0 {
		t.Skip("embedding returned empty vector")
	}
	vecLit := float32SliceLiteral(embeddings[0])

	_, err = e2ePool.Exec(ctx, `
		INSERT INTO article_chunks (article_id, chunk_index, content, content_hash, embedding, is_active, version)
		VALUES ($1, 90, $2, 'e2e_chat_seed', $3::vector, true, 1)`, articleID, content, vecLit)
	if err != nil {
		t.Fatalf("seed chunk: %v", err)
	}

	// 4. 确保 safety_messages.rejection_message 为默认值（前序测试可能已更新过）
	_, _ = e2ePool.Exec(ctx, `
		UPDATE safety_messages SET content = '抱歉，我无法回答这个问题，建议您咨询您的主治医生。'
		WHERE type = 'rejection'`)

	// 5. 验证 BM25 命中（带 dept 过滤，模拟 SearchService 实际调用路径）
	var matchCount int
	err = e2ePool.QueryRow(ctx, `
		SELECT count(*) FROM article_chunks c
		JOIN articles a ON a.id = c.article_id
		WHERE c.is_active = true AND a.is_deleted = false AND a.status = 'published'
		  AND (a.department_id = ANY(ARRAY[1]::bigint[]) OR EXISTS (
		    SELECT 1 FROM article_references r
		    WHERE r.article_id = c.article_id AND r.target_dept_id = ANY(ARRAY[1]::bigint[])
		      AND r.status = 'approved'))
		  AND c.tsv @@ bigram_tsquery('高血压患者日常管理要点有哪些？')`).Scan(&matchCount)
	if err != nil {
		t.Fatalf("verify bm25: %v", err)
	}
	if matchCount == 0 {
		t.Fatal("BM25 match count = 0 after seeding with dept filter; chunk tsv may not contain the query token")
	}
	t.Logf("setupChatSeed: seeded chunk for article %d, BM25 match count = %d", articleID, matchCount)
	return articleID
}

// cleanupChatSeed 测试结束后清理 seed chunk。
func cleanupChatSeed(t *testing.T) {
	t.Helper()
	if e2ePool == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tag, err := e2ePool.Exec(ctx, `DELETE FROM article_chunks WHERE content_hash = 'e2e_chat_seed'`)
	if err != nil {
		t.Errorf("cleanup seed: %v", err)
		return
	}
	t.Logf("cleanupChatSeed: removed %d seeded chunks", tag.RowsAffected())
}

// ─── EP23: SSE 流式问答 ────────────────────────────────────────────────────

// TestE2EChatSSE 测试 SSE 流式问答全部用例（真实 LLM 调用）。
func TestE2EChatSSE(t *testing.T) {
	setupHelper(t)
	setupChatSeed(t)
	defer cleanupChatSeed(t)

	patientToken := loginPatient(t)
	staffToken := loginDoctor(t)

	// === happy path: 高血压咨询 → 期望 token + done ===
	t.Run("EP23_HappyPath_TokenStream", func(t *testing.T) {
		events := doSSE(t, patientToken, "高血压患者日常管理要点有哪些？", nil, 90*time.Second)
		types := eventTypes(events)
		sawDone := contains(events, "done")
		sawToken := contains(events, "token")
		sawReferences := contains(events, "references")
		tokenChars := totalTokenChars(events)
		detail := fmt.Sprintf("events=%v sawDone=%v sawToken=%v sawRefs=%v tokenChars=%d",
			types, sawDone, sawToken, sawReferences, tokenChars)
		t.Logf("EP23 HappyPath: %s", detail)
		pass := sawDone && sawToken && tokenChars > 0
		recordChat("EP23 POST /api/chat/stream", "HappyPath_TokenStream", pass, detail)
		if !pass {
			t.Errorf("EP23 happy path FAILED: %s", detail)
		}
	})

	// === 危机词触发: "我想自杀" → 期望 crisis 事件 ===
	t.Run("EP23_CrisisTrigger", func(t *testing.T) {
		events := doSSE(t, patientToken, "我想自杀", nil, 60*time.Second)
		types := eventTypes(events)
		sawCrisis := contains(events, "crisis")
		sawDone := contains(events, "done")
		detail := fmt.Sprintf("events=%v sawCrisis=%v sawDone=%v", types, sawCrisis, sawDone)
		t.Logf("EP23 CrisisTrigger: %s", detail)
		pass := sawCrisis && sawDone
		recordChat("EP23 POST /api/chat/stream", "CrisisTrigger_自杀", pass, detail)
		if !pass {
			t.Errorf("EP23 crisis trigger FAILED: %s", detail)
		}
	})

	// === 紧急词触发: "我胸痛得厉害" → 期望 safety_warning(紧急就医) + token + done ===
	t.Run("EP23_EmergencyTrigger", func(t *testing.T) {
		events := doSSE(t, patientToken, "我胸痛得厉害，是心梗吗", nil, 90*time.Second)
		types := eventTypes(events)
		sawSafetyWarning := contains(events, "safety_warning")
		sawDone := contains(events, "done")
		// 紧急提醒下发后仍走 RAG → 期望 token；但若检索未命中则可能降级为 safety_warning+done
		// 我们只断言 safety_warning + done（紧急提醒必下发）；token 作为软断言记录
		sawToken := contains(events, "token")
		detail := fmt.Sprintf("events=%v sawSafetyWarning=%v sawToken=%v sawDone=%v",
			types, sawSafetyWarning, sawToken, sawDone)
		t.Logf("EP23 EmergencyTrigger: %s", detail)
		pass := sawSafetyWarning && sawDone
		recordChat("EP23 POST /api/chat/stream", "EmergencyTrigger_胸痛", pass, detail)
		if !pass {
			t.Errorf("EP23 emergency trigger FAILED: %s", detail)
		}
	})

	// === 错误：message 空 → 400 ===
	t.Run("EP23_EmptyMessage_400", func(t *testing.T) {
		req, _ := http.NewRequest("POST", apiURL()+"/api/chat/stream", jsonBody(t, map[string]any{"message": ""}))
		req.Header.Set("Authorization", authHeader(patientToken))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer drainAndClose(resp)
		pass := resp.StatusCode == http.StatusBadRequest
		detail := fmt.Sprintf("status=%d (expected 400)", resp.StatusCode)
		t.Logf("EP23 EmptyMessage_400: %s", detail)
		recordChat("EP23 POST /api/chat/stream", "EmptyMessage_400", pass, detail)
		if !pass {
			t.Errorf("EP23 empty message FAILED: %s", detail)
		}
	})

	// === 错误：message 超长 (>2000 字符) → 422 ===
	t.Run("EP23_TooLongMessage_422", func(t *testing.T) {
		longMsg := strings.Repeat("a", 2001)
		req, _ := http.NewRequest("POST", apiURL()+"/api/chat/stream", jsonBody(t, map[string]any{"message": longMsg}))
		req.Header.Set("Authorization", authHeader(patientToken))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer drainAndClose(resp)
		pass := resp.StatusCode == http.StatusUnprocessableEntity
		detail := fmt.Sprintf("status=%d (expected 422)", resp.StatusCode)
		t.Logf("EP23 TooLongMessage_422: %s", detail)
		recordChat("EP23 POST /api/chat/stream", "TooLongMessage_422", pass, detail)
		if !pass {
			t.Errorf("EP23 too long message FAILED: %s", detail)
		}
	})

	// === 错误：conversation_id 不存在 → 404 ===
	t.Run("EP23_InvalidConversationID_404", func(t *testing.T) {
		fakeID := uuid.New().String()
		req, _ := http.NewRequest("POST", apiURL()+"/api/chat/stream", jsonBody(t, map[string]any{"message": "测试", "conversation_id": fakeID}))
		req.Header.Set("Authorization", authHeader(patientToken))
		req.Header.Set("Content-Type", "application/json")
		// 用短超时 client，因为 SSE 长连接不会主动 close
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer drainAndClose(resp)
		// 404 在 SSE 写入前返回 HTTP 404
		pass := resp.StatusCode == http.StatusNotFound
		detail := fmt.Sprintf("status=%d (expected 404)", resp.StatusCode)
		t.Logf("EP23 InvalidConversationID_404: %s", detail)
		recordChat("EP23 POST /api/chat/stream", "InvalidConversationID_404", pass, detail)
		if !pass {
			t.Errorf("EP23 invalid conversation_id FAILED: %s", detail)
		}
	})

	// === 错误：匿名访问 → 401 ===
	t.Run("EP23_NoAuth_401", func(t *testing.T) {
		req, _ := http.NewRequest("POST", apiURL()+"/api/chat/stream", jsonBody(t, map[string]any{"message": "测试"}))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer drainAndClose(resp)
		pass := resp.StatusCode == http.StatusUnauthorized
		detail := fmt.Sprintf("status=%d (expected 401)", resp.StatusCode)
		t.Logf("EP23 NoAuth_401: %s", detail)
		recordChat("EP23 POST /api/chat/stream", "NoAuth_401", pass, detail)
		if !pass {
			t.Errorf("EP23 no auth FAILED: %s", detail)
		}
	})

	// === 聊天对所有已登录用户开放：staff token 不应 403 ===
	t.Run("EP23_StaffTokenAllowed", func(t *testing.T) {
		req, _ := http.NewRequest("POST", apiURL()+"/api/chat/stream", jsonBody(t, map[string]any{"message": "测试"}))
		req.Header.Set("Authorization", authHeader(staffToken))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer drainAndClose(resp)
		pass := resp.StatusCode != http.StatusForbidden
		detail := fmt.Sprintf("status=%d (expected non-403)", resp.StatusCode)
		t.Logf("EP23 StaffTokenAllowed: %s", detail)
		recordChat("EP23 POST /api/chat/stream", "StaffTokenAllowed", pass, detail)
		if !pass {
			t.Errorf("EP23 staff token should be allowed: %s", detail)
		}
		_ = staffToken
	})
}

// doSSE 发起 SSE 请求并返回所有事件。
// timeout 控制 LLM 调用最长等待时间。
func doSSE(t *testing.T, token, message string, convID *uuid.UUID, timeout time.Duration) []sseEvent {
	t.Helper()
	payload := map[string]any{"message": message}
	if convID != nil {
		payload["conversation_id"] = convID.String()
	}
	req, _ := http.NewRequest("POST", apiURL()+"/api/chat/stream", jsonBody(t, payload))
	req.Header.Set("Authorization", authHeader(token))
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("SSE request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("SSE expected 200, got %d: %s", resp.StatusCode, string(b))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		resp.Body.Close()
		t.Fatalf("expected Content-Type=text/event-stream, got %q", ct)
	}
	return readSSEEvents(t, resp)
}

// contains 判断事件列表中是否含指定类型。
func contains(events []sseEvent, eventType string) bool {
	for _, e := range events {
		if e.Event == eventType {
			return true
		}
	}
	return false
}

// totalTokenChars 统计所有 token 事件的字符总数。
func totalTokenChars(events []sseEvent) int {
	total := 0
	for _, e := range events {
		if e.Event == "token" {
			total += len(e.Data)
		}
	}
	return total
}

// ─── EP24-28: 会话管理端点 ──────────────────────────────────────────────────

// TestE2EChatConversations 测试 5 个会话管理端点的 happy + error 路径。
func TestE2EChatConversations(t *testing.T) {
	setupHelper(t)
	setupChatSeed(t)
	defer cleanupChatSeed(t)

	patientToken := loginPatient(t)
	staffToken := loginDoctor(t)

	var convID string // 由 EP23 happy path 创建的会话 ID（用于后续测试）
	// 先触发一次 SSE 创建会话
	t.Run("Setup_CreateConversationViaSSE", func(t *testing.T) {
		events := doSSE(t, patientToken, "高血压患者日常管理要点有哪些？", nil, 90*time.Second)
		t.Logf("Setup SSE events: %v", eventTypes(events))
	})

	// EP24: GET /api/chat/conversations → 200
	t.Run("EP24_ListHappy", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/chat/conversations", nil, patientToken)
		defer drainAndClose(resp)
		m := parseJSON(t, resp)
		pass := resp.StatusCode == http.StatusOK
		items, _ := m["items"].([]any)
		if pass && len(items) > 0 {
			first, _ := items[0].(map[string]any)
			convID, _ = first["id"].(string)
			for _, k := range []string{"id", "title", "locked_dept_id", "archived", "last_message_at", "created_at"} {
				if _, ok := first[k]; !ok {
					pass = false
				}
			}
		}
		detail := fmt.Sprintf("status=200 items=%d convID=%s", len(items), convID)
		t.Logf("EP24 ListHappy: %s", detail)
		recordChat("EP24 GET /api/chat/conversations", "ListHappy", pass, detail)
		if !pass {
			t.Errorf("EP24 list happy FAILED: %s", detail)
		}
	})

	// EP24 错误：匿名 → 401
	t.Run("EP24_NoAuth_401", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/chat/conversations", nil, "")
		defer drainAndClose(resp)
		pass := resp.StatusCode == http.StatusUnauthorized
		detail := fmt.Sprintf("status=%d (expected 401)", resp.StatusCode)
		t.Logf("EP24 NoAuth_401: %s", detail)
		recordChat("EP24 GET /api/chat/conversations", "NoAuth_401", pass, detail)
		if !pass {
			t.Errorf("EP24 no auth FAILED: %s", detail)
		}
	})

	// EP24 聊天对所有已登录用户开放：staff token 不应 403
	t.Run("EP24_StaffTokenAllowed", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/chat/conversations", nil, staffToken)
		defer drainAndClose(resp)
		pass := resp.StatusCode != http.StatusForbidden
		detail := fmt.Sprintf("status=%d (expected non-403)", resp.StatusCode)
		t.Logf("EP24 StaffTokenAllowed: %s", detail)
		recordChat("EP24 GET /api/chat/conversations", "StaffTokenAllowed", pass, detail)
		if !pass {
			t.Errorf("EP24 staff token should be allowed: %s", detail)
		}
	})

	// EP25: GET /api/chat/conversations/{id} → 200
	t.Run("EP25_GetHappy", func(t *testing.T) {
		if convID == "" {
			t.Skip("no convID")
		}
		resp := doReq(t, "GET", "/api/chat/conversations/"+convID, nil, patientToken)
		defer drainAndClose(resp)
		pass := resp.StatusCode == http.StatusOK
		var m map[string]any
		if pass {
			m = parseJSON(t, resp)
			for _, k := range []string{"id", "title", "locked_dept_id", "archived", "last_message_at", "created_at"} {
				if _, ok := m[k]; !ok {
					pass = false
				}
			}
		}
		detail := fmt.Sprintf("status=200 conv=%v", m)
		t.Logf("EP25 GetHappy: %s", detail)
		recordChat("EP25 GET /api/chat/conversations/{id}", "GetHappy", pass, detail)
		if !pass {
			t.Errorf("EP25 get happy FAILED")
		}
	})

	// EP25 错误：不存在 → 404
	t.Run("EP25_NotFound_404", func(t *testing.T) {
		fakeID := uuid.New().String()
		resp := doReq(t, "GET", "/api/chat/conversations/"+fakeID, nil, patientToken)
		defer drainAndClose(resp)
		pass := resp.StatusCode == http.StatusNotFound
		detail := fmt.Sprintf("status=%d (expected 404)", resp.StatusCode)
		t.Logf("EP25 NotFound_404: %s", detail)
		recordChat("EP25 GET /api/chat/conversations/{id}", "NotFound_404", pass, detail)
		if !pass {
			t.Errorf("EP25 not found FAILED: %s", detail)
		}
	})

	// EP25 错误：非 UUID 格式 → 400
	t.Run("EP25_InvalidUUID_400", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/chat/conversations/not-a-uuid", nil, patientToken)
		defer drainAndClose(resp)
		pass := resp.StatusCode == http.StatusBadRequest
		detail := fmt.Sprintf("status=%d (expected 400)", resp.StatusCode)
		t.Logf("EP25 InvalidUUID_400: %s", detail)
		recordChat("EP25 GET /api/chat/conversations/{id}", "InvalidUUID_400", pass, detail)
		if !pass {
			t.Errorf("EP25 invalid uuid FAILED: %s", detail)
		}
	})

	// EP26: PATCH /api/chat/conversations/{id} → 200 (title)
	t.Run("EP26_PatchTitleHappy", func(t *testing.T) {
		if convID == "" {
			t.Skip("no convID")
		}
		body := jsonBody(t, map[string]any{"title": "E2E_新标题"})
		resp := doReq(t, "PATCH", "/api/chat/conversations/"+convID, body, patientToken)
		defer drainAndClose(resp)
		pass := resp.StatusCode == http.StatusOK
		var m map[string]any
		if pass {
			m = parseJSON(t, resp)
			if m["title"] != "E2E_新标题" {
				pass = false
			}
		}
		detail := fmt.Sprintf("status=200 title=%v", m["title"])
		t.Logf("EP26 PatchTitleHappy: %s", detail)
		recordChat("EP26 PATCH /api/chat/conversations/{id}", "PatchTitleHappy", pass, detail)
		if !pass {
			t.Errorf("EP26 patch title FAILED")
		}
	})

	// EP26 happy: archived=true
	t.Run("EP26_PatchArchivedHappy", func(t *testing.T) {
		if convID == "" {
			t.Skip("no convID")
		}
		body := jsonBody(t, map[string]any{"archived": true})
		resp := doReq(t, "PATCH", "/api/chat/conversations/"+convID, body, patientToken)
		defer drainAndClose(resp)
		pass := resp.StatusCode == http.StatusOK
		var m map[string]any
		if pass {
			m = parseJSON(t, resp)
			if m["archived"] != true {
				pass = false
			}
		}
		detail := fmt.Sprintf("status=200 archived=%v", m["archived"])
		t.Logf("EP26 PatchArchivedHappy: %s", detail)
		recordChat("EP26 PATCH /api/chat/conversations/{id}", "PatchArchivedHappy", pass, detail)
		if !pass {
			t.Errorf("EP26 patch archived FAILED")
		}
	})

	// EP26 错误：body 空 → 422
	t.Run("EP26_EmptyBody_422", func(t *testing.T) {
		if convID == "" {
			t.Skip("no convID")
		}
		resp := doReq(t, "PATCH", "/api/chat/conversations/"+convID, strings.NewReader("{}"), patientToken)
		defer drainAndClose(resp)
		pass := resp.StatusCode == http.StatusUnprocessableEntity || resp.StatusCode == http.StatusBadRequest
		detail := fmt.Sprintf("status=%d (expected 422 or 400)", resp.StatusCode)
		t.Logf("EP26 EmptyBody_422: %s", detail)
		recordChat("EP26 PATCH /api/chat/conversations/{id}", "EmptyBody_422", pass, detail)
		if !pass {
			t.Errorf("EP26 empty body FAILED: %s", detail)
		}
	})

	// EP26 错误：不存在 → 404
	t.Run("EP26_NotFound_404", func(t *testing.T) {
		fakeID := uuid.New().String()
		body := jsonBody(t, map[string]any{"title": "x"})
		resp := doReq(t, "PATCH", "/api/chat/conversations/"+fakeID, body, patientToken)
		defer drainAndClose(resp)
		pass := resp.StatusCode == http.StatusNotFound
		detail := fmt.Sprintf("status=%d (expected 404)", resp.StatusCode)
		t.Logf("EP26 NotFound_404: %s", detail)
		recordChat("EP26 PATCH /api/chat/conversations/{id}", "NotFound_404", pass, detail)
		if !pass {
			t.Errorf("EP26 not found FAILED: %s", detail)
		}
	})

	// EP28: GET /api/chat/conversations/{id}/messages → 200
	t.Run("EP28_ListMessagesHappy", func(t *testing.T) {
		if convID == "" {
			t.Skip("no convID")
		}
		resp := doReq(t, "GET", fmt.Sprintf("/api/chat/conversations/%s/messages?limit=50", convID), nil, patientToken)
		defer drainAndClose(resp)
		pass := resp.StatusCode == http.StatusOK
		var arr []map[string]any
		if pass {
			if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
				pass = false
			} else {
				for _, msg := range arr {
					for _, k := range []string{"id", "conversation_id", "role", "content", "created_at"} {
						if _, ok := msg[k]; !ok {
							pass = false
						}
					}
				}
			}
		}
		detail := fmt.Sprintf("status=200 msg_count=%d", len(arr))
		t.Logf("EP28 ListMessagesHappy: %s", detail)
		recordChat("EP28 GET /api/chat/conversations/{id}/messages", "ListMessagesHappy", pass, detail)
		if !pass {
			t.Errorf("EP28 list messages FAILED")
		}
	})

	// EP28 错误：不存在 → 404
	t.Run("EP28_NotFound_404", func(t *testing.T) {
		fakeID := uuid.New().String()
		resp := doReq(t, "GET", fmt.Sprintf("/api/chat/conversations/%s/messages", fakeID), nil, patientToken)
		defer drainAndClose(resp)
		pass := resp.StatusCode == http.StatusNotFound
		detail := fmt.Sprintf("status=%d (expected 404)", resp.StatusCode)
		t.Logf("EP28 NotFound_404: %s", detail)
		recordChat("EP28 GET /api/chat/conversations/{id}/messages", "NotFound_404", pass, detail)
		if !pass {
			t.Errorf("EP28 not found FAILED: %s", detail)
		}
	})

	// EP27: DELETE /api/chat/conversations/{id} → 200
	t.Run("EP27_DeleteHappy", func(t *testing.T) {
		if convID == "" {
			t.Skip("no convID")
		}
		resp := doReq(t, "DELETE", "/api/chat/conversations/"+convID, nil, patientToken)
		defer drainAndClose(resp)
		pass := resp.StatusCode == http.StatusOK
		var m map[string]any
		if pass {
			m = parseJSON(t, resp)
			if m["success"] != true {
				pass = false
			}
		}
		detail := fmt.Sprintf("status=200 success=%v", m["success"])
		t.Logf("EP27 DeleteHappy: %s", detail)
		recordChat("EP27 DELETE /api/chat/conversations/{id}", "DeleteHappy", pass, detail)
		if !pass {
			t.Errorf("EP27 delete FAILED")
		}
	})

	// EP27 错误：删除后再次删除 → 404
	t.Run("EP27_AfterDelete_404", func(t *testing.T) {
		if convID == "" {
			t.Skip("no convID")
		}
		resp := doReq(t, "DELETE", "/api/chat/conversations/"+convID, nil, patientToken)
		defer drainAndClose(resp)
		pass := resp.StatusCode == http.StatusNotFound
		detail := fmt.Sprintf("status=%d (expected 404)", resp.StatusCode)
		t.Logf("EP27 AfterDelete_404: %s", detail)
		recordChat("EP27 DELETE /api/chat/conversations/{id}", "AfterDelete_404", pass, detail)
		if !pass {
			t.Errorf("EP27 after delete FAILED: %s", detail)
		}
	})
}

// ─── EP29-30: 危机事件端点（医护端） ─────────────────────────────────────

// TestE2EChatCrisis 测试危机事件 2 个端点的 happy + error 路径。
func TestE2EChatCrisis(t *testing.T) {
	setupHelper(t)
	setupChatSeed(t)
	defer cleanupChatSeed(t)

	patientToken := loginPatient(t)
	staffToken := loginDoctor(t)

	// 先用 SSE 触发一个真实的危机事件，确保 EP29 列表非空
	t.Run("Setup_TriggerCrisisEvent", func(t *testing.T) {
		events := doSSE(t, patientToken, "我想自杀", nil, 60*time.Second)
		sawCrisis := contains(events, "crisis")
		t.Logf("Setup trigger crisis events=%v sawCrisis=%v", eventTypes(events), sawCrisis)
		if !sawCrisis {
			t.Logf("⚠️ no crisis event triggered (may already exist in DB)")
		}
	})

	var crisisID int64

	// EP29: GET /api/staff/chat/crisis-events → 200
	t.Run("EP29_ListHappy", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/staff/chat/crisis-events", nil, staffToken)
		defer drainAndClose(resp)
		pass := resp.StatusCode == http.StatusOK
		m := parseJSON(t, resp)
		var itemsCount int
		if pass {
			for _, k := range []string{"items", "total", "page", "page_size"} {
				if _, ok := m[k]; !ok {
					pass = false
				}
			}
			items, _ := m["items"].([]any)
			itemsCount = len(items)
			if len(items) > 0 {
				first, _ := items[0].(map[string]any)
				for _, k := range []string{"id", "patient_id", "conversation_id", "triggered_content", "matched_keywords", "level", "handled", "created_at"} {
					if _, ok := first[k]; !ok {
						pass = false
					}
				}
				// 提取 id 用于 EP30
				if idStr, ok := first["id"].(string); ok {
					var id int
					if _, err := fmt.Sscanf(idStr, "%d", &id); err == nil && id > 0 {
						crisisID = int64(id)
					}
				}
			}
		}
		detail := fmt.Sprintf("status=200 items=%d crisisID=%d", itemsCount, crisisID)
		t.Logf("EP29 ListHappy: %s", detail)
		recordChat("EP29 GET /api/staff/chat/crisis-events", "ListHappy", pass, detail)
		if !pass {
			t.Errorf("EP29 list happy FAILED")
		}
	})

	// EP29: 过滤 level=high
	t.Run("EP29_FilterLevelHigh", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/staff/chat/crisis-events?level=high", nil, staffToken)
		defer drainAndClose(resp)
		pass := resp.StatusCode == http.StatusOK
		m := parseJSON(t, resp)
		items, _ := m["items"].([]any)
		// 所有 item 的 level 都应为 high
		allHigh := true
		for _, it := range items {
			if m2, ok := it.(map[string]any); ok && m2["level"] != "high" {
				allHigh = false
			}
		}
		pass = pass && allHigh
		detail := fmt.Sprintf("status=200 items=%d allHigh=%v", len(items), allHigh)
		t.Logf("EP29 FilterLevelHigh: %s", detail)
		recordChat("EP29 GET /api/staff/chat/crisis-events", "FilterLevelHigh", pass, detail)
		if !pass {
			t.Errorf("EP29 filter level FAILED: %s", detail)
		}
	})

	// EP29 错误：level 非法 → 400
	t.Run("EP29_InvalidLevel_400", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/staff/chat/crisis-events?level=invalid", nil, staffToken)
		defer drainAndClose(resp)
		pass := resp.StatusCode == http.StatusBadRequest
		detail := fmt.Sprintf("status=%d (expected 400)", resp.StatusCode)
		t.Logf("EP29 InvalidLevel_400: %s", detail)
		recordChat("EP29 GET /api/staff/chat/crisis-events", "InvalidLevel_400", pass, detail)
		if !pass {
			t.Errorf("EP29 invalid level FAILED: %s", detail)
		}
	})

	// EP29 错误：patient token → 403
	t.Run("EP29_PatientForbidden_403", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/staff/chat/crisis-events", nil, patientToken)
		defer drainAndClose(resp)
		pass := resp.StatusCode == http.StatusForbidden
		detail := fmt.Sprintf("status=%d (expected 403)", resp.StatusCode)
		t.Logf("EP29 PatientForbidden_403: %s", detail)
		recordChat("EP29 GET /api/staff/chat/crisis-events", "PatientForbidden_403", pass, detail)
		if !pass {
			t.Errorf("EP29 patient forbidden FAILED: %s", detail)
		}
	})

	// EP29 错误：匿名 → 401
	t.Run("EP29_NoAuth_401", func(t *testing.T) {
		resp := doReq(t, "GET", "/api/staff/chat/crisis-events", nil, "")
		defer drainAndClose(resp)
		pass := resp.StatusCode == http.StatusUnauthorized
		detail := fmt.Sprintf("status=%d (expected 401)", resp.StatusCode)
		t.Logf("EP29 NoAuth_401: %s", detail)
		recordChat("EP29 GET /api/staff/chat/crisis-events", "NoAuth_401", pass, detail)
		if !pass {
			t.Errorf("EP29 no auth FAILED: %s", detail)
		}
	})

	// EP30: POST /api/staff/chat/crisis-events/{id}/handle → 200
	t.Run("EP30_HandleHappy", func(t *testing.T) {
		if crisisID == 0 {
			t.Skip("no crisisID in DB, skip handle happy")
		}
		// 先 SELECT 一个未处理的 crisis 用于测试
		// 直接尝试 handle；若已被处理（409）则视为 pass（幂等）
		body := jsonBody(t, map[string]any{"note": "E2E 已处理"})
		resp := doReq(t, "POST", fmt.Sprintf("/api/staff/chat/crisis-events/%d/handle", crisisID), body, staffToken)
		defer drainAndClose(resp)
		pass := resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusConflict
		detail := fmt.Sprintf("status=%d (expected 200 or 409 if already handled)", resp.StatusCode)
		t.Logf("EP30 HandleHappy: %s", detail)
		recordChat("EP30 POST /api/staff/chat/crisis-events/{id}/handle", "HandleHappy", pass, detail)
		if !pass {
			t.Errorf("EP30 handle happy FAILED: %s", detail)
		}
	})

	// EP30 错误：不存在 → 404
	t.Run("EP30_NotFound_404", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"note": "test"})
		resp := doReq(t, "POST", "/api/staff/chat/crisis-events/999999/handle", body, staffToken)
		defer drainAndClose(resp)
		pass := resp.StatusCode == http.StatusNotFound
		detail := fmt.Sprintf("status=%d (expected 404)", resp.StatusCode)
		t.Logf("EP30 NotFound_404: %s", detail)
		recordChat("EP30 POST /api/staff/chat/crisis-events/{id}/handle", "NotFound_404", pass, detail)
		if !pass {
			t.Errorf("EP30 not found FAILED: %s", detail)
		}
	})

	// EP30 错误：patient token → 403
	t.Run("EP30_PatientForbidden_403", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"note": "test"})
		resp := doReq(t, "POST", "/api/staff/chat/crisis-events/1/handle", body, patientToken)
		defer drainAndClose(resp)
		pass := resp.StatusCode == http.StatusForbidden
		detail := fmt.Sprintf("status=%d (expected 403)", resp.StatusCode)
		t.Logf("EP30 PatientForbidden_403: %s", detail)
		recordChat("EP30 POST /api/staff/chat/crisis-events/{id}/handle", "PatientForbidden_403", pass, detail)
		if !pass {
			t.Errorf("EP30 patient forbidden FAILED: %s", detail)
		}
	})

	// EP30 错误：匿名 → 401
	t.Run("EP30_NoAuth_401", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"note": "test"})
		resp := doReq(t, "POST", "/api/staff/chat/crisis-events/1/handle", body, "")
		defer drainAndClose(resp)
		pass := resp.StatusCode == http.StatusUnauthorized
		detail := fmt.Sprintf("status=%d (expected 401)", resp.StatusCode)
		t.Logf("EP30 NoAuth_401: %s", detail)
		recordChat("EP30 POST /api/staff/chat/crisis-events/{id}/handle", "NoAuth_401", pass, detail)
		if !pass {
			t.Errorf("EP30 no auth FAILED: %s", detail)
		}
	})
}

// ─── 汇总 ───────────────────────────────────────────────────────────────

// TestE2EChatModuleSummary 在所有 chat 模块测试结束后输出汇总。
// 依赖 Go test 执行顺序：同包内按文件名字典序执行，本文件 chat_module_e2e_test.go 在
// e2e_api_test.go 之前（c < e），故 TestE2EChatSSE/Conversations/Crisis 已先运行。
func TestE2EChatModuleSummary(t *testing.T) {
	if len(chatResults) == 0 {
		t.Skip("no chat results collected (chat module tests may have been skipped)")
	}
	passCount := 0
	failCount := 0
	t.Logf("\n")
	t.Logf("════════════════════════════════════════════════════════════════")
	t.Logf("  Chat 模块 E2E 测试汇总（共 %d 条用例）", len(chatResults))
	t.Logf("════════════════════════════════════════════════════════════════")
	for _, r := range chatResults {
		status := "PASS"
		if !r.Pass {
			status = "FAIL"
			failCount++
		} else {
			passCount++
		}
		t.Logf("  [%s] %s :: %s", status, r.Endpoint, r.Case)
		if !r.Pass {
			t.Logf("         └─ %s", r.Detail)
		}
	}
	t.Logf("───────────────────────────────────────────────────────────────")
	t.Logf("  PASS: %d    FAIL: %d    TOTAL: %d", passCount, failCount, len(chatResults))
	t.Logf("════════════════════════════════════════════════════════════════\n")
	if failCount > 0 {
		t.Errorf("chat module e2e: %d case(s) failed", failCount)
	}
}
