// Package tests 提供 API 契约测试（契约 §0）。
// 验证 API 端点的路由完整性、鉴权门禁和响应格式。
// 不依赖外部服务（DB/Redis/LLM），仅测试路由匹配 + 中间件链。
package tests_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"

	"health-nexus/internal/config"
	authhandler "health-nexus/internal/domain/auth/handler"
	basehandler "health-nexus/internal/domain/base/handler"
	chathandler "health-nexus/internal/domain/chat/handler"
	confighandler "health-nexus/internal/domain/config/handler"
	"health-nexus/internal/domain/wiki/entity"
	wikihandler "health-nexus/internal/domain/wiki/handler"
	"health-nexus/internal/domain/wiki/repository"
	wikiservice "health-nexus/internal/domain/wiki/service"
	"health-nexus/internal/middleware"
	"health-nexus/internal/shared/constants"
	"health-nexus/internal/shared/response"
)

// 测试全局变量：JWT secret、Authenticator、路由树。
var (
	testJWTSecret = "contract-test-secret-key"
	testAuth      *middleware.Authenticator
	testRouter    http.Handler
)

// ==================== 测试辅助 ====================

// TestMain 构造 Authenticator，构建完整路由树。
func TestMain(m *testing.M) {
	var err error
	testAuth, err = middleware.NewAuthenticator(config.JWTConfig{
		Secret:     testJWTSecret,
		Issuer:     "health-nexus-test",
		AccessTTL:  30 * time.Minute,
		RefreshTTL: 7 * 24 * time.Hour,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "create authenticator: %v\n", err)
		os.Exit(1)
	}

	testRouter = buildTestRouter()
	os.Exit(m.Run())
}

// buildTestRouter 构建与 main.buildRouter 同构的完整路由树。
// 各域 handler 的 service 传 nil；中间件先于 handler 拦截，Recover 兜底 panic。
// 例外：PublicHandler 注入 stub ArticleService——公开端点（GET /api/wiki/articles[*]）无 JWT 中间件，
// 路由完整性测试会真实进入 handler，nil ArticleService 会 panic 被 Recover 兜底成 500（污染日志）。
func buildTestRouter() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recover)
	r.Use(middleware.RequestLog)
	r.Use(middleware.CORS(config.CORSConfig{AllowedOrigins: []string{"*"}}))

	// 健康检查（简化版，不依赖 DB/Redis）
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		response.WriteJSON(w, http.StatusOK, map[string]any{
			"status":    "ok",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	})

	// auth 域（13 端点：6 公开/刷新/密码重置 + 3 已登录自助 + 4 管理员账户管理）
	authhandler.NewAuthHandler(nil).Mount(r, testAuth, fakeRateLimiter(), config.RateLimitConfig{})

	// base 域（1 端点）
	basehandler.NewDepartmentHandler(nil, testAuth).Mount(r)

	// wiki 域（18 端点：2 公开 + 11 文章管理 + 5 引用授权）
	wikihandler.NewRouter(
		wikihandler.NewPublicHandler(wikiservice.NewArticleService(stubArticleRepo{}, nil, nil, nil, nil, nil, nil)),
		wikihandler.NewStaffArticleHandler(nil),
		wikihandler.NewReferenceHandler(nil, nil),
		testAuth,
	).Mount(r)

	// chat 域（8 端点：SSE + 会话管理 + 危机事件）
	r.Mount("/", chathandler.NewRouter(
		testAuth,
		fakeRateLimiter(),
		config.RateLimitConfig{},
		chathandler.NewStreamHandler(nil),
		chathandler.NewConversationHandler(nil),
		chathandler.NewCrisisHandler(nil),
	))

	// config 域（22 端点，需 JWT + Admin）
	r.Group(func(r chi.Router) {
		r.Use(middleware.JWTAuth(testAuth), middleware.RequireAdmin())
		confighandler.NewConfigHandler(nil).RegisterRoutes(r)
	})

	// 统一 404/405 JSON 响应
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		response.WriteJSON(w, http.StatusNotFound, map[string]any{
			"code":    "NOT_FOUND",
			"message": "请求的资源不存在",
		})
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		response.WriteJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"code":    "METHOD_NOT_ALLOWED",
			"message": "请求方法不允许",
		})
	})

	return r
}

