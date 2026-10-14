// auth_service_coverage_test.go — 补齐 AuthService 登录/注册/邀请码/账户管理等路径的覆盖。
// 复用 auth_service_test.go 中的 mockUserRepo / mockRedis / testConfig / assertAppErr。
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"health-nexus/internal/config"
	"health-nexus/internal/domain/auth/entity"
	"health-nexus/internal/middleware"
	"health-nexus/internal/platform/crypto"
	"health-nexus/internal/shared/constants"
	"health-nexus/internal/shared/contextkeys"
)

// ---- mock InviteRepo ----

type mockInviteRepo struct {
	onCreate        func(ic *entity.InviteCode) error
	createErr       error
	createCalls     int
	consumedUser    *entity.User
	consumeErr      error
	listCodes       []*entity.InviteCode
	listTotal       int64
	listErr         error
	lastConsumeCode string
	lastConsumeRole string
}

func (m *mockInviteRepo) Create(_ context.Context, ic *entity.InviteCode) error {
	m.createCalls++
	if m.onCreate != nil {
		if err := m.onCreate(ic); err != nil {
			return err
		}
	}
	if m.createErr != nil {
		return m.createErr
	}
	ic.ID = int64(m.createCalls)
	return nil
}

func (m *mockInviteRepo) List(_ context.Context, _, _ int) ([]*entity.InviteCode, int64, error) {
	return m.listCodes, m.listTotal, m.listErr
}

func (m *mockInviteRepo) ConsumeForRegistration(
	_ context.Context, code, username, passwordHash, role string,
) (*entity.User, error) {
	m.lastConsumeCode, m.lastConsumeRole = code, role
	if m.consumeErr != nil {
		return nil, m.consumeErr
	}
	if m.consumedUser != nil {
		return m.consumedUser, nil
	}
	return &entity.User{ID: 7, Username: username, Role: role, PasswordHash: passwordHash}, nil
}

// newCoverageAuthService 构造带真实 TokenIssuer/Authenticator 的 AuthService。
func newCoverageAuthService(
	t *testing.T, repo *mockUserRepo, invite *mockInviteRepo, rdb *mockRedis,
) *AuthService {
	t.Helper()
	jwtCfg := config.JWTConfig{
		Secret: "test-secret-at-least-32-bytes-long!!", Issuer: "test",
		AccessTTL: time.Minute, RefreshTTL: time.Hour,
	}
	issuer, err := NewTokenIssuer(jwtCfg)
	if err != nil {
		t.Fatalf("构造 TokenIssuer: %v", err)
	}
	auth, err := middleware.NewAuthenticator(jwtCfg, nil)
	if err != nil {
		t.Fatalf("构造 Authenticator: %v", err)
	}
	if rdb == nil {
		rdb = newMockRedis()
	}
	return &AuthService{repo: repo, invite: invite, issuer: issuer, auth: auth, rdb: rdb, cfg: testConfig()}
}

func hashForTest(t *testing.T, pw string) string {
	t.Helper()
	h, err := crypto.HashPassword(pw, testConfig().Argon2)
	if err != nil {
		t.Fatalf("哈希密码: %v", err)
	}
	return h
}

// ============ UnifiedLogin ============

func TestUnifiedLogin(t *testing.T) {
	hash := hashForTest(t, "Passw0rd123")

	t.Run("成功", func(t *testing.T) {
		repo := &mockUserRepo{user: &entity.User{ID: 1, Username: "alice", Role: constants.RolePatient, PasswordHash: hash, IsActive: true}}
		svc := newCoverageAuthService(t, repo, nil, nil)
		resp, err := svc.UnifiedLogin(context.Background(), "alice", "Passw0rd123")
		if err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
		if resp.Access == "" || resp.Refresh == "" || resp.User.ID != 1 {
			t.Fatalf("响应不完整: %+v", resp)
		}
	})

	t.Run("用户不存在返回401", func(t *testing.T) {
		svc := newCoverageAuthService(t, &mockUserRepo{user: nil}, nil, nil)
		_, err := svc.UnifiedLogin(context.Background(), "ghost", "x")
		assertAppErr(t, err, 401, "AUTH_INVALID_CREDENTIALS")
	})

	t.Run("密码错误返回401", func(t *testing.T) {
		repo := &mockUserRepo{user: &entity.User{ID: 1, PasswordHash: hash, IsActive: true}}
		svc := newCoverageAuthService(t, repo, nil, nil)
		_, err := svc.UnifiedLogin(context.Background(), "alice", "wrong-pass")
		assertAppErr(t, err, 401, "AUTH_INVALID_CREDENTIALS")
	})

	t.Run("账户锁定返回423", func(t *testing.T) {
		repo := &mockUserRepo{user: &entity.User{ID: 1, PasswordHash: hash, IsActive: false}}
		svc := newCoverageAuthService(t, repo, nil, nil)
		_, err := svc.UnifiedLogin(context.Background(), "alice", "Passw0rd123")
		assertAppErr(t, err, 423, "AUTH_ACCOUNT_LOCKED")
	})

	t.Run("仓储错误冒泡", func(t *testing.T) {
		svc := newCoverageAuthService(t, &mockUserRepo{getErr: errors.New("db down")}, nil, nil)
		if _, err := svc.UnifiedLogin(context.Background(), "alice", "x"); err == nil {
			t.Fatal("期望错误，实际 nil")
		}
	})
}

