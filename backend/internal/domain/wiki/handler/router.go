package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"health-nexus/internal/domain/wiki/service"
	"health-nexus/internal/middleware"
	"health-nexus/internal/shared/contextkeys"
	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/response"
)

// Router 装配 wiki 域全部 19 个 HTTP 端点。
// 公开端点无中间件；医护端统一挂载 JWTAuth + RequireStaff（契约 §0.4 权限矩阵）。
type Router struct {
	public    *PublicHandler
	staff     *StaffArticleHandler
	reference *ReferenceHandler
	auth      *middleware.Authenticator
}

// NewRouter 构造 wiki 域路由器。
func NewRouter(
	public *PublicHandler, staff *StaffArticleHandler, reference *ReferenceHandler, auth *middleware.Authenticator,
) *Router {
	return &Router{public: public, staff: staff, reference: reference, auth: auth}
}

// Mount 挂载 wiki 域路由到 chi.Router。
// 路由清单（契约 §4-5）：
//   - GET    /api/wiki/articles                       公开列表
//   - GET    /api/wiki/articles/{article_id}          公开详情
//   - POST   /api/staff/wiki/articles                 创建文章
//   - GET    /api/staff/wiki/articles                 医护文章列表
//   - GET    /api/staff/wiki/articles/{article_id}    医护文章详情（编辑回填）
//   - PUT    /api/staff/wiki/articles/{article_id}    更新文章
//   - DELETE /api/staff/wiki/articles/{article_id}    软删除
//   - POST   /api/staff/wiki/articles/{article_id}/submit   提交审核
//   - POST   /api/staff/wiki/articles/{article_id}/approve  审核通过
//   - POST   /api/staff/wiki/articles/{article_id}/reject   驳回
//   - POST   /api/staff/wiki/articles/{article_id}/archive  归档（published→archived）
//   - POST   /api/staff/wiki/articles/{article_id}/unarchive 恢复归档（archived→published）
//   - GET    /api/staff/wiki/articles/{article_id}/chunks       列出生效切片
//   - POST   /api/staff/wiki/articles/{article_id}/revectorize  重新切片向量化
//   - POST   /api/staff/wiki/references               发起引用申请
//   - GET    /api/staff/wiki/references               引用授权列表
//   - GET    /api/staff/wiki/references/articles      可引用的公开文章列表
//   - POST   /api/staff/wiki/references/{reference_id}/approve  审核通过
//   - POST   /api/staff/wiki/references/{reference_id}/reject   驳回
//   - DELETE /api/staff/wiki/references/{reference_id}          撤销
func (rt *Router) Mount(r chi.Router) {
	// 公开端点：匿名可访问，无鉴权中间件（契约 §4.1/4.2 明确匿名可访问）。
	r.Route("/api/wiki/articles", func(r chi.Router) {
		r.Get("/", rt.public.List)
		r.Get("/featured", rt.public.Featured)
		r.Get("/{article_id}", rt.public.Detail)
	})

	// 医护端：JWT + RequireStaff + DataIsolation（DOCTOR/NURSE/DEPT_ADMIN/SUPER_ADMIN 均可访问，具体权限由 service 层校验）。
	r.Group(func(r chi.Router) {
		r.Use(middleware.JWTAuth(rt.auth), middleware.RequireStaff(), middleware.DataIsolation())

		r.Route("/api/staff/wiki/articles", func(r chi.Router) {
			r.Post("/", rt.staff.Create)
			r.Get("/", rt.staff.List)
			r.Get("/{article_id}", rt.staff.Get)
			r.Put("/{article_id}", rt.staff.Update)
			r.Delete("/{article_id}", rt.staff.Delete)
			r.Post("/{article_id}/submit", rt.staff.Submit)
			r.Post("/{article_id}/approve", rt.staff.Approve)
			r.Post("/{article_id}/reject", rt.staff.Reject)
			r.Post("/{article_id}/archive", rt.staff.Archive)
			r.Post("/{article_id}/unarchive", rt.staff.Unarchive)
			r.Post("/{article_id}/featured", rt.staff.SetFeatured)
			r.Get("/{article_id}/chunks", rt.staff.Chunks)
			r.Post("/{article_id}/revectorize", rt.staff.Revectorize)
		})

		r.Route("/api/staff/wiki/references", func(r chi.Router) {
			r.Post("/", rt.reference.Apply)
			r.Get("/", rt.reference.List)
			r.Get("/articles", rt.reference.ListReferenceableArticles)
			r.Post("/{reference_id}/approve", rt.reference.Approve)
			r.Post("/{reference_id}/reject", rt.reference.Reject)
			r.Delete("/{reference_id}", rt.reference.Revoke)
		})
	})
}

