package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"unicode"

	"github.com/redis/go-redis/v9"

	"health-nexus/internal/config"
	"health-nexus/internal/domain/auth/entity"
	"health-nexus/internal/middleware"
	"health-nexus/internal/platform/crypto"
	"health-nexus/internal/platform/postgres"
	"health-nexus/internal/shared/constants"
	"health-nexus/internal/shared/contextkeys"
	apperrors "health-nexus/internal/shared/errors"
)

// 用户名与密码约束。
const (
	passwordMinLen         = 8
	usernameMinLen         = 3
	usernameMaxLen         = 64
	phoneMaxLen            = 20
	emergencyContactMaxLen = 64
	emergencyPhoneMaxLen   = 20
)

// blacklistKeyPrefix logout 写入 Redis 的 refresh token 黑名单 key 前缀。
const blacklistKeyPrefix = "blacklist:refresh:"

// passwordResetKeyPrefix 密码重置 token 在 Redis 中的 key 前缀。
// value 为 user_id 字符串，TTL 由 passwordResetTTL 控制。
const passwordResetKeyPrefix = "password_reset:"

// passwordResetTTL 密码重置 token 有效期（15 分钟）。
const passwordResetTTL = 15 * time.Minute

// UserRepo 定义 AuthService 所需的用户数据访问能力（消费者接口）。
// 实现在 internal/domain/auth/repository 包，由 di 装配时注入。
type UserRepo interface {
	GetByUsername(ctx context.Context, username string) (*entity.User, error)
	GetByID(ctx context.Context, id int64) (*entity.User, error)
	Create(ctx context.Context, username, passwordHash, role string) (*entity.User, error)
	UpdatePasswordHash(ctx context.Context, userID int64, passwordHash string) error
	UpdateProfile(ctx context.Context, userID int64, phone string, dateOfBirth *time.Time,
		gender, emergencyContact, emergencyPhone string) error
	SetActive(ctx context.Context, userID int64, active bool) error
	SoftDelete(ctx context.Context, userID int64) error
	List(ctx context.Context, limit, offset int) ([]*entity.User, int64, error)
	ListByDept(ctx context.Context, deptID, limit, offset int64) ([]*entity.User, int64, error)
}

// LoginResponse 统一登录响应体。
type LoginResponse struct {
	Access  string   `json:"access"`
	Refresh string   `json:"refresh"`
	User    UserInfo `json:"user"`
}

// UserInfo 统一登录响应中的用户信息。
type UserInfo struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	DeptID   int64  `json:"dept_id"`
}

// RegisterResponse 患者注册响应体。
type RegisterResponse struct {
	Access  string           `json:"access"`
	Refresh string           `json:"refresh"`
	User    RegisterUserInfo `json:"user"`
}

// RegisterUserInfo 注册响应中的用户信息。
type RegisterUserInfo struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// ProfileDTO 已登录用户的个人资料（GET/PATCH /api/auth/profile）。
type ProfileDTO struct {
	ID               int64      `json:"id"`
	Username         string     `json:"username"`
	Role             string     `json:"role"`
	Phone            string     `json:"phone"`
	DateOfBirth      *time.Time `json:"date_of_birth"`
	Gender           string     `json:"gender"`
	EmergencyContact string     `json:"emergency_contact"`
	EmergencyPhone   string     `json:"emergency_phone"`
	DeptID           int64      `json:"dept_id"`
}

// AccountDTO 管理员视角的账户信息（账户管理端点）。
// 不含 password_hash--敏感字段绝不出 service 层。
type AccountDTO struct {
	ID               int64      `json:"id"`
	Username         string     `json:"username"`
	Role             string     `json:"role"`
	Phone            string     `json:"phone"`
	DateOfBirth      *time.Time `json:"date_of_birth"`
	Gender           string     `json:"gender"`
	EmergencyContact string     `json:"emergency_contact"`
	EmergencyPhone   string     `json:"emergency_phone"`
	IsActive         bool       `json:"is_active"`
	IsDeleted        bool       `json:"is_deleted"`
	CreatedAt        time.Time  `json:"created_at"`
}