// signTestToken 签发 HS256 access JWT。
func signTestToken(userID int64, role string, deptID int64) string {
	claims := &middleware.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "health-nexus-test",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		UserID:    userID,
		Role:      role,
		DeptID:    deptID,
		TokenType: "access",
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testJWTSecret))
	if err != nil {
		panic("sign test token: " + err.Error())
	}
	return token
}

// fakeRateLimiter 创建使用不可达 Redis 地址的限流器（快速失败返回 503）。
// trustedProxies=nil：契约测试不验证 IP 限流逻辑，仅触发 503 快速失败路径。
func fakeRateLimiter() *middleware.RateLimiter {
	return middleware.NewRateLimiter(redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:1",
	}), nil)
}

// stubArticleRepo 公开端点路由测试用的 ArticleRepoPort 桩。
// ListPublished 返回空列表，GetPublishedByID 返回空 Article——让 PublicHandler 走完正常路径返回 200，不 panic 也不 404。
// 路由完整性测试断言"非 404"：返回 ErrNotFound 会让 handler 写 404 被误判为"路由未注册"。
// 写方法不会被调用（公开端点只读），返回零值即可。
type stubArticleRepo struct{}

func (stubArticleRepo) Create(context.Context, *entity.Article) error { return nil }
func (stubArticleRepo) GetByID(context.Context, int64) (*entity.Article, error) {
	return nil, repository.ErrNotFound
}
func (stubArticleRepo) GetPublishedByID(context.Context, int64) (*entity.Article, error) {
	return &entity.Article{}, nil
}
func (stubArticleRepo) ListPublished(context.Context, repository.ListPublishedFilter, int, int) ([]*entity.Article, int64, error) {
	return nil, 0, nil
}
func (stubArticleRepo) ListFeatured(context.Context, *int64, int) ([]*entity.Article, error) {
	return nil, nil
}
func (stubArticleRepo) SetFeaturedRank(context.Context, int64, int) error { return nil }
func (stubArticleRepo) ListForStaff(context.Context, repository.ListStaffFilter, int, int) ([]*entity.Article, int64, error) {
	return nil, 0, nil
}
func (stubArticleRepo) UpdateFields(context.Context, int64, repository.UpdateFields) (*entity.Article, error) {
	return nil, repository.ErrNotFound
}
func (stubArticleRepo) UpdateStatus(context.Context, int64, string, string, repository.StatusUpdateOpts) error {
	return repository.ErrNotFound
}
func (stubArticleRepo) SoftDelete(context.Context, int64) error { return nil }

// doRequest 发送 HTTP 请求到测试路由。
func doRequest(method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, http.NoBody)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	testRouter.ServeHTTP(rec, req)
	return rec
}

// ==================== 端点表 ====================

type endpoint struct {
	method string
	path   string
}

const testUUID = "550e8400-e29b-41d4-a716-446655440000"

// routeParamPattern 匹配路由模板中的 {id}/{article_id} 等路径参数，探测时替换为合法值。
var routeParamPattern = regexp.MustCompile(`\{[^}]+\}`)

// allRoutes 遍历真实路由树（单一真源），返回全部已注册端点。
// 替代手写端点清单——路由增删由代码自证，契约测试永不漂移。
func allRoutes(t *testing.T) []endpoint {
	t.Helper()
	router, ok := testRouter.(chi.Router)
	if !ok {
		t.Fatalf("testRouter is %T, expected chi.Router for Walk", testRouter)
	}
	seen := make(map[string]struct{})
	var out []endpoint
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		key := method + " " + route
		if _, dup := seen[key]; dup {
			return nil
		}
		seen[key] = struct{}{}
		out = append(out, endpoint{method: method, path: routeParamPattern.ReplaceAllString(route, testUUID)})
		return nil
	})
	if err != nil {
		t.Fatalf("walk router: %v", err)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].path != out[j].path {
			return out[i].path < out[j].path
		}
		return out[i].method < out[j].method
	})
	return out
}