// ============ 共享辅助 ============

// currentActor 从 ctx 提取医护身份（JWTAuth 以 int64/string/int64 写入，不通过 FromCtx）。
// 返回 service.Actor 供 service 层鉴权使用。
func currentActor(r *http.Request) (service.Actor, error) {
	ctx := r.Context()
	uid, ok := ctx.Value(contextkeys.UserID).(int64)
	if !ok || uid <= 0 {
		return service.Actor{}, apperrors.Unauthorized("UNAUTHORIZED", "missing user identity")
	}
	role, _ := ctx.Value(contextkeys.UserRole).(string)
	if role == "" {
		return service.Actor{}, apperrors.Unauthorized("UNAUTHORIZED", "missing user role")
	}
	deptID, _ := ctx.Value(contextkeys.DeptID).(int64) // omitempty，PATIENT/部分账号可能为 0
	return service.Actor{UserID: uid, Role: role, DeptID: deptID}, nil
}

// parseArticleID 解析 {article_id} 路径参数为 int64。
func parseArticleID(r *http.Request) (int64, error) {
	return parseIDParam(r, "article_id", "WIKI_INVALID_ARTICLE_ID", "article_id 无效")
}

// parseReferenceID 解析 {reference_id} 路径参数为 int64。
func parseReferenceID(r *http.Request) (int64, error) {
	return parseIDParam(r, "reference_id", "WIKI_INVALID_REFERENCE_ID", "reference_id 无效")
}

// parseIDParam 从 chi URL 路径参数解析 int64。缺失返回 400，格式错误返回 400。
func parseIDParam(r *http.Request, key, errCode, errMsg string) (int64, error) {
	raw := chi.URLParam(r, key)
	if raw == "" {
		return 0, apperrors.BadRequest(errCode, key+" 参数缺失")
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 {
		return 0, apperrors.BadRequest(errCode, errMsg)
	}
	return n, nil
}

// parseOptionalInt64Query 解析可选 int64 查询参数。空字符串返回 nil（不过滤）。
func parseOptionalInt64Query(r *http.Request, key string) (*int64, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 1 {
		return nil, apperrors.Validation("VALIDATION_INVALID_ID", key+" 参数无效")
	}
	return &n, nil
}

// maxBodyBytes 请求体大小上限（1MB，与 auth handler 一致）。
const maxBodyBytes = 1 << 20

// decodeJSON 解析请求体到 dst。空 body 或格式错误返回 422。
// 严格模式：拒绝未知字段，避免客户端笔误被静默忽略。
// 限制请求体 1MB（与 auth handler 一致）；文章 content 可含长文本，1MB 仍远够用。
func decodeJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return apperrors.Validation("WIKI_EMPTY_BODY", "请求体不能为空")
	}
	r.Body = http.MaxBytesReader(nil, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return apperrors.Validation("WIKI_INVALID_JSON", "请求体格式错误")
	}
	return nil
}

// writeSuccess 写入 {success: true} 响应（动作类端点统一格式）。
func writeSuccess(w http.ResponseWriter) {
	response.WriteOK(w, map[string]bool{"success": true})
}
