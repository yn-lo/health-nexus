package handler

import (
	"context"
	"net/http"

	"health-nexus/internal/domain/wiki/service"
	"health-nexus/internal/shared/pagination"
	"health-nexus/internal/shared/response"
)

// StaffArticleHandler 医护端文章管理（10 个端点，契约 §4.3~4.13）。
// 所有方法需 JWTAuth + RequireStaff 中间件（在 router.Mount 中挂载）。
type StaffArticleHandler struct {
	svc *service.ArticleService
}

// NewStaffArticleHandler 构造医护端文章 handler。
func NewStaffArticleHandler(svc *service.ArticleService) *StaffArticleHandler {
	return &StaffArticleHandler{svc: svc}
}

// createArticleRequest 创建文章请求体（契约 §4.3）。
type createArticleRequest struct {
	Title          string `json:"title"`
	Content        string `json:"content"`
	Summary        string `json:"summary"`
	CoverURL       string `json:"cover_url"`
	DepartmentID   int64  `json:"department_id"`
	AllowReference bool   `json:"allow_reference"`
}

// Create POST /api/staff/wiki/articles — 创建草稿文章（REQ-WIKI-003）。
func (h *StaffArticleHandler) Create(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	var req createArticleRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	dto, err := h.svc.Create(r.Context(), service.CreateInput{
		Title:          req.Title,
		Content:        req.Content,
		Summary:        req.Summary,
		CoverImageURL:  req.CoverURL,
		DepartmentID:   req.DepartmentID,
		AllowReference: req.AllowReference,
		Actor:          actor,
	})
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteCreated(w, dto)
}

// List GET /api/staff/wiki/articles — 我的文章列表（含所有状态）。
// 查询参数：status（可选）、department_id（可选，仅超管生效）、page、page_size。
func (h *StaffArticleHandler) List(w http.ResponseWriter, r *http.Request) {
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
	deptID, err := parseOptionalInt64Query(r, "department_id")
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	items, total, err := h.svc.ListMine(r.Context(), service.ListMineInput{
		Status:       r.URL.Query().Get("status"),
		DepartmentID: deptID,
		Actor:        actor,
	}, p.PageSize, p.Offset())
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, pagination.NewResult(items, total, p))
}

// Get GET /api/staff/wiki/articles/{article_id} — 获取单篇文章详情（编辑回填用）。
// 鉴权：仅作者或超管可读（由 service.GetMine 校验）。
func (h *StaffArticleHandler) Get(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	id, err := parseArticleID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	dto, err := h.svc.GetMine(r.Context(), id, actor)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, dto)
}

// Chunks GET /api/staff/wiki/articles/{article_id}/chunks — 列出文章生效切片（契约 §4.12）。
// 鉴权：仅作者或超管可读。用于编辑页诊断 RAG 切片状态。
func (h *StaffArticleHandler) Chunks(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	id, err := parseArticleID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	items, err := h.svc.ListChunks(r.Context(), id, actor)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, map[string]any{"items": items, "total": len(items)})
}

// Revectorize POST /api/staff/wiki/articles/{article_id}/revectorize — 重新切片向量化（契约 §4.13）。
// 鉴权：仅作者或超管可触发；仅 published 状态可重新切片。
func (h *StaffArticleHandler) Revectorize(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	id, err := parseArticleID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	if err := h.svc.Revectorize(r.Context(), id, actor); err != nil {
		response.WriteError(w, r, err)
		return
	}
	writeSuccess(w)
}

// updateArticleRequest 更新文章请求体（指针字段 nil 表示不更新，契约 §4.5）。
// version 为客户端加载文章时的版本号；传入则启用乐观锁，并发编辑冲突返回 409。
type updateArticleRequest struct {
	Title          *string `json:"title,omitempty"`
	Content        *string `json:"content,omitempty"`
	Summary        *string `json:"summary,omitempty"`
	CoverURL       *string `json:"cover_url,omitempty"`
	AllowReference *bool   `json:"allow_reference,omitempty"`
	Version        *int    `json:"version,omitempty"`
}

