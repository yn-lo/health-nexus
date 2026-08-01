package handler

import (
	"github.com/go-chi/chi/v5"

	"health-nexus/internal/middleware"
)

// Mount 挂载 base 域路由（契约 §2.1-2.6）：
//   - GET    /api/base/departments                  可见科室列表（医护通用）
//   - GET    /api/staff/base/departments            科室树（管理员）
//   - GET    /api/staff/base/departments/{id}       科室详情
//   - POST   /api/staff/base/departments            创建科室
//   - PATCH  /api/staff/base/departments/{id}       更新科室
//   - DELETE /api/staff/base/departments/{id}       删除科室
//
// 中间件策略（AC-SEC-01）：
//   - /api/public/*          无需认证（公开端点）
//   - /api/base/*            JWTAuth + RequireAnyRole + DataIsolation
//   - /api/staff/base/*      JWTAuth + RequireAdmin  + DataIsolation（DEPT_ADMIN 由 service 层收口到子树）
//
// Mount 将 base 域路由挂载到主路由器。
func (h *DepartmentHandler) Mount(r chi.Router) {
	// 公开端点：无需认证，供匿名用户选择咨询科室（REQ-BASE-013）
	r.Get("/api/public/departments", h.ListPublicDepartments)

	// 已登录用户均可访问：可见科室列表
	r.Route("/api/base", func(r chi.Router) {
		r.Use(middleware.JWTAuth(h.auth), middleware.RequireAnyRole(), middleware.DataIsolation())
		r.Get("/departments", h.ListDepartments)
	})

	// 管理员 CRUD：科室树管理
	r.Route("/api/staff/base/departments", func(r chi.Router) {
		r.Use(middleware.JWTAuth(h.auth), middleware.RequireAdmin(), middleware.DataIsolation())
		r.Get("/", h.ListTree)
		r.Post("/", h.CreateDepartment)
		r.Get("/{id}", h.GetDepartment)
		r.Patch("/{id}", h.UpdateDepartment)
		r.Delete("/{id}", h.DeleteDepartment)
	})
}