// RedisClient 定义 AuthService 所需的 Redis 操作（消费者接口）。
// *redis.Client 隐式满足此接口，由 di 装配时注入。
type RedisClient interface {
	Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd
	Get(ctx context.Context, key string) *redis.StringCmd
	SetNX(ctx context.Context, key string, value any, expiration time.Duration) *redis.BoolCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
}

// AuthService 实现登录/注册/刷新/登出/密码重置业务逻辑。
type AuthService struct {
	repo   UserRepo
	issuer *TokenIssuer
	auth   *middleware.Authenticator
	rdb    RedisClient
	cfg    *config.Config
}

// NewAuthService 构造 AuthService。
func NewAuthService(
	repo UserRepo,
	issuer *TokenIssuer,
	auth *middleware.Authenticator,
	rdb RedisClient,
	cfg *config.Config,
) *AuthService {
	return &AuthService{repo: repo, issuer: issuer, auth: auth, rdb: rdb, cfg: cfg}
}

// UnifiedLogin 统一登录。不校验角色，登录后由前端根据 user.role 跳转对应端。
func (s *AuthService) UnifiedLogin(ctx context.Context, username, password string) (*LoginResponse, error) {
	u, access, refresh, err := s.login(ctx, username, password, false, true)
	if err != nil {
		return nil, err
	}
	return &LoginResponse{
		Access:  access,
		Refresh: refresh,
		User:    UserInfo{ID: u.ID, Username: u.Username, Role: u.Role, DeptID: u.PrimaryDeptID},
	}, nil
}

// login 共用登录校验与 token 签发逻辑。skipRoleCheck=true 跳过角色校验（统一登录）。
// 返回 (user, access, refresh, err)，由 UnifiedLogin 包装为响应 DTO。
// 关键路径均记录业务日志（conventions.md §3）：PII 保护--不记录 username 明文，仅 user_id/role。
func (s *AuthService) login(
	ctx context.Context, username, password string, requireStaff bool, skipRoleCheck bool,
) (u *entity.User, access, refresh string, err error) {
	endpoint := "patient"
	if requireStaff {
		endpoint = "staff"
	}
	if skipRoleCheck {
		endpoint = "unified"
	}
	u, err = s.repo.GetByUsername(ctx, username)
	if err != nil {
		return nil, "", "", fmt.Errorf("get user: %w", err)
	}
	// 用户不存在与密码错误统一返回 401，避免泄露用户是否存在；日志不记 username。
	if u == nil {
		slog.WarnContext(ctx, "login failed: user not found", "endpoint", endpoint)
		return nil, "", "", apperrors.Unauthorized("AUTH_INVALID_CREDENTIALS", "用户名或密码错误")
	}
	if u.IsLocked() {
		slog.WarnContext(ctx, "login blocked: account locked", "user_id", u.ID)
		return nil, "", "", apperrors.Locked("AUTH_ACCOUNT_LOCKED", "账户已锁定，请联系管理员")
	}
	if err := crypto.ComparePasswordAndPassword(u.PasswordHash, password); err != nil {
		slog.WarnContext(ctx, "login failed: invalid credentials", "user_id", u.ID)
		return nil, "", "", apperrors.Unauthorized("AUTH_INVALID_CREDENTIALS", "用户名或密码错误")
	}
	if !skipRoleCheck {
		if requireStaff {
			if !constants.IsStaff(u.Role) {
				slog.WarnContext(ctx, "cross-end login denied", "user_id", u.ID, "role", u.Role, "endpoint", "staff")
				return nil, "", "", apperrors.Forbidden("AUTH_NOT_STAFF", "非医护角色禁止访问医护端")
			}
		} else {
			if u.Role != constants.RolePatient {
				slog.WarnContext(ctx, "cross-end login denied", "user_id", u.ID, "role", u.Role, "endpoint", "patient")
				return nil, "", "", apperrors.Forbidden("AUTH_NOT_PATIENT", "非患者角色禁止访问患者端")
			}
		}
	}
	access, refresh, err = s.issuer.Issue(ctx, u.ID, u.Role, u.PrimaryDeptID)
	if err != nil {
		return nil, "", "", fmt.Errorf("issue token: %w", err)
	}
	slog.InfoContext(ctx, "login success", "user_id", u.ID, "role", u.Role)
	return u, access, refresh, nil
}

