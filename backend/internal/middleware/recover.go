// Package middleware 提供 HTTP 中间件：请求 ID、日志、恢复、CORS、JWT 鉴权、角色控制、限流。
package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"

	"health-nexus/internal/shared/contextkeys"
)

// Recover 捕获下游 handler 的 panic，记录堆栈后返回 500 JSON。
// 直接写 JSON 而非调用 response.WriteError，避免对同一错误重复打日志。
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				rid := contextkeys.FromCtx(r.Context(), contextkeys.RequestID)
				slog.Error("panic recovered",
					"err", rec,
					"request_id", rid,
					"stack", string(debug.Stack()),
				)
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code":    "INTERNAL_ERROR",
					"message": "服务器内部错误",
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
