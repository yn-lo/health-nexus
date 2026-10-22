package middleware

import (
	"net/http"
	"strings"
)

// 前端静态资源缓存策略。
//
// 静态文件由 http.FileServer 托管，默认只下发 Last-Modified、没有 Cache-Control：
// 浏览器每次访问都要为每个资源发一次条件请求（患者端首屏 33 个资源 → 33 次 304 往返）。
// 按路径前缀统一下发策略：
//   - /assets/*：Vite 产物文件名含内容哈希，可长期 immutable 缓存；
//   - /api/*、/uploads/*：由各自 handler 管理缓存头（upload 已显式下发 immutable）；
//   - 其余（SPA 入口 HTML 及其 fallback）：no-cache，保证发版后入口立即刷新。

// StaticCache 按路径前缀下发静态资源 Cache-Control。
func StaticCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasPrefix(path, "/assets/"):
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		case strings.HasPrefix(path, "/api/"), strings.HasPrefix(path, "/uploads/"):
			// 交给具体 handler，不覆盖。
		default:
			w.Header().Set("Cache-Control", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}