// publicEndpoints 公开端点（无需认证）。
var publicEndpoints = []endpoint{
	{http.MethodGet, "/healthz"},
	{http.MethodPost, "/api/auth/login"},
	{http.MethodPost, "/api/auth/register"},
	{http.MethodPost, "/api/auth/refresh"},
	{http.MethodPost, "/api/auth/password-reset/request"},
	{http.MethodPost, "/api/auth/password-reset/confirm"},
	{http.MethodGet, "/api/wiki/articles"},
	{http.MethodGet, "/api/wiki/articles/featured"},
	{http.MethodGet, "/api/wiki/articles/1"},
	{http.MethodPost, "/api/public/chat/stream"},
	{http.MethodGet, "/api/public/departments"},
}

// protectedEndpoints 需 JWT 的端点（66 个）。
var protectedEndpoints = []endpoint{
	// auth 已登录
	{http.MethodPost, "/api/auth/logout"},
	{http.MethodPost, "/api/auth/change-password"},
	{http.MethodGet, "/api/auth/profile"},
	{http.MethodPatch, "/api/auth/profile"},
	// auth 管理员
	{http.MethodGet, "/api/staff/auth/accounts"},
	{http.MethodPost, "/api/staff/auth/accounts"},
	{http.MethodPost, "/api/staff/auth/accounts/1/lock"},
	{http.MethodPost, "/api/staff/auth/accounts/1/unlock"},
	{http.MethodDelete, "/api/staff/auth/accounts/1"},
	{http.MethodPost, "/api/staff/auth/accounts/1/restore"},
	{http.MethodPost, "/api/staff/auth/accounts/1/reset-password"},
	{http.MethodPatch, "/api/staff/auth/accounts/1/department"},
	{http.MethodPatch, "/api/staff/auth/accounts/1/role"},
	// base
	{http.MethodGet, "/api/base/departments"},
	// base 管理员（5）
	{http.MethodGet, "/api/staff/base/departments"},
	{http.MethodPost, "/api/staff/base/departments"},
	{http.MethodGet, "/api/staff/base/departments/1"},
	{http.MethodPatch, "/api/staff/base/departments/1"},
	{http.MethodDelete, "/api/staff/base/departments/1"},
	// wiki staff 文章
	{http.MethodPost, "/api/staff/wiki/articles"},
	{http.MethodGet, "/api/staff/wiki/articles"},
	{http.MethodGet, "/api/staff/wiki/articles/1"},
	{http.MethodPut, "/api/staff/wiki/articles/1"},
	{http.MethodDelete, "/api/staff/wiki/articles/1"},
	{http.MethodPost, "/api/staff/wiki/articles/1/submit"},
	{http.MethodPost, "/api/staff/wiki/articles/1/approve"},
	{http.MethodPost, "/api/staff/wiki/articles/1/reject"},
	{http.MethodPost, "/api/staff/wiki/articles/1/archive"},
	{http.MethodPost, "/api/staff/wiki/articles/1/unarchive"},
	{http.MethodPost, "/api/staff/wiki/articles/1/featured"},
	{http.MethodGet, "/api/staff/wiki/articles/1/chunks"},
	{http.MethodPost, "/api/staff/wiki/articles/1/revectorize"},
	// wiki staff 引用
	{http.MethodPost, "/api/staff/wiki/references"},
	{http.MethodGet, "/api/staff/wiki/references"},
	{http.MethodPost, "/api/staff/wiki/references/1/approve"},
	{http.MethodPost, "/api/staff/wiki/references/1/reject"},
	{http.MethodDelete, "/api/staff/wiki/references/1"},
	{http.MethodGet, "/api/staff/wiki/references/articles"},
	// chat 患者端
	{http.MethodPost, "/api/chat/stream"},
	{http.MethodGet, "/api/chat/conversations"},
	{http.MethodGet, "/api/chat/conversations/" + testUUID},
	{http.MethodPatch, "/api/chat/conversations/" + testUUID},
	{http.MethodDelete, "/api/chat/conversations/" + testUUID},
	{http.MethodGet, "/api/chat/conversations/" + testUUID + "/messages"},
	{http.MethodPost, "/api/chat/messages/1/feedback"},
	// chat 医护端
	{http.MethodGet, "/api/staff/chat/crisis-events"},
	{http.MethodPost, "/api/staff/chat/crisis-events/1/handle"},
	// config（27）
	{http.MethodGet, "/api/staff/config/ai-providers"},
	{http.MethodPost, "/api/staff/config/ai-providers"},
	{http.MethodGet, "/api/staff/config/ai-providers/1"},
	{http.MethodPut, "/api/staff/config/ai-providers/1"},
	{http.MethodDelete, "/api/staff/config/ai-providers/1"},
	{http.MethodPost, "/api/staff/config/ai-providers/1/test"},
	{http.MethodGet, "/api/staff/config/sensitive-words"},
	{http.MethodPost, "/api/staff/config/sensitive-words"},
	{http.MethodPut, "/api/staff/config/sensitive-words/1"},
	{http.MethodDelete, "/api/staff/config/sensitive-words/1"},
	{http.MethodGet, "/api/staff/config/safety-rules"},
	{http.MethodPost, "/api/staff/config/safety-rules"},
	{http.MethodPut, "/api/staff/config/safety-rules/1"},
	{http.MethodDelete, "/api/staff/config/safety-rules/1"},
	{http.MethodGet, "/api/staff/config/rag"},
	{http.MethodPut, "/api/staff/config/rag"},
	{http.MethodGet, "/api/staff/config/prompts"},
	{http.MethodPost, "/api/staff/config/prompts"},
	{http.MethodPut, "/api/staff/config/prompts/1"},
	{http.MethodDelete, "/api/staff/config/prompts/1"},
	{http.MethodGet, "/api/staff/config/prompts/effective"},
	{http.MethodGet, "/api/staff/config/safety-messages"},
	{http.MethodGet, "/api/staff/config/safety-policy"},
	{http.MethodPut, "/api/staff/config/safety-messages"},
	{http.MethodGet, "/api/staff/config/audit-logs"},
	{http.MethodGet, "/api/staff/config/status"},
}

