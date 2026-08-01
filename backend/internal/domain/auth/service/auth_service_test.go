// auth_service_test.go — AuthService 密码重置核心单元测试。
// 覆盖 RequestPasswordReset / ConfirmPasswordReset 关键路径。
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"health-nexus/internal/config"
	"health-nexus/internal/domain/auth/entity"
	"health-nexus/internal/platform/crypto"
	"health-nexus/internal/shared/constants"
	apperrors "health-nexus/internal/shared/errors"
)

// ============================================================================
// Mock 手写（interface 隐式实现，conventions.md §5）
// ============================================================================

type mockUserRepo struct {
	user         *entity.User
	getErr       error
	createUser   *entity.User
	createErr    error
	updatePwdErr error
	gotUpdateID  int64
	gotUpdatePwd string

	updateProfileErr error
	gotProfileID     int64
	gotProfilePhone  string
	gotProfileDOB    *time.Time
	gotProfileGender string
	gotProfileEC     string
	gotProfileEP     string

	setActiveErr error
	gotActiveID  int64
	gotActive    bool

	softDeleteErr error
	gotDeleteID   int64

	listUsers []*entity.User
	listTotal int64
	listErr   error

	listByDeptUsers []*entity.User
	listByDeptTotal int64
	listByDeptErr   error
}

func (m *mockUserRepo) GetByUsername(_ context.Context, _ string) (*entity.User, error) {
	return m.user, m.getErr
}

func (m *mockUserRepo) GetByID(_ context.Context, _ int64) (*entity.User, error) {
	return m.user, m.getErr
}

func (m *mockUserRepo) Create(_ context.Context, _, _, _ string) (*entity.User, error) {
	return m.createUser, m.createErr
}

func (m *mockUserRepo) UpdatePasswordHash(_ context.Context, userID int64, passwordHash string) error {
	m.gotUpdateID = userID
	m.gotUpdatePwd = passwordHash
	return m.updatePwdErr
}

func (m *mockUserRepo) UpdateProfile(_ context.Context, userID int64, phone string, dateOfBirth *time.Time, gender string, emergencyContact string, emergencyPhone string) error {
	m.gotProfileID = userID
	m.gotProfilePhone = phone
	m.gotProfileDOB = dateOfBirth
	m.gotProfileGender = gender
	m.gotProfileEC = emergencyContact
	m.gotProfileEP = emergencyPhone
	// 同步更新 user 实体，使后续 GetByID（由 GetProfile 调用）返回更新后的数据。
	if m.user != nil {
		m.user.Phone = phone
		m.user.DateOfBirth = dateOfBirth
		m.user.Gender = gender
		m.user.EmergencyContact = emergencyContact
		m.user.EmergencyPhone = emergencyPhone
	}
	return m.updateProfileErr
}

func (m *mockUserRepo) SetActive(_ context.Context, userID int64, active bool) error {
	m.gotActiveID = userID
	m.gotActive = active
	return m.setActiveErr
}

func (m *mockUserRepo) SoftDelete(_ context.Context, userID int64) error {
	m.gotDeleteID = userID
	return m.softDeleteErr
}

func (m *mockUserRepo) List(_ context.Context, _, _ int) ([]*entity.User, int64, error) {
	return m.listUsers, m.listTotal, m.listErr
}

func (m *mockUserRepo) ListByDept(_ context.Context, deptID, limit, offset int64) ([]*entity.User, int64, error) {
	return m.listByDeptUsers, m.listByDeptTotal, m.listByDeptErr
}

// mockRedis 实现 RedisClient 接口，用内存 map 模拟。
type mockRedis struct {
	store map[string]string
	ttls  map[string]time.Duration
	// 错误注入。
	getErr error
	setErr error
	delErr error
}

func newMockRedis() *mockRedis {
	return &mockRedis{store: make(map[string]string), ttls: make(map[string]time.Duration)}
}

