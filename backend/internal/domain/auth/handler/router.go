package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"health-nexus/internal/config"
	"health-nexus/internal/middleware"
)

// 限流 scope 名称（REQ-NFR-003）。
// scope 拆分：login 共享，register/refresh 独立——避免登录爆破连坐 register/refresh。
const (
	authRateScope     = "auth"          // login 统一登录
	registerRateScope = "auth_register" // register 独立
	refreshRateScope  = "auth_refresh"  // refresh 独立
	authRatePeriod    = time.Minute
)

// authRequestTimeout auth 端点 per-request ctx deadline（R8-Auth-2 修复）。
// R7-4 为支持 SSE 流式响应将 server.WriteTimeout 置 0，auth 端点失去 server 级写超时保护。
// 此处补 ctx deadline：DB/Redis 阻塞超此时长后 ctx 取消，pgx/go-redis 返回 context.DeadlineExceeded，
// service 层包装为错误，handler 写 500。10s 覆盖正常 DB+Redis 操作的最坏时长（含 argon2id 哈希计算）。
// ponytail: 仅设 ctx deadline 不主动写超时响应——下游已用 ctx，超时传播自然走错误链，无需新中间件，简化。
const authRequestTimeout = 10 * time.Second

// requestTimeout 给请求注入 ctx deadline。
func requestTimeout() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), authRequestTimeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Mount 将 auth 域 14 个端点挂载到 r。
// 限流值从 config 读取（默认 10/min），运行时可通过 Redis rl_cfg:{scope} 热更新。
func (h *AuthHandler) Mount(r chi.Router, auth *middleware.Authenticator, rl *middleware.RateLimiter, cfg config.RateLimitConfig) {
	r.Route("/api/auth", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(requestTimeout())
			r.Use(rl.HotReloadMiddleware(authRateScope, cfg.AuthLogin, authRatePeriod))
			r.Post("/login", h.UnifiedLogin)
		})
		r.With(requestTimeout(), rl.HotReloadMiddleware(registerRateScope, cfg.AuthRegister, authRatePeriod)).
			Post("/register", h.Register)
		r.With(requestTimeout(), rl.HotReloadMiddleware(refreshRateScope, cfg.AuthRefresh, authRatePeriod)).
			Post("/refresh", h.Refresh)
		r.With(requestTimeout(), middleware.JWTAuth(auth)).
			Post("/logout", h.Logout)
		r.With(requestTimeout(), rl.HotReloadMiddleware(registerRateScope, cfg.AuthRegister, authRatePeriod)).
			Post("/password-reset/request", h.PasswordResetRequest)
		r.With(requestTimeout(), rl.HotReloadMiddleware(refreshRateScope, cfg.AuthRefresh, authRatePeriod)).
			Post("/password-reset/confirm", h.PasswordResetConfirm)
		// 已登录自助端点：修改密码 + 个人资料读写（JWT 认证，不限流）。
		r.Group(func(r chi.Router) {
			r.Use(requestTimeout(), middleware.JWTAuth(auth))
			r.Post("/change-password", h.ChangePassword)
			r.Get("/profile", h.GetProfile)
			r.Patch("/profile", h.UpdateProfile)
		})
	})

	// 管理员账户管理端点：JWT + RequireAdmin（执行顺序：JWTAuth → RequireAdmin）。
	r.Route("/api/staff/auth", func(r chi.Router) {
		r.Use(requestTimeout(), middleware.JWTAuth(auth), middleware.RequireAdmin())
		r.Get("/accounts", h.ListAccounts)
		r.Post("/accounts", h.CreateAccount)
		r.Post("/accounts/{id}/lock", h.LockAccount)
		r.Post("/accounts/{id}/unlock", h.UnlockAccount)
		r.Delete("/accounts/{id}", h.SoftDeleteAccount)
		r.Post("/accounts/{id}/reset-password", h.ResetAccountPassword)
	})
}
