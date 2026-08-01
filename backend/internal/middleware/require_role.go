// Package middleware 提供 HTTP 中间件：请求 ID、日志、恢复、CORS、JWT 鉴权、角色控制、限流。
package middleware

import (
	"net/http"

	"health-nexus/internal/shared/constants"
	"health-nexus/internal/shared/contextkeys"
	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/response"
)

// RequireRole 要求请求者角色在 roles 列表中，否则返回 403。
// 缺失角色（未鉴权）返回 401，由调用方保证 JWTAuth 在前。
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		allowed[r] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := contextkeys.FromCtx(r.Context(), contextkeys.UserRole)
			if role == "" {
				response.WriteError(w, r, apperrors.Unauthorized("UNAUTHORIZED", "未认证"))
				return
			}
			if _, ok := allowed[role]; !ok {
				response.WriteError(w, r, apperrors.Forbidden("FORBIDDEN", "角色无权访问"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireStaff 要求医护角色（含超管/科室管理员/医生/护士）。
func RequireStaff() func(http.Handler) http.Handler {
	return RequireRole(constants.RoleSuperAdmin, constants.RoleDeptAdmin, constants.RoleDoctor, constants.RoleNurse)
}

// RequirePatient 要求患者角色。
func RequirePatient() func(http.Handler) http.Handler {
	return RequireRole(constants.RolePatient)
}

// RequireAdmin 要求管理员角色（超管/科室管理员）。
func RequireAdmin() func(http.Handler) http.Handler {
	return RequireRole(constants.RoleSuperAdmin, constants.RoleDeptAdmin)
}

// RequireAnyRole 要求已认证（角色非空），不限具体角色。
// 适用于所有已登录用户均可访问的端点（如 AI 聊天）。
func RequireAnyRole() func(http.Handler) http.Handler {
	return RequireRole(
		constants.RolePatient,
		constants.RoleSuperAdmin,
		constants.RoleDeptAdmin,
		constants.RoleDoctor,
		constants.RoleNurse,
	)
}