// staffEndpoints 需 JWT + Staff 角色的端点（18 个）。
var staffEndpoints = []endpoint{
	{http.MethodPost, "/api/staff/wiki/articles"},
	{http.MethodGet, "/api/staff/wiki/articles"},
	{http.MethodGet, "/api/staff/wiki/articles/1"},
	{http.MethodPut, "/api/staff/wiki/articles/1"},
	{http.MethodDelete, "/api/staff/wiki/articles/1"},
	{http.MethodPost, "/api/staff/wiki/articles/1/submit"},
	{http.MethodPost, "/api/staff/wiki/articles/1/approve"},
	{http.MethodPost, "/api/staff/wiki/articles/1/reject"},
	{http.MethodPost, "/api/staff/wiki/articles/1/archive"},
	{http.MethodPost, "/api/staff/wiki/articles/1/unarchive"},
	{http.MethodPost, "/api/staff/wiki/articles/1/featured"},
	{http.MethodGet, "/api/staff/wiki/articles/1/chunks"},
	{http.MethodPost, "/api/staff/wiki/articles/1/revectorize"},
	{http.MethodPost, "/api/staff/wiki/references"},
	{http.MethodGet, "/api/staff/wiki/references"},
	{http.MethodPost, "/api/staff/wiki/references/1/approve"},
	{http.MethodPost, "/api/staff/wiki/references/1/reject"},
	{http.MethodDelete, "/api/staff/wiki/references/1"},
	{http.MethodGet, "/api/staff/wiki/references/articles"},
	{http.MethodGet, "/api/staff/chat/crisis-events"},
	{http.MethodPost, "/api/staff/chat/crisis-events/1/handle"},
}