// Register 患者注册。校验用户名格式、密码强度、用户名未占用，创建 PATIENT 用户并签发 token。
func (s *AuthService) Register(ctx context.Context, username, password string) (*RegisterResponse, error) {
	if err := validateUsername(username); err != nil {
		return nil, err
	}
	if err := validatePasswordStrength(password); err != nil {
		return nil, err
	}
	hash, err := crypto.HashPassword(password, s.cfg.Argon2)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	u, err := s.repo.Create(ctx, username, hash, constants.RolePatient)
	if err != nil {
		if postgres.IsUniqueViolation(err) {
			return nil, apperrors.Conflict("AUTH_USERNAME_EXISTS", "用户名已存在")
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	access, refresh, err := s.issuer.Issue(ctx, u.ID, u.Role, 0)
	if err != nil {
		return nil, fmt.Errorf("issue token: %w", err)
	}
	return &RegisterResponse{
		Access:  access,
		Refresh: refresh,
		User:    RegisterUserInfo{ID: u.ID, Username: u.Username, Role: u.Role},
	}, nil
}

// Refresh 校验 refresh token 签名、类型，原子占用后签发新的 access+refresh（轮换）。
// 轮换：用 SETNX 原子占用旧 refresh token，消除"检查->签发->黑名单"之间的竞态窗口。
// 重新从 DB 加载用户--不信任 token 中的 role/dept/is_active，防止用户被降级/锁定后仍凭旧 token 获取高权限。
//
// 顺序：Parse 验签 -> token_type 校验 -> DB 重载用户 + 锁定校验 -> SETNX 原子占用 -> 签发新 token。
// DB 校验前移到 tryClaim 之前--避免 DB 故障时 token 已被 SETNX 消费但无法签发新 token，
// 导致用户被迫重新登录（R6-4 修复：原顺序 tryClaim 在 GetByID 之前，DB 抖动即丢 token）。
//
// ponytail: fail-closed 设计--DB 与 Redis 同时故障时用户无法刷新，这是安全优先于可用性的设计选择，折中。
// 上限：依赖双重可用性（DB + Redis）才能刷新；升级路径：Redis 故障时可降级为"DB 单点校验 + 写入延迟黑名单"，
// 但会重新引入 TOCTOU 竞态，目前不做（运维侧需保证 Redis 高可用）。
func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (access, refresh string, err error) {
	claims, err := s.auth.Parse(refreshToken)
	if err != nil {
		return "", "", apperrors.Unauthorized("AUTH_INVALID_REFRESH", "无效的 refresh token")
	}
	if claims.TokenType != TokenTypeRefresh {
		return "", "", apperrors.Unauthorized("AUTH_INVALID_REFRESH", "token 类型错误")
	}
	// 先重新加载用户：校验账户仍然存在、未锁定、角色/科室为最新值。
	// 放在 tryClaim 之前--DB 故障时不会消费 refresh token，用户可重试而非被迫重新登录。
	u, err := s.repo.GetByID(ctx, claims.UserID)
	if err != nil {
		return "", "", fmt.Errorf("reload user: %w", err)
	}
	if u == nil {
		return "", "", apperrors.Unauthorized("AUTH_INVALID_REFRESH", "用户不存在")
	}
	if u.IsLocked() {
		return "", "", apperrors.Locked("AUTH_ACCOUNT_LOCKED", "账户已锁定，请联系管理员")
	}
	// 原子占用旧 token：SETNX 消除 isBlacklisted+blacklist 之间的 TOCTOU 竞态。
	// 并发 Refresh 携带同一 token 时，仅首个 SETNX 成功，其余直接拒绝--防止 token 复制。
	// fail-closed：Redis 故障时拒绝刷新，用户需重新登录（UnifiedLogin 仅依赖 PostgreSQL）。
	//   安全优先于可用性--Redis 不可用正是攻击者最可能利用 token 复制的窗口。
	claimed, err := s.tryClaim(ctx, refreshToken)
	if err != nil {
		slog.ErrorContext(ctx, "claim refresh token failed, refresh denied", "err", err)
		return "", "", apperrors.ServiceUnavailable("AUTH_REFRESH_UNAVAILABLE", "刷新服务暂不可用，请稍后重试或重新登录")
	}
	if !claimed {
		return "", "", apperrors.Unauthorized("AUTH_INVALID_REFRESH", "refresh token 已失效")
	}
	access, refresh, err = s.issuer.Issue(ctx, u.ID, u.Role, u.PrimaryDeptID)
	if err != nil {
		return "", "", fmt.Errorf("issue token: %w", err)
	}
	// 旧 refresh token 已由 tryClaim 原子占用（SETNX），无需再次 blacklist。
	return access, refresh, nil
}

// Logout 将 refresh token 加入 Redis 黑名单，TTL = refresh token TTL。
// 已失效（已登出 / 已轮换）的 refresh token 再调用 logout 返回 401（spec T-AUTH-10）。
//
// 校验链：签名 -> token_type=refresh -> user_id 与 access token 一致 -> tryClaim 原子占用。
// 用 tryClaim 而非 blacklist：SETNX 失败表示已被占用（先前 logout 或 refresh 轮换），
// 此时返回 401 与规范一致，避免重复登出被静默忽略而前端无法识别 token 失效。
// user_id 一致性校验防止跨用户登出 DoS（攻击者用自己 access token + 他人 refresh token）。
func (s *AuthService) Logout(ctx context.Context, refreshToken string) error {
	claims, err := s.auth.Parse(refreshToken)
	if err != nil {
		return apperrors.Unauthorized("AUTH_INVALID_REFRESH", "无效的 refresh token")
	}
	if claims.TokenType != TokenTypeRefresh {
		return apperrors.Unauthorized("AUTH_INVALID_REFRESH", "token 类型错误")
	}
	// 校验 refresh token 的 user_id 与 access token（JWTAuth 中间件注入 ctx）一致，
	// 防止已认证用户用他人 refresh token 执行跨用户登出 DoS。
	accessUserID, ok := ctx.Value(contextkeys.UserID).(int64)
	if !ok {
		return apperrors.Unauthorized("AUTH_UNAUTHORIZED", "未认证")
	}
	if claims.UserID != accessUserID {
		return apperrors.Forbidden("AUTH_TOKEN_MISMATCH", "refresh token 与当前用户不匹配")
	}
	// tryClaim 原子占用 refresh token：SETNX 成功=首次登出，失败=已登出/已轮换->401。
	// 与 Refresh 共享 blacklistKey，登出后该 token 在 Refresh 路径同样会被拒绝，状态一致。
	// Redis 故障 fail-closed：返回 503 避免故障期间 logout 静默失败导致 token 仍可用。
	claimed, err := s.tryClaim(ctx, refreshToken)
	if err != nil {
		slog.ErrorContext(ctx, "logout tryClaim failed", "err", err)
		return apperrors.ServiceUnavailable("AUTH_BLACKLIST_UNAVAILABLE", "登出服务暂不可用，请稍后重试")
	}
	if !claimed {
		return apperrors.Unauthorized("AUTH_INVALID_REFRESH", "refresh token 已失效")
	}
	return nil
}

// RequestPasswordReset 生成密码重置 token 并存入 Redis（TTL 15 分钟）。
// 用户不存在时也返回 nil（安全设计：不泄露用户是否存在）。
// Redis 故障时返回 503。
func (s *AuthService) RequestPasswordReset(ctx context.Context, username string) error {
	u, err := s.repo.GetByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if u == nil {
		// 安全设计：不泄露用户是否存在，静默返回成功。
		slog.WarnContext(ctx, "password reset requested for non-existent user")
		return nil
	}
	if u.IsLocked() {
		slog.WarnContext(ctx, "password reset denied: account locked", "user_id", u.ID)
		return apperrors.Locked("AUTH_ACCOUNT_LOCKED", "账户已锁定，请联系管理员")
	}
	token, err := generateResetToken()
	if err != nil {
		return fmt.Errorf("generate reset token: %w", err)
	}
	key := passwordResetKeyPrefix + token
	if err := s.rdb.Set(ctx, key, fmt.Sprintf("%d", u.ID), passwordResetTTL).Err(); err != nil {
		slog.ErrorContext(ctx, "password reset redis set failed", "err", err)
		return apperrors.ServiceUnavailable("AUTH_RESET_UNAVAILABLE", "密码重置服务暂不可用，请稍后重试")
	}
	slog.InfoContext(ctx, "password reset token issued", "user_id", u.ID)
	return nil
}

// ConfirmPasswordReset 校验重置 token 并更新密码。
// token 无效或过期返回 400；密码强度不足返回 400。
// 成功后删除 Redis 中的 token（一次性使用）。
func (s *AuthService) ConfirmPasswordReset(ctx context.Context, token, newPassword string) error {
	if err := validatePasswordStrength(newPassword); err != nil {
		return err
	}
	key := passwordResetKeyPrefix + token
	userIDStr, err := s.rdb.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return apperrors.BadRequest("AUTH_RESET_TOKEN_INVALID", "重置链接无效或已过期")
		}
		slog.ErrorContext(ctx, "password reset redis get failed", "err", err)
		return apperrors.ServiceUnavailable("AUTH_RESET_UNAVAILABLE", "密码重置服务暂不可用，请稍后重试")
	}
	var userID int64
	if _, err := fmt.Sscanf(userIDStr, "%d", &userID); err != nil {
		return apperrors.BadRequest("AUTH_RESET_TOKEN_INVALID", "重置链接无效或已过期")
	}
	hash, err := crypto.HashPassword(newPassword, s.cfg.Argon2)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if err := s.repo.UpdatePasswordHash(ctx, userID, hash); err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	// 删除已使用的 token（一次性）。
	_ = s.rdb.Del(ctx, key).Err()
	slog.InfoContext(ctx, "password reset success", "user_id", userID)
	return nil
}

