// Package repository 实现 auth 域的数据访问层（pgx 手写 SQL）。
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"health-nexus/internal/domain/auth/entity"
)

// UserRepo 用户数据访问对象（pgx 手写 SQL）。
type UserRepo struct {
	pool *pgxpool.Pool
}

// NewUserRepo 构造 UserRepo。
func NewUserRepo(pool *pgxpool.Pool) *UserRepo {
	return &UserRepo{pool: pool}
}

// GetByUsername 按用户名查询用户，LEFT JOIN user_departments 取主科室 ID。
// 仅返回未删除用户。未找到返回 (nil, nil)，由 service 层映射为业务错误。
func (r *UserRepo) GetByUsername(ctx context.Context, username string) (*entity.User, error) {
	const q = `
		SELECT u.id, u.username, u.role, u.password_hash,
		       u.phone, u.date_of_birth, u.gender, u.emergency_contact, u.emergency_phone,
		       u.is_active, u.is_deleted,
		       u.created_at, u.updated_at,
		       COALESCE(ud.department_id, 0) AS primary_dept_id
		FROM users u
		LEFT JOIN user_departments ud ON ud.user_id = u.id AND ud.is_primary = TRUE
		WHERE u.username = $1 AND u.is_deleted = false`
	u := &entity.User{}
	err := r.pool.QueryRow(ctx, q, username).Scan(
		&u.ID, &u.Username, &u.Role, &u.PasswordHash,
		&u.Phone, &u.DateOfBirth, &u.Gender, &u.EmergencyContact, &u.EmergencyPhone,
		&u.IsActive, &u.IsDeleted,
		&u.CreatedAt, &u.UpdatedAt,
		&u.PrimaryDeptID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query user by username: %w", err)
	}
	return u, nil
}

// GetByID 按 ID 查询用户，LEFT JOIN user_departments 取主科室 ID。
// 仅返回未删除用户。未找到返回 (nil, nil)，由 service 层映射为业务错误。
func (r *UserRepo) GetByID(ctx context.Context, id int64) (*entity.User, error) {
	const q = `
		SELECT u.id, u.username, u.role, u.password_hash,
		       u.phone, u.date_of_birth, u.gender, u.emergency_contact, u.emergency_phone,
		       u.is_active, u.is_deleted,
		       u.created_at, u.updated_at,
		       COALESCE(ud.department_id, 0) AS primary_dept_id
		FROM users u
		LEFT JOIN user_departments ud ON ud.user_id = u.id AND ud.is_primary = TRUE
		WHERE u.id = $1 AND u.is_deleted = false`
	u := &entity.User{}
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&u.ID, &u.Username, &u.Role, &u.PasswordHash,
		&u.Phone, &u.DateOfBirth, &u.Gender, &u.EmergencyContact, &u.EmergencyPhone,
		&u.IsActive, &u.IsDeleted,
		&u.CreatedAt, &u.UpdatedAt,
		&u.PrimaryDeptID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query user by id: %w", err)
	}
	return u, nil
}

// UpdatePasswordHash 更新用户密码哈希。
// 用户不存在时返回 nil error（由调用方校验 user 是否存在）。
func (r *UserRepo) UpdatePasswordHash(ctx context.Context, userID int64, passwordHash string) error {
	const q = `UPDATE users SET password_hash = $2, updated_at = NOW() WHERE id = $1 AND is_deleted = false`
	_, err := r.pool.Exec(ctx, q, userID, passwordHash)
	if err != nil {
		return fmt.Errorf("update password hash: %w", err)
	}
	return nil
}

// UpdateProfile 更新用户个人资料字段。
// 用户不存在时返回 nil error（由调用方校验 user 是否存在）。
func (r *UserRepo) UpdateProfile(ctx context.Context, userID int64, phone string, dateOfBirth *time.Time, gender string, emergencyContact string, emergencyPhone string) error {
	const q = `UPDATE users SET phone = $2, date_of_birth = $3, gender = $4, emergency_contact = $5, emergency_phone = $6, updated_at = NOW() WHERE id = $1 AND is_deleted = false`
	_, err := r.pool.Exec(ctx, q, userID, phone, dateOfBirth, gender, emergencyContact, emergencyPhone)
	if err != nil {
		return fmt.Errorf("update profile: %w", err)
	}
	return nil
}

// SetActive 设置用户启用/锁定状态（is_active）。
// 用户不存在时返回 nil error（由调用方校验 user 是否存在）。
func (r *UserRepo) SetActive(ctx context.Context, userID int64, active bool) error {
	const q = `UPDATE users SET is_active = $2, updated_at = NOW() WHERE id = $1 AND is_deleted = false`
	_, err := r.pool.Exec(ctx, q, userID, active)
	if err != nil {
		return fmt.Errorf("set user active: %w", err)
	}
	return nil
}

// SoftDelete 软删除用户：将 is_deleted 设为 true，同时锁定账户。
// 用户不存在或已删除时返回 nil error（由调用方校验 user 是否存在）。
func (r *UserRepo) SoftDelete(ctx context.Context, userID int64) error {
	const q = `UPDATE users SET is_deleted = true, is_active = false, updated_at = NOW() WHERE id = $1 AND is_deleted = false`
	_, err := r.pool.Exec(ctx, q, userID)
	if err != nil {
		return fmt.Errorf("soft delete user: %w", err)
	}
	return nil
}

