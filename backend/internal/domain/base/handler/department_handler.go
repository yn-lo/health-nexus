// Package handler 实现 base 域的 HTTP 协议适配与路由挂载。
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"health-nexus/internal/domain/base/service"
	"health-nexus/internal/middleware"
	"health-nexus/internal/shared/contextkeys"
	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/response"
)

// DepartmentService 是 handler 需要的科室业务能力（消费者定义，ISP）。
// 由 service 包实现，通过 InitializeApp 注入。
type DepartmentService interface {
	ListVisible(ctx context.Context, userID, deptID int64, active bool) ([]service.DepartmentDTO, error)
	ListPublic(ctx context.Context) ([]service.DepartmentDTO, error)
	Create(ctx context.Context, in service.CreateDeptInput) (*service.DepartmentTreeDTO, error)
	ListTree(ctx context.Context, actor service.Actor) ([]service.DepartmentTreeDTO, error)
	Get(ctx context.Context, id int64, actor service.Actor) (*service.DepartmentTreeDTO, error)
	Update(ctx context.Context, id int64, in service.UpdateDeptInput) (*service.DepartmentTreeDTO, error)
	Delete(ctx context.Context, id int64, actor service.Actor) (bool, error)
}

// DepartmentHandler base 域 HTTP 处理器。
type DepartmentHandler struct {
	svc  DepartmentService
	auth *middleware.Authenticator // 用于 Mount 挂载 JWTAuth 中间件
}

// NewDepartmentHandler 构造处理器。
func NewDepartmentHandler(svc DepartmentService, auth *middleware.Authenticator) *DepartmentHandler {
	return &DepartmentHandler{svc: svc, auth: auth}
}

// ListDepartments GET /api/base/departments — 当前用户可见的科室列表（REQ-BASE-001/002/004）。
// 认证：JWT + RequireStaff（在 Mount 中挂载）。
// 查询参数：active（bool，可选，默认 true）。
func (h *DepartmentHandler) ListDepartments(w http.ResponseWriter, r *http.Request) {
	// UserID/DeptID 由 JWT 中间件以 int64 写入 context（非字符串，不用 FromCtx）。
	userID, ok := r.Context().Value(contextkeys.UserID).(int64)
	if !ok || userID <= 0 {
		response.WriteError(w, r, apperrors.Unauthorized("UNAUTHORIZED", "missing user identity"))
		return
	}
	deptID, _ := r.Context().Value(contextkeys.DeptID).(int64) // omitempty，可能为 0

	active := true // 默认仅启用科室（REQ-BASE-002）
	if q := r.URL.Query().Get("active"); q != "" {
		b, err := strconv.ParseBool(q)
		if err != nil {
			response.WriteError(w, r, apperrors.BadRequest("BASE_INVALID_ACTIVE", "active 参数无效"))
			return
		}
		active = b
	}

	dtos, err := h.svc.ListVisible(r.Context(), userID, deptID, active)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, dtos)
}

// ListPublicDepartments GET /api/public/departments — 公共科室列表（REQ-BASE-013）。
// 无需认证，供匿名用户选择咨询科室。仅返回 is_public=TRUE AND is_active=TRUE。
func (h *DepartmentHandler) ListPublicDepartments(w http.ResponseWriter, r *http.Request) {
	dtos, err := h.svc.ListPublic(r.Context())
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, dtos)
}

// ============ Staff CRUD（管理员，契约 §2.2-2.6） ============

// createDepartmentRequest 创建科室请求体（契约 §2.4）。
// ParentID 为 *int64：null/省略 → 根科室；数字 → 父科室 ID。
// IsPublic/IsActive 用 *bool 以支持显式默认值（is_public 默认 false，is_active 默认 true）。
type createDepartmentRequest struct {
	Name        string `json:"name"`
	ParentID    *int64 `json:"parent_id,omitempty"`
	IsPublic    *bool  `json:"is_public,omitempty"`
	IsActive    *bool  `json:"is_active,omitempty"`
	Description string `json:"description,omitempty"`
}

