package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"health-nexus/internal/config"
	"health-nexus/internal/domain/chat/service"
	"health-nexus/internal/middleware"
	"health-nexus/internal/shared/contextkeys"
	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/response"
)

// chatRatePeriod SSE 流式对话限流窗口（所有用户一致）。
const chatRatePeriod = time.Minute

// 包级静态资源句柄：避免每次请求重建 FileServer。
// ponytail: 相对路径依赖工作目录（WORKDIR=/app，web 目录位于 /app/web），
// 与 Dockerfile 部署约定一致。升级路径：通过 config 注入绝对路径。
var (
	webFS       = http.Dir("web")
	staticFiles = http.FileServer(webFS)
)

// NewRouter 装配 chat 域全部 HTTP 路由。
// 应用 JWT 鉴权 + 任意角色：/api/chat/* 对所有已登录用户开放（限流值按 scope 区分），
// /api/staff/chat/* 需 STAFF 角色。
// /api/public/chat/stream 为匿名公开端点，通过 X-Device-Id 标识设备。
//
// 限流值从 config.yaml 读取默认值，运行时可通过 Redis SET rl_cfg:{scope} <limit> 热更新。
//
// 路由清单（契约 §3 + 匿名扩展）：
//   - POST   /api/chat/stream                              SSE 流式问答（需 JWT，任意角色）
//   - POST   /api/public/chat/stream                        SSE 流式问答（匿名，X-Device-Id）
//   - GET    /api/chat/conversations                       会话列表
//   - GET    /api/chat/conversations/{id}                  会话详情
//   - PATCH  /api/chat/conversations/{id}                  修改会话（标题/归档）
//   - DELETE /api/chat/conversations/{id}                  删除会话
//   - GET    /api/chat/conversations/{id}/messages         消息列表（游标分页）
//   - POST   /api/chat/messages/{id}/feedback              消息反馈（点赞/点踩）
//   - GET    /api/staff/chat/crisis-events                 危机事件列表
//   - POST   /api/staff/chat/crisis-events/{id}/handle     处理危机事件
func NewRouter(
	auth *middleware.Authenticator,
	rl *middleware.RateLimiter,
	cfg config.RateLimitConfig,
	stream *StreamHandler,
	conv *ConversationHandler,
	crisis *CrisisHandler,
) http.Handler {
	r := chi.NewRouter()

	// 匿名公开端点（无需 JWT，通过 X-Device-Id 标识，限流更严格）。
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireDeviceID)
		r.With(rl.HotReloadMiddleware("chat_stream_anon", cfg.ChatStreamAnon, chatRatePeriod)).
			Post("/api/public/chat/stream", stream.Stream)
	})

	// 聊天端：/api/chat/*（任意已认证角色——聊天对所有登录用户开放，仅限流值有差异）
	r.Group(func(r chi.Router) {
		r.Use(middleware.JWTAuth(auth), middleware.RequireAnyRole())
		r.Route("/api/chat", func(r chi.Router) {
			r.With(rl.HotReloadMiddleware("chat_stream", cfg.ChatStream, chatRatePeriod)).
				Post("/stream", stream.Stream)
			r.Route("/conversations", func(r chi.Router) {
				r.Get("/", conv.List)
				r.Get("/{id}", conv.Get)
				r.Patch("/{id}", conv.Patch)
				r.Delete("/{id}", conv.Delete)
				r.Get("/{id}/messages", conv.ListMessages)
			})
			r.Post("/messages/{id}/feedback", conv.Feedback)
		})
	})

	// 医护端：/api/staff/chat/*
	r.Group(func(r chi.Router) {
		r.Use(middleware.JWTAuth(auth), middleware.RequireStaff(), middleware.DataIsolation())
		r.Route("/api/staff/chat/crisis-events", func(r chi.Router) {
			r.Get("/", crisis.List)
			r.Post("/{id}/handle", crisis.Handle)
		})
	})

	// 统一 404/405 为 JSON——此 router 通过 Mount("/", ...) 挂载到根，
	// 未匹配路径会命中此处的 NotFound 而非外层 router 的，故需在此重复注册。
	// 非 /api/ 路径走 SPA fallback：先尝试静态文件，不存在则返回对应 SPA 入口 HTML。
	// ponytail: 与 main.go 的 NotFound handler 逻辑重复（chat router Mount 到根导致
	// 外层 NotFound 不会被命中）。升级路径：抽到 middleware 共享。
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			response.WriteJSON(w, http.StatusNotFound, map[string]any{
				"code":    "NOT_FOUND",
				"message": "请求的资源不存在",
			})
			return
		}
		serveSPA(w, r)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		response.WriteJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"code":    "METHOD_NOT_ALLOWED",
			"message": "请求方法不允许",
		})
	})

	return r
}

