package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"health-nexus/internal/config"
	"health-nexus/internal/shared/constants"
	"health-nexus/internal/shared/contextkeys"
	"health-nexus/internal/shared/identity"
)

// stubSessions 固定返回同一份会话状态，用于验证 JWTAuth 是否以 DB 状态为准（P1）。
type stubSessions struct {
	info *SessionInfo
	err  error
}

func (s *stubSessions) CurrentSession(_ context.Context, _ int64) (*SessionInfo, error) {
	return s.info, s.err
}

const testJWTSecret = "test-secret-at-least-32-bytes-long!!"

// signAccess 用测试密钥签发一枚 access token。
func signAccess(t *testing.T, claims identity.Claims) string {
	t.Helper()
	claims.TokenType = "access"
	claims.Issuer = "test"
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Minute))
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, &claims).SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("签发 access token: %v", err)
	}
	return tok
}

// runJWTAuth 跑一次 JWTAuth 中间件，返回状态码与注入 context 的 (userID, role, deptID)。
func runJWTAuth(
	t *testing.T, sessions SessionStore, token string,
) (code int, userID int64, role string, deptID int64) {
	t.Helper()
	auth, err := NewAuthenticator(config.JWTConfig{Secret: testJWTSecret, Issuer: "test"}, sessions)
	if err != nil {
		t.Fatalf("构造 Authenticator: %v", err)
	}
	h := JWTAuth(auth)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, _ = r.Context().Value(contextkeys.UserID).(int64)
		role, _ = r.Context().Value(contextkeys.UserRole).(string)
		deptID, _ = r.Context().Value(contextkeys.DeptID).(int64)
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/staff/chat/crisis-events", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, userID, role, deptID
}

// TestJWTAuth_SessionStateOverridesClaims P1：权限以 DB 当前状态为准——降权/调科后旧 access token
// 不得继续持有令牌内声明的 role/dept。
func TestJWTAuth_SessionStateOverridesClaims(t *testing.T) {
	// 令牌声明为 DOCTOR/科室 10，但 DB 中当前已是 SUPER_ADMIN/科室 0。
	token := signAccess(t, identity.Claims{UserID: 42, Role: constants.RoleDoctor, DeptID: 10})
	code, userID, role, deptID := runJWTAuth(t, &stubSessions{
		info: &SessionInfo{Role: constants.RoleSuperAdmin, DeptID: 0, TokenVersion: 0, Active: true},
	}, token)
	if code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", code)
	}
	if userID != 42 {
		t.Errorf("期望 userID=42，实际 %d", userID)
	}
	if role != constants.RoleSuperAdmin || deptID != 0 {
		t.Errorf("期望以 DB 的 role/dept 覆盖令牌声明，实际 role=%s dept=%d", role, deptID)
	}
}

// TestJWTAuth_AccountNoLongerActive_Rejected P1：账户被锁定/删除后，旧 access token 立即失效。
func TestJWTAuth_AccountNoLongerActive_Rejected(t *testing.T) {
	token := signAccess(t, identity.Claims{UserID: 42, Role: constants.RoleDoctor, DeptID: 10})
	code, _, _, _ := runJWTAuth(t, &stubSessions{
		info: &SessionInfo{Role: constants.RoleDoctor, DeptID: 10, TokenVersion: 0, Active: false},
	}, token)
	if code != http.StatusUnauthorized {
		t.Fatalf("期望 401，实际 %d", code)
	}
}

// TestJWTAuth_TokenVersionMismatch_Rejected P1：改密后 token_version 递增，旧 access token 立即失效。
func TestJWTAuth_TokenVersionMismatch_Rejected(t *testing.T) {
	token := signAccess(t, identity.Claims{UserID: 42, Role: constants.RoleDoctor, DeptID: 10, TokenVersion: 0})
	code, _, _, _ := runJWTAuth(t, &stubSessions{
		info: &SessionInfo{Role: constants.RoleDoctor, DeptID: 10, TokenVersion: 1, Active: true},
	}, token)
	if code != http.StatusUnauthorized {
		t.Fatalf("期望 401，实际 %d", code)
	}
}

// TestJWTAuth_SessionStoreDown_FailClosed 会话存储故障时拒绝请求（fail-closed），
// 避免故障窗口内旧令牌继续持有原权限。
func TestJWTAuth_SessionStoreDown_FailClosed(t *testing.T) {
	token := signAccess(t, identity.Claims{UserID: 42, Role: constants.RoleDoctor, DeptID: 10})
	code, _, _, _ := runJWTAuth(t, &stubSessions{err: errors.New("db down")}, token)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("期望 503，实际 %d", code)
	}
}

// TestJWTAuth_NilSessionStore_TrustsClaims 未注入 SessionStore（如契约测试/本地）时退化为仅验签，
// 保证既有装配方式不被破坏。
func TestJWTAuth_NilSessionStore_TrustsClaims(t *testing.T) {
	token := signAccess(t, identity.Claims{UserID: 42, Role: constants.RoleDoctor, DeptID: 10})
	code, _, role, deptID := runJWTAuth(t, nil, token)
	if code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", code)
	}
	if role != constants.RoleDoctor || deptID != 10 {
		t.Errorf("期望沿用令牌声明 role=%s dept=%d，实际 role=%s dept=%d",
			constants.RoleDoctor, 10, role, deptID)
	}
}
