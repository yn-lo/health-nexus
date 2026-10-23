// Package middleware 提供 HTTP 中间件：请求 ID、日志、恢复、CORS、JWT 鉴权、角色控制、限流。
package middleware

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"health-nexus/internal/config"
	"health-nexus/internal/shared/contextkeys"
	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/identity"
	"health-nexus/internal/shared/response"
)

// SessionInfo 令牌对应用户的**当前**会话状态（取自 DB，不信任令牌内声明）。
type SessionInfo struct {
	Role         string
	DeptID       int64
	TokenVersion int64
	Active       bool
}

// SessionStore 读取用户当前会话状态，用于令牌失效与权限变更即时生效。
// 由 adapter 桥接 auth 域 UserRepo 实现，在装配时注入 Authenticator。
type SessionStore interface {
	// CurrentSession 返回用户当前会话状态；用户不存在/已删除返回 (nil, nil)。
	CurrentSession(ctx context.Context, userID int64) (*SessionInfo, error)
}

// authScheme Authorization 头中的认证方案。
const authScheme = "Bearer"

// Authenticator JWT 验证器，持有 HS256 对称密钥和签发者。
// sessions 非 nil 时，每次请求校验账户当前状态（未锁定/未删除）与令牌版本一致，
// 并以 DB 中的 role/dept 覆盖令牌内声明——锁定、降权、调科后旧 access token 立即失效（P1）。
type Authenticator struct {
	secret   []byte
	issuer   string
	sessions SessionStore
}

// NewAuthenticator 用 cfg.JWT.Secret 构造验证器。sessions 可为 nil（仅验签，不校验账户状态）。
func NewAuthenticator(cfg config.JWTConfig, sessions SessionStore) (*Authenticator, error) {
	if cfg.Secret == "" {
		return nil, fmt.Errorf("jwt secret is empty")
	}
	return &Authenticator{secret: []byte(cfg.Secret), issuer: cfg.Issuer, sessions: sessions}, nil
}

// Parse 验证 token 的 HS256 签名、签发者，返回声明。
func (a *Authenticator) Parse(tokenString string) (*identity.Claims, error) {
	claims := &identity.Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		return a.secret, nil
	}, jwt.WithIssuer(a.issuer), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	if !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

// JWTAuth 校验 Authorization: Bearer <token>，写入 UserID/UserRole/DeptID 到 context。
func JWTAuth(auth *Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, err := extractBearer(r.Header.Get("Authorization"))
			if err != nil {
				response.WriteError(w, r, err)
				return
			}
			claims, err := auth.Parse(token)
			if err != nil {
				response.WriteError(w, r, apperrors.Unauthorized("UNAUTHORIZED", "无效的访问令牌"))
				return
			}
			// 拒绝 refresh token 用作 access token（token_type 必须为 "access"）。
			if claims.TokenType != "access" {
				response.WriteError(w, r, apperrors.Unauthorized("UNAUTHORIZED", "无效的访问令牌"))
				return
			}
			role, deptID := claims.Role, claims.DeptID
			// 账户状态与权限以当前 DB 为准，不信任令牌内可能过期的 role/dept。
			// fail-closed：DB 故障时拒绝请求，避免故障窗口内旧令牌继续持有原权限。
			if auth.sessions != nil {
				info, err := auth.sessions.CurrentSession(r.Context(), claims.UserID)
				if err != nil {
					slog.ErrorContext(r.Context(), "jwt: load session state failed",
						"err", err, "user_id", claims.UserID)
					response.WriteError(w, r, apperrors.ServiceUnavailable("AUTH_SESSION_UNAVAILABLE", "鉴权服务暂不可用"))
					return
				}
				if info == nil || !info.Active || info.TokenVersion != claims.TokenVersion {
					response.WriteError(w, r, apperrors.Unauthorized("UNAUTHORIZED", "无效的访问令牌"))
					return
				}
				role, deptID = info.Role, info.DeptID
			}
			ctx := r.Context()
			ctx = context.WithValue(ctx, contextkeys.UserID, claims.UserID)
			ctx = context.WithValue(ctx, contextkeys.UserRole, role)
			ctx = context.WithValue(ctx, contextkeys.DeptID, deptID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// extractBearer 从 Authorization 头提取 token，校验 Bearer 方案。
func extractBearer(header string) (string, error) {
	if header == "" {
		return "", apperrors.Unauthorized("UNAUTHORIZED", "缺少 Authorization 头")
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], authScheme) {
		return "", apperrors.Unauthorized("UNAUTHORIZED", "Authorization 方案无效")
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", apperrors.Unauthorized("UNAUTHORIZED", "令牌为空")
	}
	return token, nil
}
