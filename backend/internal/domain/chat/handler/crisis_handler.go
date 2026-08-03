package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"health-nexus/internal/domain/chat/service"
	"health-nexus/internal/shared/constants"
	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/pagination"
	"health-nexus/internal/shared/response"
)

// CrisisEventResponse 危机事件响应 DTO（契约 §3.7）。
// HandleNote 用 *string：DB 中 NOT NULL DEFAULT ”，但响应中未处理/无备注时序列化为 null（契约示例 line 265）。
type CrisisEventResponse struct {
	ID               string   `json:"id"`
	PatientID        string   `json:"patient_id"`
	PatientName      string   `json:"patient_name"`
	ConversationID   string   `json:"conversation_id"`
	TriggeredContent string   `json:"triggered_content"`
	MatchedKeywords  []string `json:"matched_keywords"`
	Level            string   `json:"level"`
	Handled          bool     `json:"handled"`
	HandlerID        *string  `json:"handler_id"`
	HandledAt        *string  `json:"handled_at"`
	HandleNote       *string  `json:"handle_note"`
	CreatedAt        string   `json:"created_at"`
}

// CrisisHandler 危机事件管理 HTTP 适配器。
type CrisisHandler struct {
	svc *service.CrisisService
}

// NewCrisisHandler 构造危机事件 handler。
func NewCrisisHandler(svc *service.CrisisService) *CrisisHandler {
	return &CrisisHandler{svc: svc}
}

// List GET /api/staff/chat/crisis-events
// 查询参数：handled（bool，可选）、level（high|medium|low，可选）、page、page_size
func (h *CrisisHandler) List(w http.ResponseWriter, r *http.Request) {
	level := r.URL.Query().Get("level")
	// spec §3.7：level 仅允许 high|medium|low 或空。非法值返回 400，避免透传到 SQL 造成意外结果。
	switch level {
	case "", constants.CrisisLevelHigh, constants.CrisisLevelMedium, constants.CrisisLevelLow:
		// ok
	default:
		response.WriteError(w, r, apperrors.BadRequest("CHAT_CRISIS_LEVEL_INVALID", "level 仅允许 high|medium|low"))
		return
	}
	var handled *bool
	if raw := r.URL.Query().Get("handled"); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			response.WriteError(w, r, apperrors.BadRequest("VALIDATION_INVALID_BOOL", "handled 参数无效"))
			return
		}
		handled = &b
	}
	p, err := pagination.Parse(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	actor, err := currentCrisisActor(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	rows, total, err := h.svc.List(r.Context(), level, handled, actor, p.PageSize, p.Offset())
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	resp := make([]CrisisEventResponse, 0, len(rows))
	for _, row := range rows {
		resp = append(resp, toCrisisResponse(row))
	}
	response.WriteOK(w, pagination.NewResult(resp, total, p))
}

// HandleRequest 处理危机事件请求体。
type HandleRequest struct {
	Note *string `json:"note,omitempty"`
}

// Handle POST /api/staff/chat/crisis-events/{id}/handle
// 请求体：{note?: string}
func (h *CrisisHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		response.WriteError(w, r, apperrors.ServiceUnavailable("CHAT_CRISIS_UNAVAILABLE", "危机事件服务未初始化"))
		return
	}
	actor, err := currentCrisisActor(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	eventID, err := parseInt64Param(r, "id")
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	var req HandleRequest
	// body 可选——空 body 也能处理。限 1MB 防大报文耗尽内存（与 auth/wiki handler 一致）。
	if r.ContentLength > 0 {
		r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.WriteError(w, r, apperrors.Validation("CHAT_CRISIS_BODY_INVALID", "请求体格式错误"))
			return
		}
	}
	note := ""
	if req.Note != nil {
		note = *req.Note
	}
	if err := h.svc.Handle(r.Context(), actor, eventID, note); err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, map[string]bool{"success": true})
}

// toCrisisResponse service.CrisisListItem → CrisisEventResponse DTO。
func toCrisisResponse(row *service.CrisisListItem) CrisisEventResponse {
	resp := CrisisEventResponse{
		ID:               strconv.FormatInt(row.ID, 10),
		PatientID:        strconv.FormatInt(row.PatientID, 10),
		PatientName:      row.PatientName,
		ConversationID:   row.ConversationID,
		TriggeredContent: row.TriggeredContent,
		MatchedKeywords:  row.MatchedKeywords,
		Level:            row.Level,
		Handled:          row.IsHandled,
		HandleNote:       stringPtrFromEmpty(row.HandleNote),
		CreatedAt:        row.CreatedAt,
	}
	if row.HandlerID != nil {
		s := strconv.FormatInt(*row.HandlerID, 10)
		resp.HandlerID = &s
	}
	resp.HandledAt = row.HandledAt
	if resp.MatchedKeywords == nil {
		resp.MatchedKeywords = []string{}
	}
	return resp
}

// stringPtrFromEmpty 空字符串返回 nil（JSON null），非空返回指针。
// ponytail: DB 列 handle_note NOT NULL DEFAULT ""，DTO 层把 "" 映射为 null 以对齐 spec §3.7 示例，折中。
func stringPtrFromEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
