// Package middleware 提供 HTTP 中间件：请求 ID、日志、恢复、CORS、JWT 鉴权、角色控制、限流。
package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"health-nexus/internal/config"
	"health-nexus/internal/shared/contextkeys"
	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/response"
)

// Claims JWT 自定义声明，扩展标准 RegisteredClaims。
type Claims struct {
	jwt.RegisteredClaims
	UserID    int64  `json:"user_id"`
	Role      string `json:"role"`
	DeptID    int64  `json:"dept_id,omitempty"`
	TokenType string `json:"token_type,omitempty"` // "access" 或 "refresh"；空表示历史 token
}

// authScheme Authorization 头中的认证方案。
const authScheme = "Bearer"

// Authenticator JWT 验证器，持有 HS256 对称密钥和签发者。
type Authenticator struct {
	secret []byte
	issuer string
}

// NewAuthenticator 用 cfg.JWT.Secret 构造验证器。
func NewAuthenticator(cfg config.JWTConfig) (*Authenticator, error) {
	if cfg.Secret == "" {
		return nil, fmt.Errorf("jwt secret is empty")
	}
	return &Authenticator{secret: []byte(cfg.Secret), issuer: cfg.Issuer}, nil
}

// Parse 验证 token 的 HS256 签名、签发者，返回声明。
func (a *Authenticator) Parse(tokenString string) (*Claims, error) {
	claims := &Claims{}
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
			ctx := r.Context()
			ctx = context.WithValue(ctx, contextkeys.UserID, claims.UserID)
			ctx = context.WithValue(ctx, contextkeys.UserRole, claims.Role)
			ctx = context.WithValue(ctx, contextkeys.DeptID, claims.DeptID)
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
