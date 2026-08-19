package handler

import (
	"context"
	"net/http"

	"health-nexus/internal/domain/wiki/service"
	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/pagination"
	"health-nexus/internal/shared/response"
)

// ReferenceHandler 跨科室引用授权（6 个端点，契约 §5.1~5.6）。
type ReferenceHandler struct {
	svc *service.ReferenceService
	art *service.ArticleService
}

// NewReferenceHandler 构造引用授权 handler。
func NewReferenceHandler(svc *service.ReferenceService, art *service.ArticleService) *ReferenceHandler {
	return &ReferenceHandler{svc: svc, art: art}
}

// applyReferenceRequest 发起引用申请请求体（契约 §5.1）。
type applyReferenceRequest struct {
	ArticleID    int64 `json:"article_id"`
	TargetDeptID int64 `json:"target_dept_id"`
}

// Apply POST /api/staff/wiki/references — 发起跨科室引用申请（REQ-WIKI-019/021/022）。
func (h *ReferenceHandler) Apply(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	var req applyReferenceRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	dto, err := h.svc.Apply(r.Context(), service.ApplyInput{
		ArticleID:    req.ArticleID,
		TargetDeptID: req.TargetDeptID,
		Actor:        actor,
	})
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteCreated(w, dto)
}

// List GET /api/staff/wiki/references — 引用授权列表（REQ-WIKI-022）。
// 查询参数：status（可选）、direction（outgoing|incoming，可选）、page、page_size。
func (h *ReferenceHandler) List(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	p, err := pagination.Parse(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	items, total, err := h.svc.List(r.Context(), service.ListInput{
		Status:    r.URL.Query().Get("status"),
		Direction: r.URL.Query().Get("direction"),
		Actor:     actor,
	}, p.PageSize, p.Offset())
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, pagination.NewResult(items, total, p))
}

// noteRequest 通用 note 可选请求体（契约 §5.3）。
type noteRequest struct {
	Note string `json:"note,omitempty"`
}

// reasonRequest 通用 reason 必填请求体（契约 §5.4）。
type reasonRequest struct {
	Reason string `json:"reason"`
}

// Approve POST /api/staff/wiki/references/{reference_id}/approve — 审核通过引用申请（REQ-WIKI-021/022）。
// 仅源科室 DEPT_ADMIN / SUPER_ADMIN 可审核。
func (h *ReferenceHandler) Approve(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	id, err := parseReferenceID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	var req noteRequest
	// spec §5.3：请求体 {note?: string}，body 可选——空 body 等同 note=""
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			response.WriteError(w, r, err)
			return
		}
	}
	if err := h.svc.ApproveReference(r.Context(), id, req.Note, actor); err != nil {
		response.WriteError(w, r, err)
		return
	}
	writeSuccess(w)
}

// Reject POST /api/staff/wiki/references/{reference_id}/reject — 驳回引用申请（REQ-WIKI-022）。
func (h *ReferenceHandler) Reject(w http.ResponseWriter, r *http.Request) {
	actorIDAction(w, r, parseReferenceID,
		func(ctx context.Context, id int64, req *reasonRequest, actor service.Actor) error {
			return h.svc.RejectReference(ctx, id, req.Reason, actor)
		})
}

// Revoke DELETE /api/staff/wiki/references/{reference_id} — 撤销引用授权（approved→revoked，REQ-WIKI-022）。
func (h *ReferenceHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		response.WriteError(w, r, apperrors.ServiceUnavailable("WIKI_REFERENCE_UNAVAILABLE", "引用授权服务未初始化"))
		return
	}
	actor, err := currentActor(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	id, err := parseReferenceID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	if err := h.svc.RevokeReference(r.Context(), id, actor); err != nil {
		response.WriteError(w, r, err)
		return
	}
	writeSuccess(w)
}

// ListReferenceableArticles GET /api/staff/wiki/references/articles — 可引用的公开文章列表。
// 返回其他科室的 allow_reference=true 且已发布的文章，排除本科室自己的文章。
// 查询参数：search（可选，搜索标题）、page、page_size。
func (h *ReferenceHandler) ListReferenceableArticles(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	p, err := pagination.Parse(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	// 仅查询 allow_reference=true 且排除本科室的文章
	t := true
	excludeDeptID := actor.DeptID
	items, total, err := h.art.ListPublished(r.Context(), nil, &t, &excludeDeptID, "", p.PageSize, p.Offset())
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, pagination.NewResult(items, total, p))
}
