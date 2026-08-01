// Package middleware 提供 HTTP 中间件：请求 ID、日志、恢复、CORS、JWT 鉴权、角色控制、限流、数据隔离。
package middleware

import (
	"context"
	"net/http"

	"health-nexus/internal/shared/contextkeys"
)

// DataScope 数据隔离上下文（REQ-SEC-003）。
// 由 DataIsolation 中间件从 JWTAuth 写入的 ctx 字段构造，注入 ctx 供 service 层统一读取。
// 字段类型与 JWTAuth 写入类型一致：UserID/DeptID 为 int64，Role 为 string。
// TokenType 预留：JWTAuth 当前未写入 token_type 到 ctx，故中间件不填充（空字符串）。
type DataScope struct {
	UserID    int64
	Role      string
	DeptID    int64
	TokenType string
}

// DataIsolation 构造数据隔离中间件（REQ-SEC-003）。
// 必须位于 JWTAuth 之后、RequireRole 之后：从 ctx 读取 JWTAuth 写入的身份字段，
// 构造 DataScope 并通过 contextkeys.DataScopeKey 注入 ctx，供 service 层统一拦截数据范围。
// ponytail: 中间件仅做"采集 + 注入"，不做角色判定（角色判定由 RequireRole 完成），简化，
// 也不替代 service 层 assertCanManage 鉴权——这是补充层，不是重复校验。
// 升级路径：若需引入行级安全（RLS）或数据范围策略，可在本中间件内追加 scope 缩放逻辑。
func DataIsolation() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			scope := &DataScope{}
			// JWTAuth 写入 UserID/UserRole/DeptID 为 int64/string/int64（非字符串，不用 FromCtx）。
			if uid, ok := ctx.Value(contextkeys.UserID).(int64); ok {
				scope.UserID = uid
			}
			if role, ok := ctx.Value(contextkeys.UserRole).(string); ok {
				scope.Role = role
			}
			if did, ok := ctx.Value(contextkeys.DeptID).(int64); ok {
				scope.DeptID = did
			}
			ctx = context.WithValue(ctx, contextkeys.DataScopeKey, scope)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ScopeFromCtx 从 ctx 读取 DataScope。未挂载 DataIsolation 时返回 nil。
func ScopeFromCtx(ctx context.Context) *DataScope {
	if v, ok := ctx.Value(contextkeys.DataScopeKey).(*DataScope); ok {
		return v
	}
	return nil
}
