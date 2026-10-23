package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"health-nexus/internal/config"
	"health-nexus/internal/domain/chat/service"
	"health-nexus/internal/middleware"
	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/identity"
)

// chatRatePeriod SSE 流式对话限流窗口（所有用户一致）。
const chatRatePeriod = time.Minute

// Router 装配 chat 域全部 HTTP 路由。
// 应用 JWT 鉴权 + 任意角色：/api/chat/* 对所有已登录用户开放（限流值按 scope 区分），
// /api/staff/chat/* 需 STAFF 角色。
// /api/public/chat/stream 为匿名公开端点，通过 X-Device-Id 标识设备。
//
// 限流值从 config.yaml 读取默认值，运行时可通过 Redis SET rl_cfg:{scope} <limit> 热更新。
//
// 路由清单（契约 §3 + 匿名扩展）：
//   - POST   /api/chat/stream                              SSE 流式问答（需 JWT，任意角色）
//   - POST   /api/public/chat/stream                        SSE 流式问答（匿名，X-Device-Id）
//   - DELETE /api/public/chat/conversations/{id}            匿名会话删除（清服务端瞬态上下文）
//   - GET    /api/chat/conversations                       会话列表
//   - GET    /api/chat/conversations/{id}                  会话详情
//   - PATCH  /api/chat/conversations/{id}                  修改会话（标题/归档）
//   - DELETE /api/chat/conversations/{id}                  删除会话
//   - GET    /api/chat/conversations/{id}/messages         消息列表（游标分页）
//   - POST   /api/chat/messages/{id}/feedback              消息反馈（solved/partial/unsolved）
//   - GET    /api/staff/chat/feedback/stats                反馈统计（三态汇总 + 最近反馈）
//   - GET    /api/staff/chat/crisis-events                 危机事件列表
//   - POST   /api/staff/chat/crisis-events/{id}/handle     处理危机事件
type Router struct {
	auth   *middleware.Authenticator
	rl     *middleware.RateLimiter
	cfg    config.RateLimitConfig
	stream *StreamHandler
	conv   *ConversationHandler
	crisis *CrisisHandler
}

// NewRouter 构造 chat 域路由。
func NewRouter(
	auth *middleware.Authenticator,
	rl *middleware.RateLimiter,
	cfg config.RateLimitConfig,
	stream *StreamHandler,
	conv *ConversationHandler,
	crisis *CrisisHandler,
) *Router {
	return &Router{auth: auth, rl: rl, cfg: cfg, stream: stream, conv: conv, crisis: crisis}
}

// Mount 将 chat 域路由注册到父 router（不接管 404/405，交由应用统一处理）。
func (rt *Router) Mount(r chi.Router) {
	auth, rl, cfg := rt.auth, rt.rl, rt.cfg
	stream, conv, crisis := rt.stream, rt.conv, rt.crisis

	// 匿名公开端点（无需 JWT，通过 X-Device-Id 标识，限流更严格）。
	// 双层限流：global 总量桶（防批量伪造 device_id）+ 单设备桶。
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireDeviceID)
		r.Use(rl.HotReloadMiddleware("global:chat_stream_anon", cfg.ChatStreamAnonGlobal, chatRatePeriod))
		r.With(rl.HotReloadMiddleware("chat_stream_anon", cfg.ChatStreamAnon, chatRatePeriod)).
			Post("/api/public/chat/stream", stream.Stream)
	})

	// 匿名会话删除（无需 JWT，通过 X-Device-Id 标识设备，仅可清除本设备的瞬态上下文）。
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireDeviceID)
		r.Delete("/api/public/chat/conversations/{id}", stream.DeleteAnonConversation)
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
		r.Use(middleware.JWTAuth(auth), middleware.RequireStaff())
		r.Get("/api/staff/chat/feedback/stats", conv.FeedbackStats)
		r.Route("/api/staff/chat/crisis-events", func(r chi.Router) {
			r.Get("/", crisis.List)
			r.Post("/{id}/handle", crisis.Handle)
		})
	})
}

// currentPatientID 从 ctx 取 PATIENT 角色的 user_id。
func currentPatientID(r *http.Request) (int64, error) {
	return currentUserID(r)
}

// currentStaffActor 从 ctx 提取医护端操作者上下文（JWTAuth 注入）：
// user_id/role/dept_id 用于危机事件处理与反馈统计的科室数据隔离。
func currentStaffActor(r *http.Request) (service.CrisisActor, error) {
	return identity.ActorFromRequest(r)
}

// currentUserID 通用 user_id 解析。JWTAuth 中间件写入 user_id（int64）到 ctx。
func currentUserID(r *http.Request) (int64, error) {
	actor, err := identity.ActorFromRequest(r)
	if err != nil {
		return 0, err
	}
	return actor.UserID, nil
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
