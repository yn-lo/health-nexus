// Package handler 实现 chat 域 HTTP 适配：SSE 流式问答、会话管理、危机事件管理。
// Handler 仅做协议适配（解析/序列化），业务逻辑全部委托给 Service。
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"health-nexus/internal/domain/chat/service"
	"health-nexus/internal/shared/constants"
	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/identity"
	"health-nexus/internal/shared/response"
)

// StreamHandler SSE 流式问答 HTTP 适配器。
type StreamHandler struct {
	chat *service.ChatSendService
}

// NewStreamHandler 构造 SSE 流式问答 handler。
func NewStreamHandler(chat *service.ChatSendService) *StreamHandler {
	return &StreamHandler{chat: chat}
}

// Stream POST /api/chat/stream
//
// JSON 请求体：{"message": 必填, ≤2000 字符, "conversation_id": 可选, "selected_dept_id": 可选,
// "request_id": 可选（UUID，幂等标识，重试须复用同一值）}
// 使用 POST + JSON body 而非 GET query：2000 字中文消息 URL 编码后约 18KB，
// 超出 Nginx/CDN 默认请求行限制（414），且患者提问（PHI）会残留在 access log。
// SSE 事件：conversation, token, references, answer_replaced, notice, crisis, result, error, done
//   - conversation：首事件，携带会话 ID（新建/已有），前端据此更新 URL 与后续请求。
//   - answer_replaced：{"mode":"replace"|"append","text":...} 正文修正（输出审查 / 拒答话术），
//     修正后的正文即持久化内容。
//   - notice：{"kind":"emergency"|"timeout","text":...} 面向用户的独立提示，不进入答案正文。
//   - result：{"turn_id","user_message_id","assistant_message_id","result_code","references"}
//     本轮权威结果，前端据此替换本地乐观消息，无需猜测或整页回拉。
//
// 错误处理：
//   - 预流错误（参数解析失败 / 消息空 / 超长）：HTTP 错误响应（400/422）
//   - 流中错误（404 会话不存在 / 409 锁定或本轮生成中 / 503 LLM 不可达）：
//     若尚未写入任何 SSE 事件，回退为 HTTP 错误响应；否则写 SSE error 事件。
func (h *StreamHandler) Stream(w http.ResponseWriter, r *http.Request) {
	in, err := parseStreamInput(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}

	// 设置 SSE 响应头（仅在 Write 首次调用时实际发送）
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Nginx 关闭缓冲

	flusher, ok := w.(http.Flusher)
	if !ok {
		response.WriteError(w, r, apperrors.Internal("streaming unsupported", nil))
		return
	}
	sse := &sseWriter{w: w, flusher: flusher}

	// P2：SSE 心跳。高风险问题要"完整生成 + 语义审核"后才返回，最长可达 4 分钟，
	// 期间可能长时间无 token；前端仅靠"有无数据"判断存活会误判连接断开。
	// 独立 goroutine 周期性写 ping 事件（不进入答案正文），使连接活跃状态可观测。
	stopHeartbeat := sse.startHeartbeat(r.Context())
	defer stopHeartbeat()

	// 客户端断开时 r.Context() 自动 cancel，service 内的 LLM 流停止
	if err := h.chat.Stream(r.Context(), in, sse); err != nil {
		if !sse.wroteAny {
			// 未写过任何 SSE 事件：可回退为 HTTP 错误响应
			response.WriteError(w, r, err)
			return
		}
		// 流中错误：写 SSE error 事件 + done 事件作为终止信号。
		// done 是 spec §3.1 规定的流结束标记，error 后必须补 done，
		// 否则客户端 EventSource 会继续等待而连接挂起。
		_ = sse.Write(service.EventError, map[string]any{"message": userFacingMessage(err)})
		_ = sse.Write(service.EventDone, "[DONE]")
	}
}

// DeleteAnonConversation DELETE /api/public/chat/conversations/{id}
// 匿名会话删除：清除服务端瞬态上下文（Redis 环），使匿名"删除对话"与"新对话"语义一致
// （此前仅清本地缓存，服务端上下文仍被下一次请求复用）。
func (h *StreamHandler) DeleteAnonConversation(w http.ResponseWriter, r *http.Request) {
	id := identity.FromRequestOrZero(r)
	if !id.Anon() || id.DeviceID == "" {
		response.WriteError(w, r, apperrors.Unauthorized("UNAUTHORIZED", "missing device_id in context"))
		return
	}
	convID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.WriteError(w, r, apperrors.BadRequest("CHAT_INVALID_ID", "id 格式错误"))
		return
	}
	if err := h.chat.PurgeAnonSession(r.Context(), id.DeviceID, convID.String()); err != nil {
		response.WriteError(w, r, apperrors.Internal("CHAT_ANON_PURGE_FAILED", err))
		return
	}
	response.WriteOK(w, map[string]bool{"success": true})
}