// ChangePassword 已登录用户修改自己的密码。
// 校验旧密码正确（防止凭被盗 access token 改密）、新密码强度，然后更新哈希。
// userID 来自 JWT 中间件注入的 ctx，不由请求体传入--避免越权改他人密码。
func (s *AuthService) ChangePassword(ctx context.Context, userID int64, oldPassword, newPassword string) error {
	u, err := s.repo.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if u == nil {
		return apperrors.NotFound("AUTH_USER_NOT_FOUND", "用户不存在")
	}
	if err := crypto.ComparePasswordAndPassword(u.PasswordHash, oldPassword); err != nil {
		slog.WarnContext(ctx, "change password failed: wrong old password", "user_id", userID)
		return apperrors.Unauthorized("AUTH_OLD_PASSWORD_WRONG", "原密码错误")
	}
	if err := validatePasswordStrength(newPassword); err != nil {
		return err
	}
	hash, err := crypto.HashPassword(newPassword, s.cfg.Argon2)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if err := s.repo.UpdatePasswordHash(ctx, userID, hash); err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	slog.InfoContext(ctx, "password changed", "user_id", userID)
	return nil
}

// GetProfile 读取已登录用户的个人资料。
func (s *AuthService) GetProfile(ctx context.Context, userID int64) (*ProfileDTO, error) {
	u, err := s.repo.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if u == nil {
		return nil, apperrors.NotFound("AUTH_USER_NOT_FOUND", "用户不存在")
	}
	return &ProfileDTO{
		ID: u.ID, Username: u.Username, Role: u.Role, Phone: u.Phone,
		DateOfBirth: u.DateOfBirth, Gender: u.Gender, EmergencyContact: u.EmergencyContact,
		EmergencyPhone: u.EmergencyPhone, DeptID: u.PrimaryDeptID,
	}, nil
}