// CreateDepartment POST /api/staff/base/departments — 创建科室（REQ-BASE-005/011）。
func (h *DepartmentHandler) CreateDepartment(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	var req createDepartmentRequest
	if err := decodeDeptJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	// 默认值：is_public=false，is_active=true
	isPublic := false
	if req.IsPublic != nil {
		isPublic = *req.IsPublic
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	dto, err := h.svc.Create(r.Context(), service.CreateDeptInput{
		Name:        req.Name,
		ParentID:    req.ParentID,
		IsPublic:    isPublic,
		IsActive:    isActive,
		Description: req.Description,
		Actor:       actor,
	})
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteCreated(w, dto)
}

// ListTree GET /api/staff/base/departments — 科室树（扁平数组，前端按 parent_id 组装）。
// SUPER_ADMIN 返回全树；DEPT_ADMIN 仅返回主科室子树（REQ-BASE-006/011）。
func (h *DepartmentHandler) ListTree(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	dtos, err := h.svc.ListTree(r.Context(), actor)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, dtos)
}

// GetDepartment GET /api/staff/base/departments/{id} — 科室详情（REQ-BASE-007/011）。
func (h *DepartmentHandler) GetDepartment(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	id, err := parseDeptID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	dto, err := h.svc.Get(r.Context(), id, actor)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, dto)
}

// updateDepartmentRequest 更新科室请求体（契约 §2.5）。
// 全字段可选，nil 表示不更新。ParentID 特殊：*0 = 变根科室；nil = 不动；*N = 移到 N 下。
type updateDepartmentRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	IsPublic    *bool   `json:"is_public,omitempty"`
	IsActive    *bool   `json:"is_active,omitempty"`
	ParentID    *int64  `json:"parent_id,omitempty"`
}

// UpdateDepartment PATCH /api/staff/base/departments/{id} — 更新科室（REQ-BASE-008/009/011）。
func (h *DepartmentHandler) UpdateDepartment(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	id, err := parseDeptID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	var req updateDepartmentRequest
	if err := decodeDeptJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	dto, err := h.svc.Update(r.Context(), id, service.UpdateDeptInput{
		Name:        req.Name,
		Description: req.Description,
		IsPublic:    req.IsPublic,
		IsActive:    req.IsActive,
		ParentID:    req.ParentID,
		Actor:       actor,
	})
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, dto)
}

// DeleteDepartment DELETE /api/staff/base/departments/{id} — 删除科室（REQ-BASE-010/011）。
func (h *DepartmentHandler) DeleteDepartment(w http.ResponseWriter, r *http.Request) {
	actor, err := currentActor(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	id, err := parseDeptID(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	if _, err := h.svc.Delete(r.Context(), id, actor); err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, map[string]bool{"success": true})
}

// ============ 共享辅助 ============

// currentActor 从 ctx 提取医护身份。
// DataIsolation 中间件挂载后由 service.ActorFromDataScope 统一读取，避免重复实现。
func currentActor(r *http.Request) (service.Actor, error) {
	// 复用 service.ActorFromDataScope（DataIsolation 已挂载）。
	actor, ok := service.ActorFromDataScope(r.Context())
	if !ok || actor.UserID <= 0 {
		return service.Actor{}, apperrors.Unauthorized("UNAUTHORIZED", "missing user identity")
	}
	if actor.Role == "" {
		return service.Actor{}, apperrors.Unauthorized("UNAUTHORIZED", "missing user role")
	}
	return actor, nil
}

// parseDeptID 解析 {id} 路径参数为 int64（契约 §2.3/2.5/2.6）。
func parseDeptID(r *http.Request) (int64, error) {
	raw := chi.URLParam(r, "id")
	if raw == "" {
		return 0, apperrors.Validation("BASE_DEPT_INVALID_ID", "id 参数缺失")
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 {
		return 0, apperrors.Validation("BASE_DEPT_INVALID_ID", "id 参数无效")
	}
	return n, nil
}

const maxDeptBodyBytes = 1 << 20

// decodeDeptJSON 解析请求体到 dst。空 body 或格式错误返回 422（契约 §0.3）。
// 严格模式：拒绝未知字段，避免客户端笔误被静默忽略。
// 限制请求体 1MB（与 wiki handler 一致）。
func decodeDeptJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return apperrors.Validation("BASE_DEPT_EMPTY_BODY", "请求体不能为空")
	}
	r.Body = http.MaxBytesReader(nil, r.Body, maxDeptBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return apperrors.Validation("BASE_DEPT_INVALID_JSON", "请求体格式错误")
	}
	return nil
}
