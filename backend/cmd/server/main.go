// Package main 是 HTTP Server 入口。
// 加载配置、初始化应用、启动 HTTP 服务器，处理 SIGTERM 优雅关闭。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"health-nexus/internal/config"
	"health-nexus/internal/di"
	"health-nexus/internal/middleware"
	"health-nexus/internal/shared/response"
)

const readHeaderTimeout = 5 * time.Second

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config failed", "err", err)
		panic(err)
	}
	config.WarnIfDevSecrets(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 启动时自动执行数据库迁移（幂等，已执行的迁移不会重复）
	if err := di.RunMigrations(ctx, cfg.Postgres.DSN, ""); err != nil {
		slog.Error("run migrations failed", "err", err)
		panic(err)
	}

	app, err := di.NewApp(ctx, cfg)
	if err != nil {
		slog.Error("init app failed", "err", err)
		panic(err)
	}
	defer app.Infra.Close()

	r := buildRouter(app)

	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.Server.Port),
		Handler:           r,
		ReadTimeout:       cfg.Server.ReadTimeout,
		ReadHeaderTimeout: readHeaderTimeout, // slowloris 防护（显式短超时）
		// WriteTimeout=0 禁用 server 级写超时——SSE 流式响应可能持续数分钟，
		// server 级 WriteTimeout 会在 deadline 到期时截断每个 chunk flush（R7-4 修复）。
		// 超时控制由各 handler 的 ctx deadline 负责（chat 流式有 chatPendingLockTTL 5min + 应用层 deadline）。
		WriteTimeout: 0,
	}

	go func() {
		slog.Info("http server starting", "port", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "err", err)
			panic(err)
		}
	}()

	<-ctx.Done()
	slog.Info("shutdown signal received, gracefully stopping...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
	}
	slog.Info("server stopped")
}

// buildRouter 构建路由树，装配所有域的路由。
func buildRouter(app *di.App) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recover)
	r.Use(middleware.RequestLog)
	r.Use(middleware.CORS(app.Infra.Cfg.CORS))

	// 健康检查
	r.Get("/healthz", healthz(app.Infra))

	// auth 域（14 端点：7 公开/刷新/密码重置 + 3 已登录自助 + 4 管理员账户管理）
	app.Auth.Mount(r, app.Infra.Auth, app.Infra.RateLimiter, app.Infra.Cfg.RateLimit)

	// base 域（1 端点）
	app.Base.Mount(r)

	// base 域：站内通知（4 端点，/api/staff/notifications）
	app.Notifications.Mount(r)

	// wiki 域（14 端点：2 公开 + 7 文章管理 + 5 引用授权）
	app.Wiki.Mount(r)

	// chat 域（8 端点：SSE + 会话管理 + 危机事件）
	r.Mount("/", app.Chat)

	// config 域（20 端点，需要 JWT + RequireAdmin）
	r.Group(func(r chi.Router) {
		r.Use(middleware.JWTAuth(app.Infra.Auth), middleware.RequireAdmin())
		app.Config.RegisterRoutes(r)
	})

	// 前端静态文件托管 + SPA fallback（同源部署）
	// /api/* 的 404 返回 JSON；其他路径先尝试静态文件，不存在则返回对应 SPA 入口 HTML，
	// 由客户端 vue-router 接管路由（/staff/* → staff.html，其他 → chat.html）。
	// 注意：chat 域 router 通过 Mount("/", ...) 挂载到根，未匹配路径会先命中
	// chat router 的 NotFound（同样走 SPA fallback 逻辑），此处为兜底。
	webDir := http.Dir("web")
	fs := http.FileServer(webDir)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			response.WriteJSON(w, http.StatusNotFound, map[string]any{
				"code":    "NOT_FOUND",
				"message": "请求的资源不存在",
			})
			return
		}
		if f, err := webDir.Open(r.URL.Path); err == nil {
			f.Close()
			fs.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/staff") || r.URL.Path == "/styles" {
			http.ServeFile(w, r, "web/staff.html")
			return
		}
		http.ServeFile(w, r, "web/chat.html")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		response.WriteJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"code":    "METHOD_NOT_ALLOWED",
			"message": "请求方法不允许",
		})
	})

	return r
}

// healthzTimeout 健康检查依赖探测超时（DB+Redis Ping）。
const healthzTimeout = 3 * time.Second

// healthz 健康检查端点（契约 §7.1）。检查 DB + Redis，返回 {status, database, redis, timestamp}。
// 任一依赖不可用返回 503；全部 ok 返回 200。
func healthz(infra *di.Infrastructure) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), healthzTimeout)
		defer cancel()

		dbStat := "ok"
		if err := infra.Pool.Ping(ctx); err != nil {
			dbStat = "fail"
		}
		redisStat := "ok"
		if err := infra.Redis.Ping(ctx).Err(); err != nil {
			redisStat = "fail"
		}

		status := "ok"
		httpCode := http.StatusOK
		if dbStat == "fail" || redisStat == "fail" {
			status = "degraded"
			httpCode = http.StatusServiceUnavailable
		}
		response.WriteJSON(w, httpCode, map[string]any{
			"status":    status,
			"database":  dbStat,
			"redis":     redisStat,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	}
}
