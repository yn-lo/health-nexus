// Package middleware 提供 HTTP 中间件：请求 ID、日志、恢复、CORS、JWT 鉴权、角色控制、限流。
// 中间件按职责拆分到独立文件，由 cmd/ 中的路由装配顺序组合。
package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"health-nexus/internal/shared/contextkeys"
)

// RequestIDHeader 请求/响应头中的请求 ID 字段名。
const RequestIDHeader = "X-Request-ID"

// RequestID 从 X-Request-ID 读取请求 ID，缺失则生成 UUID；写入 context 和响应头。
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.Header.Get(RequestIDHeader)
		if rid == "" {
			rid = uuid.NewString()
		}
		w.Header().Set(RequestIDHeader, rid)
		ctx := context.WithValue(r.Context(), contextkeys.RequestID, rid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