// currentPatientID 从 ctx 取 PATIENT 角色的 user_id。
func currentPatientID(r *http.Request) (int64, error) {
	return currentUserID(r)
}

// currentPatientIDOrZero 从 ctx 取 user_id，不存在时返回 0（用于区分匿名/已认证）。
func currentPatientIDOrZero(r *http.Request) (int64, error) {
	uid, ok := r.Context().Value(contextkeys.UserID).(int64)
	if !ok || uid <= 0 {
		return 0, nil
	}
	return uid, nil
}

// currentStaffID 从 ctx 取 STAFF 角色的 user_id。
func currentStaffID(r *http.Request) (int64, error) {
	return currentUserID(r)
}

// currentCrisisActor 从 ctx 提取危机事件操作者上下文（JWTAuth + DataIsolation 注入）。
func currentCrisisActor(r *http.Request) (service.CrisisActor, error) {
	ctx := r.Context()
	uid, ok := ctx.Value(contextkeys.UserID).(int64)
	if !ok || uid <= 0 {
		return service.CrisisActor{}, apperrors.Unauthorized("UNAUTHORIZED", "missing user identity")
	}
	role, _ := ctx.Value(contextkeys.UserRole).(string)
	if role == "" {
		return service.CrisisActor{}, apperrors.Unauthorized("UNAUTHORIZED", "missing user role")
	}
	deptID, _ := ctx.Value(contextkeys.DeptID).(int64)
	return service.CrisisActor{UserID: uid, Role: role, DeptID: deptID}, nil
}

// currentUserID 通用 user_id 解析。JWTAuth 中间件写入 user_id（int64）到 ctx。
// ponytail: 直接断言 int64 而非 FromCtx(string)——JWT 写入的是 int64（claims.UserID），简化。
func currentUserID(r *http.Request) (int64, error) {
	uid, ok := r.Context().Value(contextkeys.UserID).(int64)
	if !ok || uid <= 0 {
		return 0, apperrors.Unauthorized("UNAUTHORIZED", "missing user_id in context")
	}
	return uid, nil
}

// parseUUIDParam 从 chi URL 路径参数 "id" 解析 UUID。
func parseUUIDParam(r *http.Request) (uuid.UUID, error) {
	raw := chi.URLParam(r, "id")
	if raw == "" {
		return uuid.Nil, apperrors.BadRequest("CHAT_INVALID_ID", "id 参数缺失")
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apperrors.BadRequest("CHAT_INVALID_ID", "id 格式错误")
	}
	return id, nil
}

// parseInt64Param 从 chi URL 路径参数解析 int64。
func parseInt64Param(r *http.Request, key string) (int64, error) {
	raw := chi.URLParam(r, key)
	if raw == "" {
		return 0, apperrors.BadRequest("CHAT_INVALID_ID", key+" 参数缺失")
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, apperrors.BadRequest("CHAT_INVALID_ID", key+" 格式错误")
	}
	return n, nil
}

// serveSPA 处理非 API 路径的静态资源与 SPA fallback。
// 先尝试静态文件（含 /assets/、index.html 等），不存在则按路径前缀返回对应 SPA 入口：
//   - /staff/* 或 /styles → web/staff.html
//   - 其他路径（/chat、/wiki/article/:id、/about、/terms、/privacy 等）→ web/chat.html
//
// 这样客户端路由（vue-router）接管后续导航，无需后端为每个前端路由注册规则。
func serveSPA(w http.ResponseWriter, r *http.Request) {
	if f, err := webFS.Open(r.URL.Path); err == nil {
		f.Close()
		staticFiles.ServeHTTP(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/staff") || r.URL.Path == "/styles" {
		http.ServeFile(w, r, "web/staff.html")
		return
	}
	http.ServeFile(w, r, "web/chat.html")
}