// ============ Register ============

func TestRegister(t *testing.T) {
	t.Run("用户名非法", func(t *testing.T) {
		svc := newCoverageAuthService(t, &mockUserRepo{}, &mockInviteRepo{}, nil)
		_, err := svc.Register(context.Background(), "a", "Passw0rd123", "123456")
		assertAppErr(t, err, 422, "AUTH_USERNAME_INVALID")
	})
	t.Run("密码过弱", func(t *testing.T) {
		svc := newCoverageAuthService(t, &mockUserRepo{}, &mockInviteRepo{}, nil)
		_, err := svc.Register(context.Background(), "bob", "short", "123456")
		assertAppErr(t, err, 422, "AUTH_PASSWORD_WEAK")
	})
	t.Run("缺邀请码", func(t *testing.T) {
		svc := newCoverageAuthService(t, &mockUserRepo{}, &mockInviteRepo{}, nil)
		_, err := svc.Register(context.Background(), "bob", "Passw0rd123", "")
		assertAppErr(t, err, 422, "VALIDATION_MISSING")
	})
	t.Run("成功", func(t *testing.T) {
		invite := &mockInviteRepo{consumedUser: &entity.User{ID: 9, Username: "bob", Role: constants.RolePatient}}
		svc := newCoverageAuthService(t, &mockUserRepo{}, invite, nil)
		resp, err := svc.Register(context.Background(), "bob", "Passw0rd123", "123456")
		if err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
		if resp.User.ID != 9 || invite.lastConsumeRole != constants.RolePatient {
			t.Fatalf("注册结果不符: %+v role=%s", resp, invite.lastConsumeRole)
		}
	})
	t.Run("邀请码无效", func(t *testing.T) {
		invite := &mockInviteRepo{consumeErr: entity.ErrInviteInvalid}
		svc := newCoverageAuthService(t, &mockUserRepo{}, invite, nil)
		_, err := svc.Register(context.Background(), "bob", "Passw0rd123", "000000")
		assertAppErr(t, err, 422, "AUTH_INVALID_INVITE")
	})
	t.Run("用户名已存在", func(t *testing.T) {
		invite := &mockInviteRepo{consumeErr: &pgconn.PgError{Code: "23505"}}
		svc := newCoverageAuthService(t, &mockUserRepo{}, invite, nil)
		_, err := svc.Register(context.Background(), "bob", "Passw0rd123", "000000")
		assertAppErr(t, err, 409, "AUTH_USERNAME_EXISTS")
	})
}

// ============ Logout ============

func TestLogout(t *testing.T) {
	svc := newCoverageAuthService(t, &mockUserRepo{}, nil, nil)
	_, refresh, err := svc.issuer.Issue(context.Background(), 5, constants.RolePatient, 0, 0)
	if err != nil {
		t.Fatalf("签发 refresh: %v", err)
	}
	ctx := context.WithValue(context.Background(), contextkeys.UserID, int64(5))

	if err := svc.Logout(ctx, refresh); err != nil {
		t.Fatalf("首次登出期望 nil，实际 %v", err)
	}
	// 同一 token 再次登出 → 已被 SETNX 占用 → 401。
	assertAppErr(t, svc.Logout(ctx, refresh), 401, "AUTH_INVALID_REFRESH")

	// 未认证 ctx → 401。
	assertAppErr(t, svc.Logout(context.Background(), refresh), 401, "AUTH_UNAUTHORIZED")

	// user_id 不匹配 → 403。
	_, refresh2, _ := svc.issuer.Issue(context.Background(), 6, constants.RolePatient, 0, 0)
	ctxMismatch := context.WithValue(context.Background(), contextkeys.UserID, int64(5))
	assertAppErr(t, svc.Logout(ctxMismatch, refresh2), 403, "AUTH_TOKEN_MISMATCH")

	// 非法 token → 401。
	assertAppErr(t, svc.Logout(ctx, "not-a-token"), 401, "AUTH_INVALID_REFRESH")
}