// chatEndpoints 需 JWT + 任意角色的聊天端点（7 个）— 聊天对所有已登录用户开放。
var chatEndpoints = []endpoint{
	{http.MethodPost, "/api/chat/stream"},
	{http.MethodGet, "/api/chat/conversations"},
	{http.MethodGet, "/api/chat/conversations/" + testUUID},
	{http.MethodPatch, "/api/chat/conversations/" + testUUID},
	{http.MethodDelete, "/api/chat/conversations/" + testUUID},
	{http.MethodGet, "/api/chat/conversations/" + testUUID + "/messages"},
	{http.MethodPost, "/api/chat/messages/1/feedback"},
}

// configEndpoints 需 JWT + Admin 角色的端点（27 个）。
var configEndpoints = []endpoint{
	{http.MethodGet, "/api/staff/config/ai-providers"},
	{http.MethodPost, "/api/staff/config/ai-providers"},
	{http.MethodGet, "/api/staff/config/ai-providers/1"},
	{http.MethodPut, "/api/staff/config/ai-providers/1"},
	{http.MethodDelete, "/api/staff/config/ai-providers/1"},
	{http.MethodPost, "/api/staff/config/ai-providers/1/test"},
	{http.MethodGet, "/api/staff/config/sensitive-words"},
	{http.MethodPost, "/api/staff/config/sensitive-words"},
	{http.MethodPut, "/api/staff/config/sensitive-words/1"},
	{http.MethodDelete, "/api/staff/config/sensitive-words/1"},
	{http.MethodGet, "/api/staff/config/safety-rules"},
	{http.MethodPost, "/api/staff/config/safety-rules"},
	{http.MethodPut, "/api/staff/config/safety-rules/1"},
	{http.MethodDelete, "/api/staff/config/safety-rules/1"},
	{http.MethodGet, "/api/staff/config/rag"},
	{http.MethodPut, "/api/staff/config/rag"},
	{http.MethodGet, "/api/staff/config/prompts"},
	{http.MethodPost, "/api/staff/config/prompts"},
	{http.MethodPut, "/api/staff/config/prompts/1"},
	{http.MethodDelete, "/api/staff/config/prompts/1"},
	{http.MethodGet, "/api/staff/config/prompts/effective"},
	{http.MethodGet, "/api/staff/config/safety-messages"},
	{http.MethodGet, "/api/staff/config/safety-policy"},
	{http.MethodPut, "/api/staff/config/safety-messages"},
	{http.MethodGet, "/api/staff/config/audit-logs"},
	{http.MethodGet, "/api/staff/config/status"},
}

// adminBaseEndpoints 需 JWT + Admin 角色的 base 域端点（5 个）。
var adminBaseEndpoints = []endpoint{
	{http.MethodGet, "/api/staff/base/departments"},
	{http.MethodPost, "/api/staff/base/departments"},
	{http.MethodGet, "/api/staff/base/departments/1"},
	{http.MethodPatch, "/api/staff/base/departments/1"},
	{http.MethodDelete, "/api/staff/base/departments/1"},
}

// adminAuthEndpoints 需 JWT + Admin 角色的 auth 域端点（8 个）。
var adminAuthEndpoints = []endpoint{
	{http.MethodGet, "/api/staff/auth/accounts"},
	{http.MethodPost, "/api/staff/auth/accounts"},
	{http.MethodPost, "/api/staff/auth/accounts/1/lock"},
	{http.MethodPost, "/api/staff/auth/accounts/1/unlock"},
	{http.MethodDelete, "/api/staff/auth/accounts/1"},
	{http.MethodPost, "/api/staff/auth/accounts/1/restore"},
	{http.MethodPost, "/api/staff/auth/accounts/1/reset-password"},
	{http.MethodPatch, "/api/staff/auth/accounts/1/department"},
	{http.MethodPatch, "/api/staff/auth/accounts/1/role"},
}

// ==================== P0: 路由完整性 ====================