// Update PUT /api/staff/wiki/articles/{article_id} — 更新文章（REQ-WIKI-005/015）。
func (h *StaffArticleHandler) Update(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	id, err := parseArticleID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	var req updateArticleRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	dto, err := h.svc.Update(r.Context(), service.UpdateInput{
		Title:           req.Title,
		Content:         req.Content,
		Summary:         req.Summary,
		CoverImageURL:   req.CoverURL,
		AllowReference:  req.AllowReference,
		ArticleID:       id,
		Actor:           actor,
		ExpectedVersion: req.Version,
	})
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, dto)
}

// Delete DELETE /api/staff/wiki/articles/{article_id} — 软删除文章（REQ-WIKI-004）。
func (h *StaffArticleHandler) Delete(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	id, err := parseArticleID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	if err := h.svc.Delete(r.Context(), id, actor); err != nil {
		response.WriteError(w, r, err)
		return
	}
	writeSuccess(w)
}

// Submit POST /api/staff/wiki/articles/{article_id}/submit — 提交审核（draft→pending，REQ-WIKI-009）。
func (h *StaffArticleHandler) Submit(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	id, err := parseArticleID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	if err := h.svc.SubmitForReview(r.Context(), id, actor); err != nil {
		response.WriteError(w, r, err)
		return
	}
	writeSuccess(w)
}

// approveArticleRequest 审核通过请求体（note 可选，契约 §4.8）。
type approveArticleRequest struct {
	Note string `json:"note,omitempty"`
}

// Approve POST /api/staff/wiki/articles/{article_id}/approve — 审核通过（pending→published，REQ-WIKI-009~012）。
// 管理员可自审；事务提交后异步入队向量化任务。
func (h *StaffArticleHandler) Approve(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	id, err := parseArticleID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	var req approveArticleRequest
	// spec §4.8：请求体 {note?: string}，body 可选——空 body 等同 note=""
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			response.WriteError(w, r, err)
			return
		}
	}
	if err := h.svc.Approve(r.Context(), service.ApproveInput{
		ArticleID: id,
		Note:      req.Note,
		Actor:     actor,
	}); err != nil {
		response.WriteError(w, r, err)
		return
	}
	writeSuccess(w)
}

// rejectArticleRequest 驳回请求体（reason 必填，契约 §4.9）。
type rejectArticleRequest struct {
	Reason string `json:"reason"`
}

// Reject POST /api/staff/wiki/articles/{article_id}/reject — 驳回（pending→draft，REQ-WIKI-009/010）。
func (h *StaffArticleHandler) Reject(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	id, err := parseArticleID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	var req rejectArticleRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	if err := h.svc.Reject(r.Context(), service.RejectInput{
		ArticleID: id,
		Reason:    req.Reason,
		Actor:     actor,
	}); err != nil {
		response.WriteError(w, r, err)
		return
	}
	writeSuccess(w)
}

// Archive POST /api/staff/wiki/articles/{article_id}/archive — 归档已发布文章（published→archived，REQ-WIKI-001）。
func (h *StaffArticleHandler) Archive(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	id, err := parseArticleID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	if err := h.svc.Archive(r.Context(), id, actor); err != nil {
		response.WriteError(w, r, err)
		return
	}
	writeSuccess(w)
}

// Unarchive POST /api/staff/wiki/articles/{article_id}/unarchive — 恢复归档文章（archived→published）。
// 仅管理员可执行；恢复后文章重新对公众可见，自动入队向量化重建切片。
func (h *StaffArticleHandler) Unarchive(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	id, err := parseArticleID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	if err := h.svc.Unarchive(r.Context(), id, actor); err != nil {
		response.WriteError(w, r, err)
		return
	}
	writeSuccess(w)
}

type setFeaturedRequest struct {
	Rank int `json:"rank"`
}

func (h *StaffArticleHandler) SetFeatured(w http.ResponseWriter, r *http.Request) {
	actorIDAction(w, r, parseArticleID,
		func(ctx context.Context, id int64, req *setFeaturedRequest, actor service.Actor) error {
			return h.svc.SetFeaturedRank(ctx, id, req.Rank, actor)
		})
}
