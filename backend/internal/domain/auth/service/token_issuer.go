// Package service 实现 auth 域的业务逻辑：登录/注册/刷新/登出。
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"health-nexus/internal/config"
	"health-nexus/internal/middleware"
)

// TokenTypeAccess / TokenTypeRefresh 区分 access 与 refresh token。
const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

// TokenIssuer 用 HS256 对称密钥签发 JWT access/refresh token。
type TokenIssuer struct {
	secret     []byte
	issuer     string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewTokenIssuer 从 cfg.JWT.Secret 构造 TokenIssuer。
func NewTokenIssuer(cfg config.JWTConfig) (*TokenIssuer, error) {
	if cfg.Secret == "" {
		return nil, fmt.Errorf("jwt secret is empty")
	}
	return &TokenIssuer{
		secret:     []byte(cfg.Secret),
		issuer:     cfg.Issuer,
		accessTTL:  cfg.AccessTTL,
		refreshTTL: cfg.RefreshTTL,
	}, nil
}

// Issue 签发 access 与 refresh token，二者携带相同 user_id/role/dept_id 但 TTL 与 token_type 不同。
// JWTAuth 中间件校验 token_type=access，Refresh 校验 token_type=refresh，双向互斥。
func (t *TokenIssuer) Issue(
	ctx context.Context, userID int64, role string, deptID int64,
) (access, refresh string, err error) {
	_ = ctx
	access, err = t.sign(userID, role, deptID, TokenTypeAccess, t.accessTTL)
	if err != nil {
		return "", "", fmt.Errorf("sign access: %w", err)
	}
	refresh, err = t.sign(userID, role, deptID, TokenTypeRefresh, t.refreshTTL)
	if err != nil {
		return "", "", fmt.Errorf("sign refresh: %w", err)
	}
	return access, refresh, nil
}

// RefreshTTL 返回 refresh token 的 TTL，供 logout 黑名单写入使用。
func (t *TokenIssuer) RefreshTTL() time.Duration { return t.refreshTTL }

func (t *TokenIssuer) sign(
	userID int64, role string, deptID int64, tokenType string, ttl time.Duration,
) (string, error) {
	now := time.Now()
	claims := &middleware.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewString(),
			Issuer:    t.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
		UserID:    userID,
		Role:      role,
		DeptID:    deptID,
		TokenType: tokenType,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(t.secret)
}
