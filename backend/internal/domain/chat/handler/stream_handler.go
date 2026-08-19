// Package handler 实现 chat 域 HTTP 适配：SSE 流式问答、会话管理、危机事件管理。
// Handler 仅做协议适配（解析/序列化），业务逻辑全部委托给 Service。
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"health-nexus/internal/domain/chat/service"
	"health-nexus/internal/shared/constants"
	"health-nexus/internal/shared/contextkeys"
	apperrors "health-nexus/internal/shared/errors"
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
// JSON 请求体：{"message": 必填, ≤2000 字符, "conversation_id": 可选, "selected_dept_id": 可选}
// 使用 POST + JSON body 而非 GET query：2000 字中文消息 URL 编码后约 18KB，
// 超出 Nginx/CDN 默认请求行限制（414），且患者提问（PHI）会残留在 access log。
// SSE 事件：conversation, token, references, safety_warning, crisis, error, done
//   - conversation：认证用户首事件，携带会话 ID（新建/已有），前端据此更新 URL 与后续请求。
//   - safety_warning：纯文本（紧急提醒/拒答）或 {"mode":"replace"|"append","text":...}（输出审查）。
//
// 错误处理：
//   - 预流错误（参数解析失败 / 消息空 / 超长）：HTTP 错误响应（400/422）
//   - 流中错误（404 会话不存在 / 409 锁定 / 503 LLM 不可达）：
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
		_ = sse.Write("error", map[string]any{"message": userFacingMessage(err)})
		_ = sse.Write("done", "[DONE]")
	}
}

// parseStreamInput 解析 JSON 请求体为 service.StreamInput。
// 已认证用户从 context 读取 UserID；匿名用户从 context 读取 DeviceID（UserID=0）。
// 校验：message 必填且 ≤2000 字符；conversation_id 与 selected_dept_id 可选且需为有效 UUID/非负整数。selected_dept_id=0 表示不限科室。
func parseStreamInput(r *http.Request) (service.StreamInput, error) {
	// 已认证用户取 user_id；匿名用户取 device_id
	userID := currentPatientIDOrZero(r)
	deviceID := ""
	if userID == 0 {
		did, ok := r.Context().Value(contextkeys.DeviceID).(string)
		if !ok || did == "" {
			return service.StreamInput{}, apperrors.Unauthorized(
				"UNAUTHORIZED", "missing user_id or device_id in context",
			)
		}
		deviceID = did
	}

	var body struct {
		Message        string `json:"message"`
		ConversationID string `json:"conversation_id"`
		SelectedDeptID *int64 `json:"selected_dept_id"`
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

	return service.StreamInput{
		UserID:         userID,
		DeviceID:       deviceID,
		ConversationID: convID,
		SelectedDeptID: body.SelectedDeptID,
		Message:        msg,
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
}

// sseDataReplacer 将 data 中的换行拆成多条 data: 行（SSE spec 要求）。
// strings.NewReplacer 同一位置优先匹配最长串，故 \r\n 整体处理，不会被拆成两条 data 行；
// 单独的 \r（部分 LLM/代理的行尾）同样处理，避免 \r 混入前端 token 内容。
var sseDataReplacer = strings.NewReplacer("\r\n", "\ndata: ", "\r", "\ndata: ", "\n", "\ndata: ")

// Write 写入一个 SSE 事件并立即 flush。
// 裸字符串（token / done=[DONE] / safety_warning 话术）原样输出，不经 JSON 序列化，
// 以符合 spec §3.1（token data 为裸字符串、done data 为 [DONE] 字面量）。
// 其他类型（数组、map）正常 JSON 序列化。
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
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, payload); err != nil {
		return err
	}
	s.flusher.Flush()
	s.wroteAny = true
	return nil
}
