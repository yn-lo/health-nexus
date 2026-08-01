package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"health-nexus/internal/domain/base/entity"
	"health-nexus/internal/domain/base/service"
	"health-nexus/internal/middleware"
	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/response"
)

const (
	defaultNotificationLimit = 20
	maxNotificationLimit     = 100
)

// notificationResponse 通知响应 DTO（snake_case，ref_id/recipient_dept_id 可为 null）。
type notificationResponse struct {
	ID              int64     `json:"id"`
	RecipientRole   string    `json:"recipient_role"`
	RecipientDeptID *int64    `json:"recipient_dept_id"`
	Type            string    `json:"type"`
	Title           string    `json:"title"`
	Body            string    `json:"body"`
	RefID           *string   `json:"ref_id"`
	IsRead          bool      `json:"is_read"`
	CreatedAt       time.Time `json:"created_at"`
}

// NotificationHandler 站内通知 HTTP 处理器。
type NotificationHandler struct {
	svc  *service.NotificationService
	auth *middleware.Authenticator
}

// NewNotificationHandler 构造通知处理器。
func NewNotificationHandler(svc *service.NotificationService, auth *middleware.Authenticator) *NotificationHandler {
	return &NotificationHandler{svc: svc, auth: auth}
}

// Mount 挂载通知路由：/api/staff/notifications（JWTAuth + RequireStaff + DataIsolation）。
func (h *NotificationHandler) Mount(r chi.Router) {
	r.Route("/api/staff/notifications", func(r chi.Router) {
		r.Use(middleware.JWTAuth(h.auth), middleware.RequireStaff(), middleware.DataIsolation())
		r.Get("/", h.List)
		r.Get("/unread-count", h.UnreadCount)
		r.Post("/read-all", h.MarkAllRead)
		r.Post("/{id}/read", h.MarkRead)
	})
}

// List GET /api/staff/notifications?limit=20 — 当前用户角色+科室可见的通知列表。
func (h *NotificationHandler) List(w http.ResponseWriter, r *http.Request) {
	role, deptID, err := notificationScope(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	limit := defaultNotificationLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, perr := strconv.Atoi(raw)
		if perr != nil || n < 1 {
			response.WriteError(w, r, apperrors.BadRequest("NOTIF_INVALID_LIMIT", "limit 参数无效"))
			return
		}
		if n > maxNotificationLimit {
			n = maxNotificationLimit
		}
		limit = n
	}
	items, err := h.svc.List(r.Context(), role, deptID, limit)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	resp := make([]notificationResponse, 0, len(items))
	for _, n := range items {
		resp = append(resp, toNotificationResponse(n))
	}
	response.WriteOK(w, resp)
}

// MarkRead POST /api/staff/notifications/{id}/read — 标记单条已读。
func (h *NotificationHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	id, err := parseNotificationID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	if err := h.svc.MarkRead(r.Context(), id); err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, map[string]bool{"success": true})
}

// MarkAllRead POST /api/staff/notifications/read-all — 标记当前角色+科室全部已读。
func (h *NotificationHandler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	role, deptID, err := notificationScope(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	if err := h.svc.MarkAllRead(r.Context(), role, deptID); err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, map[string]bool{"success": true})
}

// UnreadCount GET /api/staff/notifications/unread-count — 未读数量。
func (h *NotificationHandler) UnreadCount(w http.ResponseWriter, r *http.Request) {
	role, deptID, err := notificationScope(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	count, err := h.svc.UnreadCount(r.Context(), role, deptID)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, map[string]int{"count": count})
}

// notificationScope 从 DataScope（DataIsolation 注入）提取角色与科室。
// DeptID<=0 时返回 nil（仅可见广播通知）。
func notificationScope(r *http.Request) (string, *int64, error) {
	scope := middleware.ScopeFromCtx(r.Context())
	if scope == nil || scope.Role == "" {
		return "", nil, apperrors.Unauthorized("UNAUTHORIZED", "missing user identity")
	}
	var deptID *int64
	if scope.DeptID > 0 {
		deptID = &scope.DeptID
	}
	return scope.Role, deptID, nil
}

func parseNotificationID(r *http.Request) (int64, error) {
	raw := chi.URLParam(r, "id")
	if raw == "" {
		return 0, apperrors.BadRequest("NOTIF_INVALID_ID", "id 参数缺失")
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 {
		return 0, apperrors.BadRequest("NOTIF_INVALID_ID", "id 参数无效")
	}
	return n, nil
}

func toNotificationResponse(n *entity.Notification) notificationResponse {
	return notificationResponse{
		ID:              n.ID,
		RecipientRole:   n.RecipientRole,
		RecipientDeptID: n.RecipientDeptID,
		Type:            n.Type,
		Title:           n.Title,
		Body:            n.Body,
		RefID:           n.RefID,
		IsRead:          n.IsRead,
		CreatedAt:       n.CreatedAt,
	}
}
