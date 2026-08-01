// Package middleware 提供 HTTP 中间件：请求 ID、日志、恢复、CORS、JWT 鉴权、角色控制、限流。
package middleware

import (
	"log/slog"
	"net/http"
	"strconv"

	"health-nexus/internal/config"
)

// CORS 相关头名称。
const (
	headerOrigin           = "Origin"
	headerAllowOrigin      = "Access-Control-Allow-Origin"
	headerAllowMethods     = "Access-Control-Allow-Methods"
	headerAllowHeaders     = "Access-Control-Allow-Headers"
	headerAllowCredentials = "Access-Control-Allow-Credentials"
	headerMaxAge           = "Access-Control-Max-Age"
	headerVary             = "Vary"
)

// 预检缓存时长（秒）。ponytail: 固定值够用，折中；若多客户端场景需要调整可改为配置项。
const corsMaxAgeSeconds = 600

// corsAllowMethods 允许的 HTTP 方法。
const corsAllowMethods = "GET,POST,PUT,PATCH,DELETE,OPTIONS"

// corsAllowHeaders 允许的请求头。
const corsAllowHeaders = "Authorization,Content-Type,X-Request-ID"

// CORS 按白名单校验 Origin 并处理 OPTIONS 预检。
// 凭证模式下不能用 "*"，故命中白名单时回显具体 Origin。
//
// 安全约束（fail-fast）：AllowedOrigins 含 "*" 且 AllowCredentials=true 时 panic，
// 避免不安全配置上线（任意站点可携带 cookie 跨站请求）。
func CORS(cfg config.CORSConfig) func(http.Handler) http.Handler {
	allowAll := false
	for _, o := range cfg.AllowedOrigins {
		if o == "*" {
			allowAll = true
			break
		}
	}
	if allowAll && cfg.AllowCredentials {
		slog.Error("CORS config unsafe: AllowedOrigins contains '*' with AllowCredentials=true; refusing to start",
			"allowed_origins", cfg.AllowedOrigins,
			"allow_credentials", cfg.AllowCredentials,
		)
		panic("CORS: '*' origin cannot be combined with AllowCredentials=true " +
			"(CORS spec violation + credential exfiltration risk)")
	}

	allowed := make(map[string]struct{}, len(cfg.AllowedOrigins))
	for _, o := range cfg.AllowedOrigins {
		if o == "*" {
			continue
		}
		allowed[o] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get(headerOrigin)
			if origin != "" {
				if _, ok := allowed[origin]; ok || allowAll {
					w.Header().Set(headerAllowOrigin, origin)
					if cfg.AllowCredentials {
						w.Header().Set(headerAllowCredentials, "true")
					}
					w.Header().Add(headerVary, headerOrigin)
				}
			}
			w.Header().Set(headerAllowMethods, corsAllowMethods)
			w.Header().Set(headerAllowHeaders, corsAllowHeaders)
			if r.Method == http.MethodOptions {
				w.Header().Set(headerMaxAge, strconv.Itoa(corsMaxAgeSeconds))
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