// ============ 邀请码 ============

func TestCreateInviteCodes(t *testing.T) {
	t.Run("非管理员403", func(t *testing.T) {
		svc := newCoverageAuthService(t, &mockUserRepo{}, &mockInviteRepo{}, nil)
		_, err := svc.CreateInviteCodes(context.Background(), 1, constants.RoleDoctor, 1)
		assertAppErr(t, err, 403, "AUTH_NOT_ADMIN")
	})
	t.Run("数量收口且码为6位数字", func(t *testing.T) {
		svc := newCoverageAuthService(t, &mockUserRepo{}, &mockInviteRepo{}, nil)
		dtos, err := svc.CreateInviteCodes(context.Background(), 1, constants.RoleSuperAdmin, 0)
		if err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
		if len(dtos) != 1 || len(dtos[0].Code) != 6 {
			t.Fatalf("期望 1 个 6 位码，实际 %+v", dtos)
		}
	})
	t.Run("撞码后重试", func(t *testing.T) {
		n := 0
		invite := &mockInviteRepo{onCreate: func(_ *entity.InviteCode) error {
			n++
			if n == 1 {
				return &pgconn.PgError{Code: "23505"}
			}
			return nil
		}}
		svc := newCoverageAuthService(t, &mockUserRepo{}, invite, nil)
		dtos, err := svc.CreateInviteCodes(context.Background(), 1, constants.RoleSuperAdmin, 1)
		if err != nil || len(dtos) != 1 || invite.createCalls != 2 {
			t.Fatalf("期望重试后成功，实际 err=%v dtos=%d calls=%d", err, len(dtos), invite.createCalls)
		}
	})
	t.Run("仓储错误冒泡", func(t *testing.T) {
		invite := &mockInviteRepo{createErr: errors.New("db down")}
		svc := newCoverageAuthService(t, &mockUserRepo{}, invite, nil)
		if _, err := svc.CreateInviteCodes(context.Background(), 1, constants.RoleSuperAdmin, 1); err == nil {
			t.Fatal("期望错误，实际 nil")
		}
	})
}

func TestListInviteCodes(t *testing.T) {
	t.Run("非管理员403", func(t *testing.T) {
		svc := newCoverageAuthService(t, &mockUserRepo{}, &mockInviteRepo{}, nil)
		_, _, err := svc.ListInviteCodes(context.Background(), constants.RoleDoctor, 20, 0)
		assertAppErr(t, err, 403, "AUTH_NOT_ADMIN")
	})
	t.Run("成功映射DTO", func(t *testing.T) {
		invite := &mockInviteRepo{listCodes: []*entity.InviteCode{{ID: 1, Code: "123456", Role: constants.RolePatient}}, listTotal: 1}
		svc := newCoverageAuthService(t, &mockUserRepo{}, invite, nil)
		dtos, total, err := svc.ListInviteCodes(context.Background(), constants.RoleSuperAdmin, 20, 0)
		if err != nil || total != 1 || len(dtos) != 1 || dtos[0].Code != "123456" {
			t.Fatalf("结果不符: dtos=%+v total=%d err=%v", dtos, total, err)
		}
	})
	t.Run("仓储错误冒泡", func(t *testing.T) {
		invite := &mockInviteRepo{listErr: errors.New("db down")}
		svc := newCoverageAuthService(t, &mockUserRepo{}, invite, nil)
		if _, _, err := svc.ListInviteCodes(context.Background(), constants.RoleSuperAdmin, 20, 0); err == nil {
			t.Fatal("期望错误，实际 nil")
		}
	})
}

func TestGenerateInviteCode(t *testing.T) {
	for i := 0; i < 20; i++ {
		code := generateInviteCode()
		if len(code) != 6 {
			t.Fatalf("期望 6 位，实际 %q", code)
		}
		for _, c := range code {
			if c < '0' || c > '9' {
				t.Fatalf("期望纯数字，实际 %q", code)
			}
		}
	}
}

// ============ 账户管理 ============

