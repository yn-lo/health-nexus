package service

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"health-nexus/internal/config"
	"health-nexus/internal/middleware"
)

// newTestTokenIssuer 在同包内直接构造 TokenIssuer，跳过配置读取。
func newTestTokenIssuer(t *testing.T) *TokenIssuer {
	t.Helper()
	return &TokenIssuer{
		secret:     []byte("test-secret-key"),
		issuer:     "health-nexus-test",
		accessTTL:  15 * time.Minute,
		refreshTTL: 7 * 24 * time.Hour,
	}
}

// parseToken 用测试 issuer 的密钥解析 token 并返回 Claims。
func parseToken(t *testing.T, issuer *TokenIssuer, tokenStr string) *middleware.Claims {
	t.Helper()
	claims := &middleware.Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (any, error) {
		return issuer.secret, nil
	})
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	if !token.Valid {
		t.Fatal("token 应为有效")
	}
	return claims
}

func TestNewTokenIssuer_EmptySecret(t *testing.T) {
	_, err := NewTokenIssuer(configJWT(""))
	if err == nil {
		t.Fatal("期望 error（secret 为空），实际 nil")
	}
}

func TestIssue_HappyPath(t *testing.T) {
	issuer := newTestTokenIssuer(t)
	access, refresh, err := issuer.Issue(context.Background(), 42, "patient", 1)
	if err != nil {
		t.Fatalf("Issue 返回 error: %v", err)
	}
	if access == "" {
		t.Error("access token 不应为空")
	}
	if refresh == "" {
		t.Error("refresh token 不应为空")
	}
	if access == refresh {
		t.Error("access 与 refresh token 不应相同")
	}
	parseToken(t, issuer, access)
	parseToken(t, issuer, refresh)
}

func TestIssue_TokenClaims(t *testing.T) {
	issuer := newTestTokenIssuer(t)
	access, _, err := issuer.Issue(context.Background(), 7, "doctor", 3)
	if err != nil {
		t.Fatalf("Issue 返回 error: %v", err)
	}
	claims := parseToken(t, issuer, access)
	if claims.UserID != 7 {
		t.Errorf("期望 UserID=7，实际 %d", claims.UserID)
	}
	if claims.Role != "doctor" {
		t.Errorf("期望 Role=doctor，实际 %s", claims.Role)
	}
	if claims.DeptID != 3 {
		t.Errorf("期望 DeptID=3，实际 %d", claims.DeptID)
	}
	if claims.Issuer != "health-nexus-test" {
		t.Errorf("期望 Issuer=health-nexus-test，实际 %s", claims.Issuer)
	}
	if claims.ID == "" {
		t.Error("期望 jti 非空")
	}
	if claims.IssuedAt.IsZero() {
		t.Error("期望 iat 非零")
	}
	if claims.ExpiresAt.IsZero() {
		t.Error("期望 exp 非零")
	}
}

func TestIssue_AccessTokenType(t *testing.T) {
	issuer := newTestTokenIssuer(t)
	access, _, err := issuer.Issue(context.Background(), 1, "admin", 0)
	if err != nil {
		t.Fatalf("Issue 返回 error: %v", err)
	}
	claims := parseToken(t, issuer, access)
	if claims.TokenType != TokenTypeAccess {
		t.Errorf("期望 token_type=%s，实际 %s", TokenTypeAccess, claims.TokenType)
	}
}

func TestIssue_RefreshTokenType(t *testing.T) {
	issuer := newTestTokenIssuer(t)
	_, refresh, err := issuer.Issue(context.Background(), 1, "admin", 0)
	if err != nil {
		t.Fatalf("Issue 返回 error: %v", err)
	}
	claims := parseToken(t, issuer, refresh)
	if claims.TokenType != TokenTypeRefresh {
		t.Errorf("期望 token_type=%s，实际 %s", TokenTypeRefresh, claims.TokenType)
	}
}

func TestRefreshTTL(t *testing.T) {
	issuer := newTestTokenIssuer(t)
	got := issuer.RefreshTTL()
	want := 7 * 24 * time.Hour
	if got != want {
		t.Errorf("期望 RefreshTTL=%v，实际 %v", want, got)
	}
}

// configJWT 返回仅填充 Secret 的 JWTConfig，用于测试 NewTokenIssuer 错误路径。
func configJWT(secret string) config.JWTConfig {
	return config.JWTConfig{
		Secret:     secret,
		AccessTTL:  15 * time.Minute,
		RefreshTTL: 7 * 24 * time.Hour,
		Issuer:     "test",
	}
}