// List 分页查询用户列表（含锁定用户，不含已删除用户），按 id 升序，LEFT JOIN 取主科室 ID。
// 返回 (用户切片, 总数, error)。总数为未删除用户计数（不受分页影响），供前端分页展示。
func (r *UserRepo) List(ctx context.Context, limit, offset int) ([]*entity.User, int64, error) {
	const countQ = `SELECT COUNT(*) FROM users WHERE is_deleted = false`
	var total int64
	if err := r.pool.QueryRow(ctx, countQ).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}

	const listQ = `
		SELECT u.id, u.username, u.role, u.password_hash,
		       u.phone, u.date_of_birth, u.gender, u.emergency_contact, u.emergency_phone,
		       u.is_active, u.is_deleted,
		       u.created_at, u.updated_at,
		       COALESCE(ud.department_id, 0) AS primary_dept_id
		FROM users u
		LEFT JOIN user_departments ud ON ud.user_id = u.id AND ud.is_primary = TRUE
		WHERE u.is_deleted = false
		ORDER BY u.id ASC
		LIMIT $1 OFFSET $2`
	rows, err := r.pool.Query(ctx, listQ, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()

	users := make([]*entity.User, 0, limit)
	for rows.Next() {
		u := &entity.User{}
		if err := rows.Scan(
			&u.ID, &u.Username, &u.Role, &u.PasswordHash,
			&u.Phone, &u.DateOfBirth, &u.Gender, &u.EmergencyContact, &u.EmergencyPhone,
			&u.IsActive, &u.IsDeleted,
			&u.CreatedAt, &u.UpdatedAt,
			&u.PrimaryDeptID,
		); err != nil {
			return nil, 0, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate users: %w", err)
	}
	return users, total, nil
}

// ListByDept 分页查询指定科室的用户列表（含锁定用户，不含已删除用户）。
// 通过 user_departments 表 JOIN 过滤，不限制 is_primary（覆盖主科室 + 兼任科室）。
// 返回 (用户切片, 总数, error)。
func (r *UserRepo) ListByDept(ctx context.Context, deptID, limit, offset int64) ([]*entity.User, int64, error) {
	const countQ = `SELECT COUNT(*)
		FROM users u
		JOIN user_departments ud ON ud.user_id = u.id AND ud.department_id = $1
		WHERE u.is_deleted = false`
	var total int64
	if err := r.pool.QueryRow(ctx, countQ, deptID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count users by dept: %w", err)
	}

	const listQ = `
		SELECT u.id, u.username, u.role, u.password_hash,
		       u.phone, u.date_of_birth, u.gender, u.emergency_contact, u.emergency_phone,
		       u.is_active, u.is_deleted,
		       u.created_at, u.updated_at,
		       COALESCE(ud2.department_id, 0) AS primary_dept_id
		FROM users u
		JOIN user_departments ud ON ud.user_id = u.id AND ud.department_id = $1
		LEFT JOIN user_departments ud2 ON ud2.user_id = u.id AND ud2.is_primary = TRUE
		WHERE u.is_deleted = false
		ORDER BY u.id ASC
		LIMIT $2 OFFSET $3`
	rows, err := r.pool.Query(ctx, listQ, deptID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query users by dept: %w", err)
	}
	defer rows.Close()

	users := make([]*entity.User, 0, limit)
	for rows.Next() {
		u := &entity.User{}
		if err := rows.Scan(
			&u.ID, &u.Username, &u.Role, &u.PasswordHash,
			&u.Phone, &u.DateOfBirth, &u.Gender, &u.EmergencyContact, &u.EmergencyPhone,
			&u.IsActive, &u.IsDeleted,
			&u.CreatedAt, &u.UpdatedAt,
			&u.PrimaryDeptID,
		); err != nil {
			return nil, 0, fmt.Errorf("scan user by dept: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate users by dept: %w", err)
	}
	return users, total, nil
}

// Create 创建用户，返回带 ID 与时间戳的 User。
// username 唯一约束冲突时返回 *pgconn.PgError（Code=23505），由 service 层映射为 409。
func (r *UserRepo) Create(ctx context.Context, username, passwordHash, role string) (*entity.User, error) {
	const q = `
		INSERT INTO users (username, role, password_hash)
		VALUES ($1, $2, $3)
		RETURNING id, username, role, password_hash,
		          phone, date_of_birth, gender, emergency_contact, emergency_phone,
		          is_active, is_deleted, created_at, updated_at`
	u := &entity.User{}
	err := r.pool.QueryRow(ctx, q, username, role, passwordHash).Scan(
		&u.ID, &u.Username, &u.Role, &u.PasswordHash,
		&u.Phone, &u.DateOfBirth, &u.Gender, &u.EmergencyContact, &u.EmergencyPhone,
		&u.IsActive, &u.IsDeleted, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	// 新用户无 user_departments 行，PrimaryDeptID 保持零值 0。
	return u, nil
}