func TestUpdateAccountDept(t *testing.T) {
	t.Run("deptID非法", func(t *testing.T) {
		svc := newCoverageAuthService(t, &mockUserRepo{}, nil, nil)
		_, err := svc.UpdateAccountDept(context.Background(), constants.RoleSuperAdmin, 1, 1, 0)
		assertAppErr(t, err, 422, "AUTH_DEPT_REQUIRED")
	})
	t.Run("目标不存在", func(t *testing.T) {
		svc := newCoverageAuthService(t, &mockUserRepo{user: nil}, nil, nil)
		_, err := svc.UpdateAccountDept(context.Background(), constants.RoleSuperAdmin, 1, 1, 2)
		assertAppErr(t, err, 404, "AUTH_USER_NOT_FOUND")
	})
	t.Run("科室管理员跨科室403", func(t *testing.T) {
		repo := &mockUserRepo{user: &entity.User{ID: 2, PrimaryDeptID: 9, IsActive: true}}
		svc := newCoverageAuthService(t, repo, nil, nil)
		_, err := svc.UpdateAccountDept(context.Background(), constants.RoleDeptAdmin, 1, 2, 1)
		assertAppErr(t, err, 403, "AUTH_FORBIDDEN_DEPT")
	})
	t.Run("超管成功", func(t *testing.T) {
		repo := &mockUserRepo{user: &entity.User{ID: 2, Role: constants.RoleDoctor, PrimaryDeptID: 3, IsActive: true}}
		svc := newCoverageAuthService(t, repo, nil, nil)
		dto, err := svc.UpdateAccountDept(context.Background(), constants.RoleSuperAdmin, 1, 2, 4)
		if err != nil || dto.ID != 2 {
			t.Fatalf("期望成功，实际 dto=%+v err=%v", dto, err)
		}
	})
}

func TestSoftDeleteUser(t *testing.T) {
	t.Run("非超管403", func(t *testing.T) {
		svc := newCoverageAuthService(t, &mockUserRepo{}, nil, nil)
		err := svc.SoftDeleteUser(context.Background(), 1, constants.RoleDeptAdmin, 2)
		assertAppErr(t, err, 403, "AUTH_FORBIDDEN_ROLE")
	})
	t.Run("删除自己409", func(t *testing.T) {
		svc := newCoverageAuthService(t, &mockUserRepo{}, nil, nil)
		err := svc.SoftDeleteUser(context.Background(), 1, constants.RoleSuperAdmin, 1)
		assertAppErr(t, err, 409, "AUTH_SELF_DELETE")
	})
	t.Run("目标不存在404", func(t *testing.T) {
		svc := newCoverageAuthService(t, &mockUserRepo{user: nil}, nil, nil)
		err := svc.SoftDeleteUser(context.Background(), 1, constants.RoleSuperAdmin, 2)
		assertAppErr(t, err, 404, "AUTH_USER_NOT_FOUND")
	})
	t.Run("成功", func(t *testing.T) {
		repo := &mockUserRepo{user: &entity.User{ID: 2, IsActive: true}}
		svc := newCoverageAuthService(t, repo, nil, nil)
		if err := svc.SoftDeleteUser(context.Background(), 1, constants.RoleSuperAdmin, 2); err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
		if repo.gotDeleteID != 2 {
			t.Fatalf("期望删除 2，实际 %d", repo.gotDeleteID)
		}
	})
}

func TestRestoreUser(t *testing.T) {
	t.Run("非超管403", func(t *testing.T) {
		svc := newCoverageAuthService(t, &mockUserRepo{}, nil, nil)
		_, err := svc.RestoreUser(context.Background(), 1, constants.RoleDeptAdmin, 2)
		assertAppErr(t, err, 403, "AUTH_FORBIDDEN_ROLE")
	})
	t.Run("目标不存在404", func(t *testing.T) {
		svc := newCoverageAuthService(t, &mockUserRepo{user: nil}, nil, nil)
		_, err := svc.RestoreUser(context.Background(), 1, constants.RoleSuperAdmin, 2)
		assertAppErr(t, err, 404, "AUTH_USER_NOT_FOUND")
	})
	t.Run("未删除409", func(t *testing.T) {
		repo := &mockUserRepo{user: &entity.User{ID: 2, IsDeleted: false, IsActive: true}}
		svc := newCoverageAuthService(t, repo, nil, nil)
		_, err := svc.RestoreUser(context.Background(), 1, constants.RoleSuperAdmin, 2)
		assertAppErr(t, err, 409, "AUTH_NOT_DELETED")
	})
	t.Run("成功", func(t *testing.T) {
		repo := &mockUserRepo{user: &entity.User{ID: 2, IsDeleted: true, IsActive: false}}
		svc := newCoverageAuthService(t, repo, nil, nil)
		if _, err := svc.RestoreUser(context.Background(), 1, constants.RoleSuperAdmin, 2); err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
	})
}