func (m *mockRedis) Set(_ context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd {
	if m.setErr != nil {
		return redis.NewStatusResult("", m.setErr)
	}
	m.store[key] = fmt.Sprintf("%v", value)
	m.ttls[key] = expiration
	return redis.NewStatusResult("OK", nil)
}

func (m *mockRedis) Get(_ context.Context, key string) *redis.StringCmd {
	if m.getErr != nil {
		return redis.NewStringResult("", m.getErr)
	}
	v, ok := m.store[key]
	if !ok {
		return redis.NewStringResult("", redis.Nil)
	}
	return redis.NewStringResult(v, nil)
}

func (m *mockRedis) SetNX(_ context.Context, key string, value any, _ time.Duration) *redis.BoolCmd {
	if m.setErr != nil {
		return redis.NewBoolResult(false, m.setErr)
	}
	_, exists := m.store[key]
	if !exists {
		m.store[key] = fmt.Sprintf("%v", value)
	}
	return redis.NewBoolResult(!exists, nil)
}

func (m *mockRedis) Del(_ context.Context, keys ...string) *redis.IntCmd {
	if m.delErr != nil {
		return redis.NewIntResult(0, m.delErr)
	}
	var count int64
	for _, k := range keys {
		if _, ok := m.store[k]; ok {
			delete(m.store, k)
			count++
		}
	}
	return redis.NewIntResult(count, nil)
}

// ============================================================================
// RequestPasswordReset 测试
// ============================================================================

func TestRequestPasswordReset_UserNotFound_ReturnsNil(t *testing.T) {
	repo := &mockUserRepo{user: nil}
	svc := newTestAuthService(repo, nil)
	err := svc.RequestPasswordReset(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("期望 nil（安全设计：不泄露用户是否存在），实际 %v", err)
	}
}

func TestRequestPasswordReset_AccountLocked_Returns423(t *testing.T) {
	repo := &mockUserRepo{user: &entity.User{ID: 1, IsActive: false}}
	svc := newTestAuthService(repo, nil)
	err := svc.RequestPasswordReset(context.Background(), "locked")
	assertAppErr(t, err, 423, "AUTH_ACCOUNT_LOCKED")
}

func TestRequestPasswordReset_HappyPath_StoresToken(t *testing.T) {
	repo := &mockUserRepo{user: &entity.User{ID: 42, IsActive: true, Username: "alice"}}
	rdb := newMockRedis()
	svc := newTestAuthService(repo, rdb)
	err := svc.RequestPasswordReset(context.Background(), "alice")
	if err != nil {
		t.Fatalf("期望 nil，实际 %v", err)
	}
	// 验证 Redis 中存入了 token（key 前缀匹配，value = "42"）。
	found := false
	for k, v := range rdb.store {
		if len(k) > len(passwordResetKeyPrefix) && k[:len(passwordResetKeyPrefix)] == passwordResetKeyPrefix && v == "42" {
			found = true
			break
		}
	}
	if !found {
		t.Error("期望 Redis 中存储 password_reset:<token> -> \"42\"")
	}
}

func TestRequestPasswordReset_RedisDown_Returns503(t *testing.T) {
	repo := &mockUserRepo{user: &entity.User{ID: 1, IsActive: true}}
	rdb := newMockRedis()
	rdb.setErr = errors.New("redis connection refused")
	svc := newTestAuthService(repo, rdb)
	err := svc.RequestPasswordReset(context.Background(), "alice")
	assertAppErr(t, err, 503, "AUTH_RESET_UNAVAILABLE")
}

// ============================================================================
// ConfirmPasswordReset 测试
// ============================================================================

func TestConfirmPasswordReset_WeakPassword_Returns422(t *testing.T) {
	repo := &mockUserRepo{}
	svc := newTestAuthService(repo, nil)
	err := svc.ConfirmPasswordReset(context.Background(), "sometoken", "123")
	assertAppErr(t, err, 422, "AUTH_PASSWORD_WEAK")
}

func TestConfirmPasswordReset_InvalidToken_Returns400(t *testing.T) {
	repo := &mockUserRepo{}
	rdb := newMockRedis()
	// 不预置 token → mock Get 返回 redis.Nil（模拟 key 不存在）。
	svc := newTestAuthService(repo, rdb)
	err := svc.ConfirmPasswordReset(context.Background(), "badtoken", "NewPass123")
	assertAppErr(t, err, 400, "AUTH_RESET_TOKEN_INVALID")
}

func TestConfirmPasswordReset_HappyPath_UpdatesPassword(t *testing.T) {
	repo := &mockUserRepo{}
	rdb := newMockRedis()
	rdb.store[passwordResetKeyPrefix+"validtoken"] = "42"
	svc := newTestAuthService(repo, rdb)
	err := svc.ConfirmPasswordReset(context.Background(), "validtoken", "NewPass123")
	if err != nil {
		t.Fatalf("期望 nil，实际 %v", err)
	}
	if repo.gotUpdateID != 42 {
		t.Errorf("期望 UpdatePasswordHash userID=42，实际 %d", repo.gotUpdateID)
	}
	if repo.gotUpdatePwd == "" {
		t.Error("期望 passwordHash 非空")
	}
	// token 已被删除（一次性）。
	if _, ok := rdb.store[passwordResetKeyPrefix+"validtoken"]; ok {
		t.Error("期望 token 已被删除（一次性使用）")
	}
}

func TestConfirmPasswordReset_RepoDown_PropagatesError(t *testing.T) {
	repo := &mockUserRepo{updatePwdErr: errors.New("db connection lost")}
	rdb := newMockRedis()
	rdb.store[passwordResetKeyPrefix+"tok"] = "1"
	svc := newTestAuthService(repo, rdb)
	err := svc.ConfirmPasswordReset(context.Background(), "tok", "NewPass123")
	if err == nil {
		t.Fatal("期望 error，实际 nil")
	}
}

// ============================================================================
// ChangePassword 测试
// ============================================================================

func TestChangePassword_UserNotFound_Returns404(t *testing.T) {
	repo := &mockUserRepo{user: nil}
	svc := newTestAuthService(repo, nil)
	err := svc.ChangePassword(context.Background(), 99, "OldPass123", "NewPass123")
	assertAppErr(t, err, 404, "AUTH_USER_NOT_FOUND")
}

func TestChangePassword_WrongOldPassword_Returns401(t *testing.T) {
	hash, _ := crypto.HashPassword("CorrectOld1", testConfig().Argon2)
	repo := &mockUserRepo{user: &entity.User{ID: 1, PasswordHash: hash}}
	svc := newTestAuthService(repo, nil)
	err := svc.ChangePassword(context.Background(), 1, "WrongOld123", "NewPass123")
	assertAppErr(t, err, 401, "AUTH_OLD_PASSWORD_WRONG")
}

func TestChangePassword_WeakNewPassword_Returns422(t *testing.T) {
	hash, _ := crypto.HashPassword("OldPass123", testConfig().Argon2)
	repo := &mockUserRepo{user: &entity.User{ID: 1, PasswordHash: hash}}
	svc := newTestAuthService(repo, nil)
	err := svc.ChangePassword(context.Background(), 1, "OldPass123", "123")
	assertAppErr(t, err, 422, "AUTH_PASSWORD_WEAK")
}

func TestChangePassword_HappyPath_UpdatesHash(t *testing.T) {
	hash, _ := crypto.HashPassword("OldPass123", testConfig().Argon2)
	repo := &mockUserRepo{user: &entity.User{ID: 7, PasswordHash: hash}}
	svc := newTestAuthService(repo, nil)
	err := svc.ChangePassword(context.Background(), 7, "OldPass123", "NewPass456")
	if err != nil {
		t.Fatalf("期望 nil，实际 %v", err)
	}
	if repo.gotUpdateID != 7 {
		t.Errorf("期望 UpdatePasswordHash userID=7，实际 %d", repo.gotUpdateID)
	}
	if repo.gotUpdatePwd == "" || repo.gotUpdatePwd == hash {
		t.Error("期望 passwordHash 已更新为新哈希")
	}
}

func TestChangePassword_RepoDown_PropagatesError(t *testing.T) {
	hash, _ := crypto.HashPassword("OldPass123", testConfig().Argon2)
	repo := &mockUserRepo{user: &entity.User{ID: 1, PasswordHash: hash}, updatePwdErr: errors.New("db down")}
	svc := newTestAuthService(repo, nil)
	err := svc.ChangePassword(context.Background(), 1, "OldPass123", "NewPass456")
	if err == nil {
		t.Fatal("期望 error，实际 nil")
	}
}

// ============================================================================
// GetProfile 测试
// ============================================================================

func TestGetProfile_UserNotFound_Returns404(t *testing.T) {
	repo := &mockUserRepo{user: nil}
	svc := newTestAuthService(repo, nil)
	_, err := svc.GetProfile(context.Background(), 99)
	assertAppErr(t, err, 404, "AUTH_USER_NOT_FOUND")
}

func TestGetProfile_HappyPath_ReturnsDTO(t *testing.T) {
	repo := &mockUserRepo{user: &entity.User{ID: 5, Username: "alice", Role: constants.RolePatient, Phone: "13800138000"}}
	svc := newTestAuthService(repo, nil)
	dto, err := svc.GetProfile(context.Background(), 5)
	if err != nil {
		t.Fatalf("期望 nil，实际 %v", err)
	}
	if dto.ID != 5 || dto.Username != "alice" || dto.Role != constants.RolePatient || dto.Phone != "13800138000" {
		t.Errorf("DTO 字段不匹配: %+v", dto)
	}
}

// ============================================================================
// UpdateProfile 测试
// ============================================================================

func TestUpdateProfile_PhoneTooLong_Returns422(t *testing.T) {
	repo := &mockUserRepo{user: &entity.User{ID: 1}}
	svc := newTestAuthService(repo, nil)
	longPhone := strings.Repeat("1", 21)
	_, err := svc.UpdateProfile(context.Background(), 1, longPhone, nil, "", "", "")
	assertAppErr(t, err, 422, "AUTH_PHONE_TOO_LONG")
}

func TestUpdateProfile_InvalidGender_Returns422(t *testing.T) {
	repo := &mockUserRepo{user: &entity.User{ID: 1}}
	svc := newTestAuthService(repo, nil)
	_, err := svc.UpdateProfile(context.Background(), 1, "", nil, "invalid", "", "")
	assertAppErr(t, err, 422, "AUTH_GENDER_INVALID")
}

func TestUpdateProfile_EmergencyContactTooLong_Returns422(t *testing.T) {
	repo := &mockUserRepo{user: &entity.User{ID: 1}}
	svc := newTestAuthService(repo, nil)
	longContact := strings.Repeat("张", 65)
	_, err := svc.UpdateProfile(context.Background(), 1, "", nil, "", longContact, "")
	assertAppErr(t, err, 422, "AUTH_EMERGENCY_CONTACT_TOO_LONG")
}

func TestUpdateProfile_EmergencyPhoneTooLong_Returns422(t *testing.T) {
	repo := &mockUserRepo{user: &entity.User{ID: 1}}
	svc := newTestAuthService(repo, nil)
	longPhone := strings.Repeat("1", 21)
	_, err := svc.UpdateProfile(context.Background(), 1, "", nil, "", "", longPhone)
	assertAppErr(t, err, 422, "AUTH_EMERGENCY_PHONE_TOO_LONG")
}

func TestUpdateProfile_UserNotFound_Returns404(t *testing.T) {
	repo := &mockUserRepo{user: nil}
	svc := newTestAuthService(repo, nil)
	_, err := svc.UpdateProfile(context.Background(), 99, "", nil, "", "", "")
	assertAppErr(t, err, 404, "AUTH_USER_NOT_FOUND")
}

func TestUpdateProfile_HappyPath_ReturnsUpdatedDTO(t *testing.T) {
	repo := &mockUserRepo{user: &entity.User{ID: 3, Username: "bob", Role: constants.RoleDoctor}}
	svc := newTestAuthService(repo, nil)
	dto, err := svc.UpdateProfile(context.Background(), 3, "13800138000", nil, "male", "张三", "13900139000")
	if err != nil {
		t.Fatalf("期望 nil，实际 %v", err)
	}
	if dto.Phone != "13800138000" {
		t.Errorf("期望 phone 已更新，实际 %s", dto.Phone)
	}
	if repo.gotProfileID != 3 || repo.gotProfilePhone != "13800138000" || repo.gotProfileGender != "male" || repo.gotProfileEC != "张三" || repo.gotProfileEP != "13900139000" {
		t.Errorf("期望 repo.UpdateProfile(3, 13800138000, male, 张三, 13900139000)，实际 (%d, %s, %s, %s, %s)", repo.gotProfileID, repo.gotProfilePhone, repo.gotProfileGender, repo.gotProfileEC, repo.gotProfileEP)
	}
}

func TestUpdateProfile_RepoDown_PropagatesError(t *testing.T) {
	repo := &mockUserRepo{user: &entity.User{ID: 1}, updateProfileErr: errors.New("db down")}
	svc := newTestAuthService(repo, nil)
	_, err := svc.UpdateProfile(context.Background(), 1, "", nil, "", "", "")
	if err == nil {
		t.Fatal("期望 error，实际 nil")
	}
}

// ============================================================================
// ListAccounts 测试
// ============================================================================

func TestListAccounts_HappyPath_ReturnsDTOs(t *testing.T) {
	now := time.Now()
	repo := &mockUserRepo{
		listUsers: []*entity.User{
			{ID: 1, Username: "admin", Role: constants.RoleSuperAdmin, IsActive: true, CreatedAt: now},
			{ID: 2, Username: "patient1", Role: constants.RolePatient, IsActive: false, CreatedAt: now},
		},
		listTotal: 2,
	}
	svc := newTestAuthService(repo, nil)
	dtos, total, err := svc.ListAccounts(context.Background(), constants.RoleSuperAdmin, 0, 1, 20)
	if err != nil {
		t.Fatalf("期望 nil，实际 %v", err)
	}
	if total != 2 || len(dtos) != 2 {
		t.Fatalf("期望 total=2 len=2，实际 total=%d len=%d", total, len(dtos))
	}
	if dtos[0].Username != "admin" || dtos[1].IsActive != false {
		t.Errorf("DTO 字段不匹配: %+v", dtos)
	}
}

func TestListAccounts_RepoDown_PropagatesError(t *testing.T) {
	repo := &mockUserRepo{listErr: errors.New("db down")}
	svc := newTestAuthService(repo, nil)
	_, _, err := svc.ListAccounts(context.Background(), constants.RoleSuperAdmin, 0, 1, 20)
	if err == nil {
		t.Fatal("期望 error，实际 nil")
	}
}

// ============================================================================
// CreateAccount 测试
// ============================================================================

func TestCreateAccount_InvalidRole_Returns400(t *testing.T) {
	repo := &mockUserRepo{}
	svc := newTestAuthService(repo, nil)
	_, err := svc.CreateAccount(context.Background(), constants.RoleSuperAdmin, "newuser", "Pass1234", "INVALID_ROLE")
	assertAppErr(t, err, 400, "AUTH_ROLE_INVALID")
}

func TestCreateAccount_NonSuperAdminCreatesAdmin_Returns403(t *testing.T) {
	repo := &mockUserRepo{}
	svc := newTestAuthService(repo, nil)
	_, err := svc.CreateAccount(context.Background(), constants.RoleDeptAdmin, "newadmin", "Pass1234", constants.RoleSuperAdmin)
	assertAppErr(t, err, 403, "AUTH_FORBIDDEN_ROLE")
}

func TestCreateAccount_WeakPassword_Returns422(t *testing.T) {
	repo := &mockUserRepo{}
	svc := newTestAuthService(repo, nil)
	_, err := svc.CreateAccount(context.Background(), constants.RoleSuperAdmin, "newuser", "123", constants.RoleDoctor)
	assertAppErr(t, err, 422, "AUTH_PASSWORD_WEAK")
}

func TestCreateAccount_HappyPath_ReturnsDTO(t *testing.T) {
	now := time.Now()
	repo := &mockUserRepo{createUser: &entity.User{ID: 10, Username: "newdoc", Role: constants.RoleDoctor, IsActive: true, CreatedAt: now}}
	svc := newTestAuthService(repo, nil)
	dto, err := svc.CreateAccount(context.Background(), constants.RoleSuperAdmin, "newdoc", "Pass1234", constants.RoleDoctor)
	if err != nil {
		t.Fatalf("期望 nil，实际 %v", err)
	}
	if dto.ID != 10 || dto.Username != "newdoc" || dto.Role != constants.RoleDoctor {
		t.Errorf("DTO 字段不匹配: %+v", dto)
	}
}

// ============================================================================
// SetAccountActive 测试
// ============================================================================

func TestSetAccountActive_SelfLock_Returns409(t *testing.T) {
	repo := &mockUserRepo{}
	svc := newTestAuthService(repo, nil)
	err := svc.SetAccountActive(context.Background(), 1, constants.RoleSuperAdmin, 1, false)
	assertAppErr(t, err, 409, "AUTH_SELF_LOCK")
}

func TestSetAccountActive_TargetNotFound_Returns404(t *testing.T) {
	repo := &mockUserRepo{user: nil}
	svc := newTestAuthService(repo, nil)
	err := svc.SetAccountActive(context.Background(), 1, constants.RoleSuperAdmin, 99, false)
	assertAppErr(t, err, 404, "AUTH_USER_NOT_FOUND")
}

func TestSetAccountActive_NonSuperAdminLocksAdmin_Returns403(t *testing.T) {
	repo := &mockUserRepo{user: &entity.User{ID: 2, Role: constants.RoleSuperAdmin, IsActive: true}}
	svc := newTestAuthService(repo, nil)
	err := svc.SetAccountActive(context.Background(), 1, constants.RoleDeptAdmin, 2, false)
	assertAppErr(t, err, 403, "AUTH_FORBIDDEN_ROLE")
}

func TestSetAccountActive_HappyPath_SetsActive(t *testing.T) {
	repo := &mockUserRepo{user: &entity.User{ID: 5, Role: constants.RolePatient, IsActive: true}}
	svc := newTestAuthService(repo, nil)
	err := svc.SetAccountActive(context.Background(), 1, constants.RoleSuperAdmin, 5, false)
	if err != nil {
		t.Fatalf("期望 nil，实际 %v", err)
	}
	if repo.gotActiveID != 5 || repo.gotActive != false {
		t.Errorf("期望 SetActive(5, false)，实际 (%d, %v)", repo.gotActiveID, repo.gotActive)
	}
}

func TestSetAccountActive_RepoDown_PropagatesError(t *testing.T) {
	repo := &mockUserRepo{user: &entity.User{ID: 5, Role: constants.RolePatient, IsActive: true}, setActiveErr: errors.New("db down")}
	svc := newTestAuthService(repo, nil)
	err := svc.SetAccountActive(context.Background(), 1, constants.RoleSuperAdmin, 5, false)
	if err == nil {
		t.Fatal("期望 error，实际 nil")
	}
}

// ============================================================================
// 测试辅助
// ============================================================================

func newTestAuthService(repo *mockUserRepo, rdb *mockRedis) *AuthService {
	if rdb == nil {
		rdb = newMockRedis()
	}
	// ponytail: 直接构造 AuthService 字段——测试同包访问，跳过 TokenIssuer/Authenticator。
	return &AuthService{
		repo:   repo,
		rdb:    rdb,
		issuer: nil,
		auth:   nil,
		cfg:    testConfig(),
	}
}

func testConfig() *config.Config {
	return &config.Config{
		Argon2: config.Argon2Config{
			Memory:      64,
			Iterations:  1,
			Parallelism: 1,
			SaltLength:  8,
			KeyLength:   16,
		},
	}
}

func assertAppErr(t *testing.T, err error, wantHTTP int, wantCode string) {
	t.Helper()
	if err == nil {
		t.Fatalf("期望 AppError，实际 nil")
	}
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("期望 *AppError，实际 %T: %v", err, err)
	}
	if appErr.HTTP != wantHTTP {
		t.Errorf("期望 HTTP=%d，实际 %d", wantHTTP, appErr.HTTP)
	}
	if appErr.Code != wantCode {
		t.Errorf("期望 Code=%s，实际 %s", wantCode, appErr.Code)
	}
}
