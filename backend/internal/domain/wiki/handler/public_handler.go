// Package handler 实现 wiki 域的 HTTP 协议适配与路由挂载（14 个端点）。
// 公开端点（匿名可访问）：GET /api/wiki/articles、GET /api/wiki/articles/{article_id}。
// 医护端文章管理（JWT+RequireStaff）：POST/GET/PUT/DELETE/submit/approve/reject。
// 跨科室引用授权（JWT+RequireStaff）：POST/GET references、approve/reject/revoke。
package handler

import (
	"net/http"
	"strings"

	"health-nexus/internal/domain/wiki/service"
	"health-nexus/internal/shared/pagination"
	"health-nexus/internal/shared/response"
)

// homeFeaturedLimit 首页热门文章展示数量。
const homeFeaturedLimit = 3

// PublicHandler 公开文章端点（匿名可访问，契约 §4.1/4.2）。
type PublicHandler struct {
	svc *service.ArticleService
}

// NewPublicHandler 构造公开端点 handler。
func NewPublicHandler(svc *service.ArticleService) *PublicHandler {
	return &PublicHandler{svc: svc}
}

// List GET /api/wiki/articles - 已发布文章列表（匿名可访问）。
// 查询参数：department_id（可选）、allow_reference（可选，true=仅公开文章）、search（可选，标题/摘要模糊匹配）、page、page_size。
func (h *PublicHandler) List(w http.ResponseWriter, r *http.Request) {
	deptID, err := parseOptionalInt64Query(r, "department_id")
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	var allowRef *bool
	if v := r.URL.Query().Get("allow_reference"); v == "true" {
		t := true
		allowRef = &t
	} else if v == "false" {
		f := false
		allowRef = &f
	}
	search := strings.TrimSpace(r.URL.Query().Get("search"))
	p, err := pagination.Parse(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	items, total, err := h.svc.ListPublished(r.Context(), deptID, allowRef, nil, search, p.PageSize, p.Offset())
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, pagination.NewResult(items, total, p))
}

// Detail GET /api/wiki/articles/{article_id} — 已发布文章详情（匿名可访问）。
// 副作用：阅读量 +1（契约 §4.2 规定每次访问 +1，未定义去重）。
func (h *PublicHandler) Detail(w http.ResponseWriter, r *http.Request) {
	id, err := parseArticleID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	dto, err := h.svc.GetPublished(r.Context(), id)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, dto)
}

func (h *PublicHandler) Featured(w http.ResponseWriter, r *http.Request) {
	deptID, err := parseOptionalInt64Query(r, "department_id")
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	items, err := h.svc.ListFeatured(r.Context(), deptID, homeFeaturedLimit)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, map[string]any{"items": items})
}