// TestRouteIntegrity_AllEndpointsReachable 验证路由树中注册的每个端点均可达（非 404）。
// 端点清单来自 chi.Walk 遍历真实路由树（单一真源），路由增删由代码自证。
func TestRouteIntegrity_AllEndpointsReachable(t *testing.T) {
	eps := allRoutes(t)
	t.Logf("collected %d endpoints from route tree", len(eps))
	for _, ep := range eps {
		t.Run(ep.method+"_"+ep.path, func(t *testing.T) {
			rec := doRequest(ep.method, ep.path, "")
			if rec.Code == http.StatusNotFound {
				t.Errorf("endpoint %s %s returned 404 (route not registered)", ep.method, ep.path)
			}
		})
	}
}

// ==================== P0: 鉴权门禁 ====================

// TestAuthGate_ProtectedEndpoints_RequireToken 受保护端点无 token 应返回 401。
func TestAuthGate_ProtectedEndpoints_RequireToken(t *testing.T) {
	for _, ep := range protectedEndpoints {
		t.Run(ep.method+"_"+ep.path, func(t *testing.T) {
			rec := doRequest(ep.method, ep.path, "")
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d", rec.Code)
			}
		})
	}
}

// TestAuthGate_PublicEndpoints_NoAuthRequired 公开端点无 token 不应返回 401/403。
func TestAuthGate_PublicEndpoints_NoAuthRequired(t *testing.T) {
	for _, ep := range publicEndpoints {
		t.Run(ep.method+"_"+ep.path, func(t *testing.T) {
			rec := doRequest(ep.method, ep.path, "")
			if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
				t.Errorf("public endpoint returned %d", rec.Code)
			}
		})
	}
}

// ==================== P1: 角色控制 ====================

// TestRoleGate_PatientCannotAccessStaffEndpoints 患者角色访问医护端点应返回 403。
func TestRoleGate_PatientCannotAccessStaffEndpoints(t *testing.T) {
	token := signTestToken(1, constants.RolePatient, 0)
	for _, ep := range staffEndpoints {
		t.Run(ep.method+"_"+ep.path, func(t *testing.T) {
			rec := doRequest(ep.method, ep.path, token)
			if rec.Code != http.StatusForbidden {
				t.Errorf("expected 403, got %d", rec.Code)
			}
		})
	}
}

// TestRoleGate_AnyRoleCanAccessChatEndpoints 医护角色访问聊天端点不应返回 403（聊天对所有已登录用户开放）。
func TestRoleGate_AnyRoleCanAccessChatEndpoints(t *testing.T) {
	token := signTestToken(1, constants.RoleDoctor, 100)
	for _, ep := range chatEndpoints {
		t.Run(ep.method+"_"+ep.path, func(t *testing.T) {
			rec := doRequest(ep.method, ep.path, token)
			if rec.Code == http.StatusForbidden {
				t.Errorf("chat endpoint should be accessible to any role, got 403")
			}
		})
	}
}

// TestRoleGate_ConfigEndpoints_RequireAdmin 非管理员（DOCTOR）访问 config 端点应返回 403。
func TestRoleGate_ConfigEndpoints_RequireAdmin(t *testing.T) {
	token := signTestToken(1, constants.RoleDoctor, 100)
	for _, ep := range configEndpoints {
		t.Run(ep.method+"_"+ep.path, func(t *testing.T) {
			rec := doRequest(ep.method, ep.path, token)
			if rec.Code != http.StatusForbidden {
				t.Errorf("expected 403, got %d", rec.Code)
			}
		})
	}
}

// TestRoleGate_AdminAuthEndpoints_RequireAdmin 非管理员（DOCTOR）访问 auth 管理端点应返回 403。
func TestRoleGate_AdminAuthEndpoints_RequireAdmin(t *testing.T) {
	token := signTestToken(1, constants.RoleDoctor, 100)
	for _, ep := range adminAuthEndpoints {
		t.Run(ep.method+"_"+ep.path, func(t *testing.T) {
			rec := doRequest(ep.method, ep.path, token)
			if rec.Code != http.StatusForbidden {
				t.Errorf("expected 403, got %d", rec.Code)
			}
		})
	}
}

