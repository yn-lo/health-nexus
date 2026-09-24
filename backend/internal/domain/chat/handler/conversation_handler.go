package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"health-nexus/internal/domain/chat/service"
	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/pagination"
	"health-nexus/internal/shared/response"
)

// maxBodyBytes 请求体大小上限（1MB），防大报文耗尽内存。chat 域各 handler 共用。
const maxBodyBytes = 1 << 20

// 消息列表分页上限（契约 §3.6：默认 50，最大 200）。
const (
	defaultMessagesLimit = 50
	maxMessagesLimit     = 200
)

// ConversationHandler 会话管理 HTTP 适配器。
type ConversationHandler struct {
	svc *service.ConversationService
}

// NewConversationHandler 构造会话 handler。
func NewConversationHandler(svc *service.ConversationService) *ConversationHandler {
	return &ConversationHandler{svc: svc}
}

// List GET /api/chat/conversations
// 查询参数：archived（bool，默认 false）、page、page_size
func (h *ConversationHandler) List(w http.ResponseWriter, r *http.Request) {
	patientID, err := currentPatientID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	includeArchived, err := parseBool(r, "archived", false)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	p, err := pagination.Parse(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	items, total, err := h.svc.List(r.Context(), patientID, includeArchived, p.PageSize, p.Offset())
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, pagination.NewResult(items, total, p))
}

// Get GET /api/chat/conversations/{id}
func (h *ConversationHandler) Get(w http.ResponseWriter, r *http.Request) {
	patientID, err := currentPatientID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	id, err := parseUUIDParam(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	conv, err := h.svc.Get(r.Context(), id, patientID)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, conv)
}

// PatchRequest PATCH 请求体（至少一个字段）。
type PatchRequest struct {
	Title    *string `json:"title,omitempty"`
	Archived *bool   `json:"archived,omitempty"`
}

// Patch PATCH /api/chat/conversations/{id}
func (h *ConversationHandler) Patch(w http.ResponseWriter, r *http.Request) {
	patientID, err := currentPatientID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	id, err := parseUUIDParam(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	var req PatchRequest
	r.Body = http.MaxBytesReader(nil, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	// ponytail: 空 body 返回 io.EOF，与未知字段/类型错误统一映射为 CHAT_PATCH_BODY_INVALID，折中，
	// 不区分 EOF 与其他 decode 错误——避免向客户端泄露内部解析细节（安全优先于精确诊断）。
	if err := dec.Decode(&req); err != nil {
		response.WriteError(w, r, apperrors.Validation("CHAT_PATCH_BODY_INVALID", "请求体格式错误"))
		return
	}
	conv, err := h.svc.Patch(r.Context(), id, patientID, service.PatchInput{Title: req.Title, Archived: req.Archived})
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, conv)
}

// Delete DELETE /api/chat/conversations/{id}
func (h *ConversationHandler) Delete(w http.ResponseWriter, r *http.Request) {
	patientID, err := currentPatientID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	id, err := parseUUIDParam(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	if err := h.svc.Delete(r.Context(), id, patientID); err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, map[string]bool{"success": true})
}

// ListMessages GET /api/chat/conversations/{id}/messages
// 查询参数：limit（默认 50，最大 200）、before（UUID，分页游标）
func (h *ConversationHandler) ListMessages(w http.ResponseWriter, r *http.Request) {
	patientID, err := currentPatientID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	id, err := parseUUIDParam(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	limit, err := parseLimit(r, defaultMessagesLimit, maxMessagesLimit)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	var before *uuid.UUID
	if raw := r.URL.Query().Get("before"); raw != "" {
		bid, err := uuid.Parse(raw)
		if err != nil {
			response.WriteError(w, r, apperrors.BadRequest("CHAT_INVALID_BEFORE", "before 格式错误"))
			return
		}
		before = &bid
	}
	msgs, err := h.svc.ListMessages(r.Context(), id, patientID, before, limit)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, msgs)
}

// FeedbackRequest 消息反馈请求体。Feedback 取值 up/down。
type FeedbackRequest struct {
	Feedback string `json:"feedback"`
}

// Feedback POST /api/chat/messages/{id}/feedback
// 记录患者对消息的点赞/点踩。成功返回 204；取值无效 422；消息不存在或不属于当前用户 404。
func (h *ConversationHandler) Feedback(w http.ResponseWriter, r *http.Request) {
	patientID, err := currentPatientID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	id, err := parseUUIDParam(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	var req FeedbackRequest
	r.Body = http.MaxBytesReader(nil, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		response.WriteError(w, r, apperrors.Validation("CHAT_FEEDBACK_BODY_INVALID", "请求体格式错误"))
		return
	}
	if req.Feedback != service.FeedbackSolved &&
		req.Feedback != service.FeedbackPartial &&
		req.Feedback != service.FeedbackUnsolved {
		response.WriteError(w, r, apperrors.Validation("CHAT_FEEDBACK_INVALID", "feedback 取值无效"))
		return
	}
	if err := h.svc.Feedback(r.Context(), id, patientID, req.Feedback); err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteNoContent(w)
}

// parseBool 解析 bool 查询参数。空字符串返回默认值。
func parseBool(r *http.Request, key string, def bool) (bool, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, apperrors.Validation("VALIDATION_INVALID_BOOL", key+" 参数无效")
	}
	return b, nil
}

// parseLimit 解析 limit 参数，def 为默认值，maxLimit 为上限。
func parseLimit(r *http.Request, def, maxLimit int) (int, error) {
	v := r.URL.Query().Get("limit")
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, apperrors.Validation("VALIDATION_INVALID_LIMIT", "limit 参数无效")
	}
	if n > maxLimit {
		n = maxLimit
	}
	return n, nil
}