// UpdateProfile 更新已登录用户的个人资料（phone/date_of_birth/gender/emergency_contact/emergency_phone）。
// 用户名为登录凭证不可自改（改用户名需管理员介入，避免凭证混乱）。
func (s *AuthService) UpdateProfile(ctx context.Context, userID int64, phone string, dateOfBirth *time.Time,
	gender, emergencyContact, emergencyPhone string) (*ProfileDTO, error) {
	if len(phone) > phoneMaxLen {
		return nil, apperrors.Validation("AUTH_PHONE_TOO_LONG", "手机号过长")
	}
	if !validGender(gender) {
		return nil, apperrors.Validation("AUTH_GENDER_INVALID", "性别无效")
	}
	if len(emergencyContact) > emergencyContactMaxLen {
		return nil, apperrors.Validation("AUTH_EMERGENCY_CONTACT_TOO_LONG", "紧急联系人过长")
	}
	if len(emergencyPhone) > emergencyPhoneMaxLen {
		return nil, apperrors.Validation("AUTH_EMERGENCY_PHONE_TOO_LONG", "紧急联系电话过长")
	}
	u, err := s.repo.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if u == nil {
		return nil, apperrors.NotFound("AUTH_USER_NOT_FOUND", "用户不存在")
	}
	if err := s.repo.UpdateProfile(ctx, userID, phone, dateOfBirth, gender,
		emergencyContact, emergencyPhone); err != nil {
		return nil, fmt.Errorf("update profile: %w", err)
	}
	return s.GetProfile(ctx, userID)
}