// TestRoleGate_BaseAdminEndpoints_RequireAdmin 非管理员（DOCTOR）访问 base 管理端点应返回 403。
func TestRoleGate_BaseAdminEndpoints_RequireAdmin(t *testing.T) {
	token := signTestToken(1, constants.RoleDoctor, 100)
	for _, ep := range adminBaseEndpoints {
		t.Run(ep.method+"_"+ep.path, func(t *testing.T) {
			rec := doRequest(ep.method, ep.path, token)
			if rec.Code != http.StatusForbidden {
				t.Errorf("expected 403, got %d", rec.Code)
			}
		})
	}
}

// ==================== P2: 响应格式 ====================

// TestErrorFormat_Unauthorized 401 响应体含 {code, message}。
func TestErrorFormat_Unauthorized(t *testing.T) {
	rec := doRequest(http.MethodPost, "/api/auth/logout", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := body["code"]; !ok {
		t.Error("response missing 'code' field")
	}
	if _, ok := body["message"]; !ok {
		t.Error("response missing 'message' field")
	}
}

// TestErrorFormat_NotFound 404 响应体含 {code: "NOT_FOUND", message}。
func TestErrorFormat_NotFound(t *testing.T) {
	rec := doRequest(http.MethodGet, "/api/nonexistent-path", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["code"] != "NOT_FOUND" {
		t.Errorf("expected code NOT_FOUND, got %v", body["code"])
	}
}

// TestErrorFormat_MethodNotAllowed 405 响应体含 {code: "METHOD_NOT_ALLOWED", message}。
// 使用独立 chi.Router 验证 405 格式，避免与 chat 域 catch-all mount 的路由优先级干扰。
func TestErrorFormat_MethodNotAllowed(t *testing.T) {
	r := chi.NewRouter()
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		response.WriteJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"code":    "METHOD_NOT_ALLOWED",
			"message": "请求方法不允许",
		})
	})
	r.Get("/test", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodDelete, "/test", http.NoBody)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["code"] != "METHOD_NOT_ALLOWED" {
		t.Errorf("expected code METHOD_NOT_ALLOWED, got %v", body["code"])
	}
}

// ==================== P0: 限流 scope 隔离 ====================

// rateKeyRedisPrefix 是限流 key 在 Redis 中的完整前缀：
// redis_rate 包内常量 "rate:" + middleware.rateKeyPrefix "rate" = "rate:rate:"。
// 完整 key 形如 "rate:rate:<scope>:<ip>"。
const rateKeyRedisPrefix = "rate:rate:"

// scopeCapturingHook 捕获 redis_rate 经 go-redis 发出的 EVAL/EVALSHA 命令中的限流 key。
//
// 利用 go-redis Hook 机制：ProcessHook 在命令实际下发前调用（即使指向不可达 Redis，
// next(ctx, cmd) 必然失败返回 503，hook 已先于失败记录下 cmd.Args() 中的 key）。
// hook 在重试层之上——每个顶层命令仅触发一次（go-redis 的 MaxRetries 重试在 baseProcess 内部，
// 不会重新走 hook 链），因此每个请求恰好捕获一个 key。
type scopeCapturingHook struct {
	mu   sync.Mutex
	keys []string
}

func (h *scopeCapturingHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *scopeCapturingHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		// redis_rate 经 Script.Run 走 EVALSHA（NOSCRIPT 时回退 EVAL）。
		// 扫描 cmd.Args() 中以 rateKeyRedisPrefix 为前缀的字符串即限流 key，
		// 避免依赖 EVAL/EVALSHA 的 arg 下标布局（name 在 args[0]，key 在 args[3]）。
		if name := cmd.Name(); name == "evalsha" || name == "eval" {
			for _, arg := range cmd.Args() {
				if key, ok := arg.(string); ok && strings.HasPrefix(key, rateKeyRedisPrefix) {
					h.mu.Lock()
					h.keys = append(h.keys, key)
					h.mu.Unlock()
					break
				}
			}
		}
		return next(ctx, cmd)
	}
}