// feedbackStatsRecentLimit 最近反馈列表条数上限（统计页一次加载，无分页）。
const feedbackStatsRecentLimit = 20

// feedbackContentMaxRunes 统计列表内容截断长度（列表仅作概览，控制报文体积）。
const feedbackContentMaxRunes = 120

// FeedbackItemResponse 最近反馈条目 DTO。
type FeedbackItemResponse struct {
	MessageID      string `json:"message_id"`
	ConversationID string `json:"conversation_id"`
	Feedback       string `json:"feedback"`
	Content        string `json:"content"`
	CreatedAt      string `json:"created_at"`
}

// FeedbackStatsResponse 反馈统计响应 DTO。
type FeedbackStatsResponse struct {
	Total    int64                  `json:"total"`
	Solved   int64                  `json:"solved"`
	Partial  int64                  `json:"partial"`
	Unsolved int64                  `json:"unsolved"`
	Recent   []FeedbackItemResponse `json:"recent"`
}

// truncateRunes 按 rune 截断字符串，超出部分以 … 结尾。
func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}

// FeedbackStats GET /api/staff/chat/feedback/stats
// 反馈三态汇总（total/solved/partial/unsolved）+ 最近反馈列表。
func (h *ConversationHandler) FeedbackStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.svc.FeedbackStats(r.Context(), feedbackStatsRecentLimit)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	recent := make([]FeedbackItemResponse, 0, len(stats.Recent))
	for _, item := range stats.Recent {
		recent = append(recent, FeedbackItemResponse{
			MessageID:      item.MessageID,
			ConversationID: item.ConversationID,
			Feedback:       item.Feedback,
			Content:        truncateRunes(item.Content, feedbackContentMaxRunes),
			CreatedAt:      item.CreatedAt,
		})
	}
	response.WriteOK(w, FeedbackStatsResponse{
		Total:    stats.Total,
		Solved:   stats.Solved,
		Partial:  stats.Partial,
		Unsolved: stats.Unsolved,
		Recent:   recent,
	})
}
