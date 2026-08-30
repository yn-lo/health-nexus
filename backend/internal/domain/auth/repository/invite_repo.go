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

// InviteRepo 邀请码数据访问对象（pgx 手写 SQL）。
type InviteRepo struct {
	pool *pgxpool.Pool
}

// NewInviteRepo 构造 InviteRepo。
func NewInviteRepo(pool *pgxpool.Pool) *InviteRepo {
	return &InviteRepo{pool: pool}
}

// Create 写入一条邀请码。code 唯一约束冲突时返回 *pgconn.PgError（Code=23505），
// 调用方（service 生成流程）捕获后重试重新生成。
func (r *InviteRepo) Create(ctx context.Context, ic *entity.InviteCode) error {
	const q = `
		INSERT INTO invite_codes (code, role, created_by, expires_at)
		VALUES ($1, $2, $3, $4)`
	if _, err := r.pool.Exec(ctx, q, ic.Code, ic.Role, ic.CreatedBy, ic.ExpiresAt); err != nil {
		return fmt.Errorf("create invite code: %w", err)
	}
	return nil
}

// List 分页查询全部邀请码（含已使用/已过期），按创建时间倒序，供管理员查看。
// 返回 (邀请码切片, 总数, error)。
func (r *InviteRepo) List(ctx context.Context, limit, offset int) ([]*entity.InviteCode, int64, error) {
	const countQ = `SELECT COUNT(*) FROM invite_codes`
	var total int64
	if err := r.pool.QueryRow(ctx, countQ).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count invite codes: %w", err)
	}

	const listQ = `
		SELECT id, code, role, created_by, used_by, used_at, expires_at, created_at
		FROM invite_codes
		ORDER BY created_at DESC, id DESC
		LIMIT $1 OFFSET $2`
	rows, err := r.pool.Query(ctx, listQ, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query invite codes: %w", err)
	}
	defer rows.Close()

	codes := make([]*entity.InviteCode, 0, limit)
	for rows.Next() {
		ic := &entity.InviteCode{}
		if err := rows.Scan(
			&ic.ID, &ic.Code, &ic.Role, &ic.CreatedBy, &ic.UsedBy,
			&ic.UsedAt, &ic.ExpiresAt, &ic.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan invite code: %w", err)
		}
		codes = append(codes, ic)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate invite codes: %w", err)
	}
	return codes, total, nil
}

// ConsumeForRegistration 在单事务内完成"校验并消费邀请码 + 创建用户"。
// 流程：锁定该码行（SELECT ... FOR UPDATE）→ 校验未使用/未过期/角色匹配 → INSERT users →
// 回写 used_by/used_at → Commit。保证并发注册同一码只会成功一次（其余返回 ErrInviteInvalid）。
// 用户名唯一冲突（已存在）返回 *pgconn.PgError（Code=23505），由 service 映射为 409。
func (r *InviteRepo) ConsumeForRegistration(
	ctx context.Context, code, username, passwordHash, role string,
) (*entity.User, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var codeID int64
	var codeRole string
	var usedAt *time.Time
	var expiresAt time.Time
	err = tx.QueryRow(ctx,
		`SELECT id, role, used_at, expires_at FROM invite_codes WHERE code = $1 FOR UPDATE`, code,
	).Scan(&codeID, &codeRole, &usedAt, &expiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, entity.ErrInviteInvalid
		}
		return nil, fmt.Errorf("lock invite code: %w", err)
	}
	// 统一判不可用：已使用 / 已过期 / 目标角色不符。
	if usedAt != nil || codeRole != role || time.Now().After(expiresAt) {
		return nil, entity.ErrInviteInvalid
	}

	const insertUser = `
		INSERT INTO users (username, role, password_hash)
		VALUES ($1, $2, $3)
		RETURNING id, username, role, password_hash,
		          phone, date_of_birth, gender, emergency_contact, emergency_phone,
		          is_active, is_deleted, created_at, updated_at`
	u := &entity.User{}
	if err := tx.QueryRow(ctx, insertUser, username, role, passwordHash).Scan(
		&u.ID, &u.Username, &u.Role, &u.PasswordHash,
		&u.Phone, &u.DateOfBirth, &u.Gender, &u.EmergencyContact, &u.EmergencyPhone,
		&u.IsActive, &u.IsDeleted, &u.CreatedAt, &u.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE invite_codes SET used_by = $2, used_at = now() WHERE id = $1`, codeID, u.ID); err != nil {
		return nil, fmt.Errorf("consume invite code: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return u, nil
}