func (h *scopeCapturingHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error { return next(ctx, cmds) }
}

// take 返回并清空已捕获的 key——每个子测试独立断言，避免上一请求残留污染。
func (h *scopeCapturingHook) take() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, len(h.keys))
	copy(out, h.keys)
	h.keys = nil
	return out
}

// newScopeRecordingRouter 构造仅含 auth 域的测试路由，使用 hook 捕获限流 key。
// 仅挂载 auth 域（chat/wiki/config 不挂）避免噪声 Redis 命令；
// auth 自助/管理员端点不限流，不会触发 hook。复用 fakeRateLimiter 的不可达 Redis 地址。
func newScopeRecordingRouter(hook *scopeCapturingHook) http.Handler {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"}) // 不可达：连接被拒，必然触发 503
	rdb.AddHook(hook)
	rl := middleware.NewRateLimiter(rdb, nil)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recover)
	authhandler.NewAuthHandler(nil).Mount(r, testAuth, rl, config.RateLimitConfig{})
	return r
}

// rateLimitScopeFromKey 从 "rate:rate:<scope>:<ip>" 中提取 <scope>。
// scope 自身不含 ':'（auth / auth_register / auth_refresh）；
// httptest.NewRequest 默认 RemoteAddr 为 IPv4 "192.0.2.1:1234"，clientIP 返回 "192.0.2.1"（无 ':'）。
func rateLimitScopeFromKey(key string) (string, bool) {
	if !strings.HasPrefix(key, rateKeyRedisPrefix) {
		return "", false
	}
	rest := key[len(rateKeyRedisPrefix):] // "<scope>:<ip>"
	idx := strings.LastIndex(rest, ":")
	if idx < 0 {
		return rest, true
	}
	return rest[:idx], true
}

// TestRateLimitScope 验证 auth 域限流 scope 隔离（REQ-NFR-003）。
//
// scope 拆分防止登录爆破连坐 register/refresh：攻击者耗尽 auth scope 额度后，
// 正常用户仍可注册/刷新。断言：
//   - login 使用 "auth" scope
//   - register 使用独立 "auth_register" scope
//   - refresh 使用独立 "auth_refresh" scope
//
// 实现：通过 go-redis Hook 捕获 redis_rate 发往 Redis 的 EVALSHA 命令中的限流 key
// （Redis 不可达，必然返回 503——但 hook 在命令下发前已记录 key，足以验证 scope 接线）。
// 请求不会进入 handler（svc 为 nil），故无 nil panic 风险。
func TestRateLimitScope(t *testing.T) {
	hook := &scopeCapturingHook{}
	router := newScopeRecordingRouter(hook)

	cases := []struct {
		name  string
		path  string
		scope string
	}{
		{"统一登录_使用auth_scope", "/api/auth/login", "auth"},
		{"注册_使用独立auth_register_scope", "/api/auth/register", "auth_register"},
		{"刷新_使用独立auth_refresh_scope", "/api/auth/refresh", "auth_refresh"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hook.take() // 清空上一子测试残留

			req := httptest.NewRequest(http.MethodPost, tc.path, http.NoBody)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			// 限流中间件必然因 Redis 不可达返回 503，请求不进入 handler（svc 为 nil 也不会 panic）。
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected 503 (rate limiter unavailable), got %d for %s", rec.Code, tc.path)
			}

			captured := hook.take()
			if len(captured) == 0 {
				t.Fatalf("%s: 限流中间件未运行（无 EVALSHA 命令捕获）", tc.path)
			}

			// 去重断言：至少一个捕获的 key 匹配预期 scope。
			found := false
			for _, key := range captured {
				scope, ok := rateLimitScopeFromKey(key)
				if !ok {
					continue
				}
				if scope == tc.scope {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s: 期望 scope=%q，捕获 keys=%v", tc.path, tc.scope, captured)
			}
		})
	}
}