// parseStreamInput 解析 JSON 请求体为 service.StreamInput。
// 身份从 context 解析（shared/identity）：已认证取 user，匿名取 device；拿到后再校验 IsValid。
// 其余校验：message 必填且 ≤2000 字符；conversation_id 与 selected_dept_id 可选且需为有效 UUID/非负整数。selected_dept_id=0 表示不限科室。
func parseStreamInput(r *http.Request) (service.StreamInput, error) {
	id := identity.FromRequestOrZero(r)
	// 身份可信边界的单一校验：认证必携 user，匿名必携 device。
	if !id.IsValid() {
		return service.StreamInput{}, apperrors.Unauthorized(
			"UNAUTHORIZED", "missing user_id or device_id in context",
		)
	}

	var body struct {
		Message        string `json:"message"`
		ConversationID string `json:"conversation_id"`
		SelectedDeptID *int64 `json:"selected_dept_id"`
		RequestID      string `json:"request_id"`
	}
	// 限 1MB 防大报文耗尽内存（匿名端点无鉴权门槛，风险最高；与 auth/wiki handler 一致）。
	r.Body = http.MaxBytesReader(nil, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return service.StreamInput{}, apperrors.BadRequest("CHAT_INVALID_PARAM", "请求体须为 JSON")
	}

	// 提前校验 message，使预流错误完全在 SSE 头设置之前处理
	msg := body.Message
	if msg == "" {
		return service.StreamInput{}, apperrors.BadRequest("CHAT_MESSAGE_EMPTY", "消息内容不能为空")
	}
	if utf8.RuneCountInString(msg) > constants.MaxMessageLength {
		return service.StreamInput{}, apperrors.Validation("CHAT_MESSAGE_TOO_LONG", "消息长度超过 2000 字符")
	}

	var convID *uuid.UUID
	if body.ConversationID != "" {
		id, err := uuid.Parse(body.ConversationID)
		if err != nil {
			return service.StreamInput{}, apperrors.BadRequest("CHAT_INVALID_CONVERSATION_ID", "conversation_id 格式错误")
		}
		convID = &id
	}

	if body.SelectedDeptID != nil && *body.SelectedDeptID < 0 {
		return service.StreamInput{}, apperrors.BadRequest("CHAT_INVALID_DEPT_ID", "selected_dept_id 格式错误")
	}

	// request_id 幂等标识：可选；存在时须为合法 UUID（作为幂等登记 key 的一部分）。
	// 非 UUID 值会污染 key 空间，直接拒绝而非静默忽略。
	requestID := ""
	if body.RequestID != "" {
		if _, err := uuid.Parse(body.RequestID); err != nil {
			return service.StreamInput{}, apperrors.BadRequest("CHAT_INVALID_REQUEST_ID", "request_id 格式错误")
		}
		requestID = body.RequestID
	}

	return service.StreamInput{
		Identity:       id,
		ConversationID: convID,
		SelectedDeptID: body.SelectedDeptID,
		Message:        msg,
		RequestID:      requestID,
	}, nil
}

// userFacingMessage 提取 AppError 的用户可读消息；非 AppError 返回通用兜底。
func userFacingMessage(err error) string {
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		return appErr.Message
	}
	return "服务器内部错误"
}

// sseWriter 实现 service.SSEWriter，封装 SSE 事件写入 + flush。
type sseWriter struct {
	w        http.ResponseWriter
	flusher  http.Flusher
	wroteAny bool
	// mu 保护 w/flusher/wroteAny：心跳 goroutine 与主流写线程可能并发写同一 ResponseWriter。
	mu sync.Mutex
}

// heartbeatInterval SSE 心跳间隔：远小于前端空闲超时（60s），确保长审核期间连接被判存活。
const heartbeatInterval = 15 * time.Second

// startHeartbeat 启动周期性 ping 事件写入，返回停止函数（幂等）。
// 心跳使"连接是否存活"与"是否收到业务数据"解耦——生成 + 审核阶段可能长时间无 token。
func (s *sseWriter) startHeartbeat(ctx context.Context) func() {
	done := make(chan struct{})
	var once sync.Once
	go func() {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				// 心跳载荷为时间戳字符串，前端仅用于刷新空闲计时，不解析语义。
				_ = s.Write(service.EventPing, time.Now().UTC().Format(time.RFC3339))
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

// sseDataReplacer 将 data 中的换行拆成多条 data: 行（SSE spec 要求）。
// strings.NewReplacer 同一位置优先匹配最长串，故 \r\n 整体处理，不会被拆成两条 data 行；
// 单独的 \r（部分 LLM/代理的行尾）同样处理，避免 \r 混入前端 token 内容。
var sseDataReplacer = strings.NewReplacer("\r\n", "\ndata: ", "\r", "\ndata: ", "\n", "\ndata: ")

// Write 写入一个 SSE 事件并立即 flush。
// 裸字符串（token / done=[DONE]）原样输出，不经 JSON 序列化，
// 以符合 spec §3.1（token data 为裸字符串、done data 为 [DONE] 字面量）。
// 其他类型（数组、map、结构体）正常 JSON 序列化。
// ponytail: 错误忽略——SSE 单向推送，写失败（如客户端断开）无法回传给 service，折中；
// service 通过 ctx.Done() 感知客户端断开并停止 LLM 流。
func (s *sseWriter) Write(event string, data any) error {
	var payload []byte
	if str, ok := data.(string); ok {
		payload = []byte(sseDataReplacer.Replace(str))
	} else {
		var err error
		payload, err = json.Marshal(data)
		if err != nil {
			return fmt.Errorf("marshal sse payload: %w", err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, payload); err != nil {
		return err
	}
	s.flusher.Flush()
	s.wroteAny = true
	return nil
}