func TestUpdateAccountRole(t *testing.T) {
	t.Run("非超管403", func(t *testing.T) {
		svc := newCoverageAuthService(t, &mockUserRepo{}, nil, nil)
		_, err := svc.UpdateAccountRole(context.Background(), 1, constants.RoleDeptAdmin, 2, constants.RoleDoctor)
		assertAppErr(t, err, 403, "AUTH_FORBIDDEN_ROLE")
	})
	t.Run("角色非法400", func(t *testing.T) {
		svc := newCoverageAuthService(t, &mockUserRepo{}, nil, nil)
		_, err := svc.UpdateAccountRole(context.Background(), 1, constants.RoleSuperAdmin, 2, "WIZARD")
		assertAppErr(t, err, 400, "AUTH_ROLE_INVALID")
	})
	t.Run("改自己409", func(t *testing.T) {
		svc := newCoverageAuthService(t, &mockUserRepo{}, nil, nil)
		_, err := svc.UpdateAccountRole(context.Background(), 1, constants.RoleSuperAdmin, 1, constants.RoleDoctor)
		assertAppErr(t, err, 409, "AUTH_SELF_ROLE")
	})
	t.Run("目标不存在404", func(t *testing.T) {
		svc := newCoverageAuthService(t, &mockUserRepo{user: nil}, nil, nil)
		_, err := svc.UpdateAccountRole(context.Background(), 1, constants.RoleSuperAdmin, 2, constants.RoleDoctor)
		assertAppErr(t, err, 404, "AUTH_USER_NOT_FOUND")
	})
	t.Run("成功", func(t *testing.T) {
		repo := &mockUserRepo{user: &entity.User{ID: 2, Role: constants.RoleNurse, IsActive: true}}
		svc := newCoverageAuthService(t, repo, nil, nil)
		if _, err := svc.UpdateAccountRole(context.Background(), 1, constants.RoleSuperAdmin, 2, constants.RoleDoctor); err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
		if repo.gotRole != constants.RoleDoctor {
			t.Fatalf("期望角色更新为 doctor，实际 %s", repo.gotRole)
		}
	})
}

func TestResetUserPassword(t *testing.T) {
	t.Run("非超管403", func(t *testing.T) {
		svc := newCoverageAuthService(t, &mockUserRepo{}, nil, nil)
		err := svc.ResetUserPassword(context.Background(), 1, constants.RoleDeptAdmin, 2, "Passw0rd123")
		assertAppErr(t, err, 403, "AUTH_FORBIDDEN_ROLE")
	})
	t.Run("重置自己409", func(t *testing.T) {
		svc := newCoverageAuthService(t, &mockUserRepo{}, nil, nil)
		err := svc.ResetUserPassword(context.Background(), 1, constants.RoleSuperAdmin, 1, "Passw0rd123")
		assertAppErr(t, err, 409, "AUTH_SELF_RESET")
	})
	t.Run("密码过弱422", func(t *testing.T) {
		svc := newCoverageAuthService(t, &mockUserRepo{}, nil, nil)
		err := svc.ResetUserPassword(context.Background(), 1, constants.RoleSuperAdmin, 2, "weak")
		assertAppErr(t, err, 422, "AUTH_PASSWORD_WEAK")
	})
	t.Run("目标不存在404", func(t *testing.T) {
		svc := newCoverageAuthService(t, &mockUserRepo{user: nil}, nil, nil)
		err := svc.ResetUserPassword(context.Background(), 1, constants.RoleSuperAdmin, 2, "Passw0rd123")
		assertAppErr(t, err, 404, "AUTH_USER_NOT_FOUND")
	})
	t.Run("成功", func(t *testing.T) {
		repo := &mockUserRepo{user: &entity.User{ID: 2, IsActive: true}}
		svc := newCoverageAuthService(t, repo, nil, nil)
		if err := svc.ResetUserPassword(context.Background(), 1, constants.RoleSuperAdmin, 2, "Passw0rd123"); err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
		if repo.gotUpdateID != 2 || repo.gotUpdatePwd == "" {
			t.Fatalf("期望写入新哈希，实际 id=%d hash=%q", repo.gotUpdateID, repo.gotUpdatePwd)
		}
	})
}