// ListAccounts 管理员分页查询账户列表。
// 数据隔离：非 SUPER_ADMIN 仅查看本科室账户（通过 user_departments JOIN 过滤）。
func (s *AuthService) ListAccounts(ctx context.Context, actorRole string,
	actorDeptID, page, pageSize int64) ([]AccountDTO, int64, error) {
	offset := (page - 1) * pageSize
	var (
		users []*entity.User
		total int64
		err   error
	)
	if actorRole == constants.RoleSuperAdmin {
		users, total, err = s.repo.List(ctx, int(pageSize), int(offset))
	} else {
		users, total, err = s.repo.ListByDept(ctx, actorDeptID, pageSize, offset)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	dtos := make([]AccountDTO, 0, len(users))
	for _, u := range users {
		dtos = append(dtos, accountDTO(u))
	}
	return dtos, total, nil
}

// CreateAccount 管理员创建账户。
// 权限收口：非 SUPER_ADMIN 不得创建管理员角色（SUPER_ADMIN/DEPT_ADMIN）账户，防止 DEPT_ADMIN 提权。
func (s *AuthService) CreateAccount(
	ctx context.Context, actorRole, username, password, role string,
) (*AccountDTO, error) {
	if !validRole(role) {
		return nil, apperrors.BadRequest("AUTH_ROLE_INVALID", "角色无效")
	}
	if constants.IsAdmin(role) && actorRole != constants.RoleSuperAdmin {
		return nil, apperrors.Forbidden("AUTH_FORBIDDEN_ROLE", "无权创建管理员账户")
	}
	if err := validateUsername(username); err != nil {
		return nil, err
	}
	if err := validatePasswordStrength(password); err != nil {
		return nil, err
	}
	hash, err := crypto.HashPassword(password, s.cfg.Argon2)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	u, err := s.repo.Create(ctx, username, hash, role)
	if err != nil {
		if postgres.IsUniqueViolation(err) {
			return nil, apperrors.Conflict("AUTH_USERNAME_EXISTS", "用户名已存在")
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	slog.InfoContext(ctx, "account created", "actor_role", actorRole, "user_id", u.ID, "role", role)
	dto := accountDTO(u)
	return &dto, nil
}

// SetAccountActive 管理员锁定（active=false）/解锁（active=true）账户。
// 安全约束：不可锁定自己；非 SUPER_ADMIN 不得操作管理员角色账户；
// DEPT_ADMIN 仅可操作本科室账户（防跨科室账户 DoS）。
func (s *AuthService) SetAccountActive(
	ctx context.Context, actorID int64, actorRole string, actorDeptID int64, targetID int64, active bool,
) error {
	if actorID == targetID {
		return apperrors.Conflict("AUTH_SELF_LOCK", "不能锁定或解锁自己的账户")
	}
	target, err := s.repo.GetByID(ctx, targetID)
	if err != nil {
		return fmt.Errorf("get target user: %w", err)
	}
	if target == nil {
		return apperrors.NotFound("AUTH_USER_NOT_FOUND", "用户不存在")
	}
	if constants.IsAdmin(target.Role) && actorRole != constants.RoleSuperAdmin {
		return apperrors.Forbidden("AUTH_FORBIDDEN_ROLE", "无权操作管理员账户")
	}
	if actorRole == constants.RoleDeptAdmin && target.PrimaryDeptID != actorDeptID {
		return apperrors.Forbidden("AUTH_FORBIDDEN_DEPT", "无权操作其他科室的账户")
	}
	if err := s.repo.SetActive(ctx, targetID, active); err != nil {
		return fmt.Errorf("set active: %w", err)
	}
	slog.InfoContext(ctx, "account active state changed", "actor_id", actorID, "target_id", targetID, "active", active)
	return nil
}

// SoftDeleteUser 超级管理员软删除用户。
// 安全约束：仅 SUPER_ADMIN 可执行；不可删除自己。
// 软删除同时锁定账户（is_active=false），被删除用户无法登录/刷新。
func (s *AuthService) SoftDeleteUser(ctx context.Context, actorID int64, actorRole string, targetID int64) error {
	if actorRole != constants.RoleSuperAdmin {
		return apperrors.Forbidden("AUTH_FORBIDDEN_ROLE", "仅超级管理员可删除用户")
	}
	if actorID == targetID {
		return apperrors.Conflict("AUTH_SELF_DELETE", "不能删除自己的账户")
	}
	target, err := s.repo.GetByID(ctx, targetID)
	if err != nil {
		return fmt.Errorf("get target user: %w", err)
	}
	if target == nil {
		return apperrors.NotFound("AUTH_USER_NOT_FOUND", "用户不存在")
	}
	if err := s.repo.SoftDelete(ctx, targetID); err != nil {
		return fmt.Errorf("soft delete user: %w", err)
	}
	slog.InfoContext(ctx, "user soft deleted", "actor_id", actorID, "target_id", targetID)
	return nil
}

// ResetUserPassword 超级管理员重置用户密码。
// 安全约束：仅 SUPER_ADMIN 可执行；不可重置自己密码（用 change-password 自助）。
// 重置后用户需用新密码重新登录。
func (s *AuthService) ResetUserPassword(ctx context.Context, actorID int64,
	actorRole string, targetID int64, newPassword string) error {
	if actorRole != constants.RoleSuperAdmin {
		return apperrors.Forbidden("AUTH_FORBIDDEN_ROLE", "仅超级管理员可重置用户密码")
	}
	if actorID == targetID {
		return apperrors.Conflict("AUTH_SELF_RESET", "不能重置自己的密码，请使用修改密码功能")
	}
	if err := validatePasswordStrength(newPassword); err != nil {
		return err
	}
	target, err := s.repo.GetByID(ctx, targetID)
	if err != nil {
		return fmt.Errorf("get target user: %w", err)
	}
	if target == nil {
		return apperrors.NotFound("AUTH_USER_NOT_FOUND", "用户不存在")
	}
	hash, err := crypto.HashPassword(newPassword, s.cfg.Argon2)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if err := s.repo.UpdatePasswordHash(ctx, targetID, hash); err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	slog.InfoContext(ctx, "user password reset by admin", "actor_id", actorID, "target_id", targetID)
	return nil
}

// accountDTO 将 User 实体映射为 AccountDTO（剥离 password_hash 等敏感字段）。
func accountDTO(u *entity.User) AccountDTO {
	return AccountDTO{
		ID:               u.ID,
		Username:         u.Username,
		Role:             u.Role,
		Phone:            u.Phone,
		DateOfBirth:      u.DateOfBirth,
		Gender:           u.Gender,
		EmergencyContact: u.EmergencyContact,
		EmergencyPhone:   u.EmergencyPhone,
		IsActive:         u.IsActive,
		IsDeleted:        u.IsDeleted,
		CreatedAt:        u.CreatedAt,
	}
}

// validRole 校验角色是否为系统定义的合法角色。
func validRole(role string) bool {
	switch role {
	case constants.RoleSuperAdmin, constants.RoleDeptAdmin, constants.RoleDoctor,
		constants.RoleNurse, constants.RolePatient:
		return true
	default:
		return false
	}
}

// validGender 校验性别是否为合法值（空串允许，表示未设置）。
func validGender(gender string) bool {
	switch gender {
	case "", "male", "female", "other":
		return true
	default:
		return false
	}
}

// generateResetToken 生成 32 字节的密码重置 token（hex 编码，64 字符）。
func generateResetToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// tryClaim 原子占用 refresh token：SETNX 成功表示首次占用，失败表示已被占用。
// 用于 Refresh 轮换与 Logout 占用：用单步原子操作替代 isBlacklisted+blacklist 两步，消除 TOCTOU 竞态。
// 返回 (true, nil) = 占用成功；(false, nil) = 已被占用；(false, err) = Redis 故障。
func (s *AuthService) tryClaim(ctx context.Context, refreshToken string) (bool, error) {
	ok, err := s.rdb.SetNX(ctx, blacklistKey(refreshToken), "1", s.issuer.RefreshTTL()).Result()
	if err != nil {
		return false, err
	}
	return ok, nil
}

// blacklistKey 生成 refresh token 的 Redis 黑名单 key。
// 用 SHA-256 摘要而非全量 token：Redis 内存常驻 key/value，存全量 token 等于在 Redis 中
// 留存可重放的 refresh token（Redis 持久化/泄露即重放风险）；摘要不可逆，仅作占用标记。
func blacklistKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return blacklistKeyPrefix + hex.EncodeToString(sum[:])
}

func validateUsername(username string) error {
	n := len(username)
	if n < usernameMinLen || n > usernameMaxLen {
		return apperrors.Validation("AUTH_USERNAME_INVALID", "用户名长度需为 3-64 字符")
	}
	for _, c := range username {
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '_' {
			return apperrors.Validation("AUTH_USERNAME_INVALID", "用户名仅允许字母、数字、下划线")
		}
	}
	return nil
}

func validatePasswordStrength(password string) error {
	if len(password) < passwordMinLen {
		return apperrors.Validation("AUTH_PASSWORD_WEAK", "密码至少 8 位，需含字母和数字")
	}
	hasLetter, hasDigit := false, false
	for _, c := range password {
		switch {
		case unicode.IsLetter(c):
			hasLetter = true
		case unicode.IsDigit(c):
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return apperrors.Validation("AUTH_PASSWORD_WEAK", "密码至少 8 位，需含字母和数字")
	}
	return nil
}
