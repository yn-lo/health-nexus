// Package middleware 提供 HTTP 中间件：请求 ID、日志、恢复、CORS、JWT 鉴权、角色控制、限流。
package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"health-nexus/internal/shared/contextkeys"
)

// statusRecorder 包装 ResponseWriter 以捕获状态码。
// 实现 http.Flusher 透传，避免 SSE 等流式场景中间件层丢失 Flusher 接口。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader 记录状态码后透传。
func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush 实现 http.Flusher，透传给底层 ResponseWriter（SSE 流式响应需要）。
// ponytail: 仅当底层 writer 实现 Flusher 时才透传；标准 net/http server 默认支持，简化。
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// RequestLog 记录每个请求的方法、路径、状态码、耗时和 request_id。
func RequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", contextkeys.FromCtx(r.Context(), contextkeys.RequestID),
		)
	})
}
