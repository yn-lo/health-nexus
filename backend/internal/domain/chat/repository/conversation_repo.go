// Package repository 实现 chat 域持久化，手写 SQL（pgx）。
// 事务由 Service 层通过 postgres.TxFromCtx(ctx) 注入；无事务时回退到 pool。
package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"health-nexus/internal/domain/chat/entity"
	"health-nexus/internal/platform/postgres"
)

// ConversationRepo 会话仓储。
type ConversationRepo struct {
	pool *pgxpool.Pool
}

// NewConversationRepo 构造会话仓储。
func NewConversationRepo(pool *pgxpool.Pool) *ConversationRepo {
	return &ConversationRepo{pool: pool}
}

// Create 创建新会话，返回完整实体。
// lockedDeptID 为 nil 表示未锁定科室。
func (r *ConversationRepo) Create(
	ctx context.Context, patientID int64, lockedDeptID *int64,
) (*entity.Conversation, error) {
	const sql = `INSERT INTO conversations (patient_id, locked_dept_id)
	             VALUES ($1, $2)
	             RETURNING id, patient_id, locked_dept_id, title, is_archived, last_message_at, created_at, updated_at`
	c := &entity.Conversation{}
	row := postgres.Q(ctx, r.pool).QueryRow(ctx, sql, patientID, lockedDeptID)
	if err := row.Scan(
		&c.ID, &c.PatientID, &c.LockedDeptID, &c.Title,
		&c.IsArchived, &c.LastMessageAt, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("create conversation: %w", err)
	}
	return c, nil
}

// GetByIDForPatient 取会话并校验属于该患者；不存在或不属于返回 (nil, nil)。
func (r *ConversationRepo) GetByIDForPatient(
	ctx context.Context, id uuid.UUID, patientID int64,
) (*entity.Conversation, error) {
	const sql = `SELECT id, patient_id, locked_dept_id, title, is_archived, last_message_at, created_at, updated_at
	             FROM conversations WHERE id = $1 AND patient_id = $2`
	c := &entity.Conversation{}
	row := postgres.Q(ctx, r.pool).QueryRow(ctx, sql, id, patientID)
	if err := row.Scan(
		&c.ID, &c.PatientID, &c.LockedDeptID, &c.Title,
		&c.IsArchived, &c.LastMessageAt, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get conversation: %w", err)
	}
	return c, nil
}

// ListByPatient 列出患者会话。includeArchived=false 时仅未归档。
func (r *ConversationRepo) ListByPatient(
	ctx context.Context, patientID int64, includeArchived bool, limit, offset int,
) ([]*entity.Conversation, int64, error) {
	args := []any{patientID}
	filter := "patient_id = $1 AND last_message_at IS NOT NULL"
	if !includeArchived {
		args = append(args, false)
		filter += fmt.Sprintf(" AND is_archived = $%d", len(args))
	}
	// count
	var total int64
	countSQL := fmt.Sprintf(`SELECT count(*) FROM conversations WHERE %s`, filter)
	if err := postgres.Q(ctx, r.pool).QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count conversations: %w", err)
	}
	args = append(args, limit, offset)
	// id DESC 作为 tiebreaker：last_message_at 相同（如同一时刻创建/触发）时保证跨页顺序确定，
	// 避免 OFFSET 分页在不同查询间对并列行返回不同顺序导致重复/漏行。
	listSQL := fmt.Sprintf(
		`SELECT id, patient_id, locked_dept_id, title, is_archived,
		        last_message_at, created_at, updated_at
		 FROM conversations WHERE %s
		 ORDER BY last_message_at DESC, id DESC LIMIT $%d OFFSET $%d`,
		filter, len(args)-1, len(args))
	rows, err := postgres.Q(ctx, r.pool).Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()
	out := []*entity.Conversation{}
	for rows.Next() {
		c := &entity.Conversation{}
		if err := rows.Scan(
			&c.ID, &c.PatientID, &c.LockedDeptID, &c.Title,
			&c.IsArchived, &c.LastMessageAt, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan conversation: %w", err)
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

// Patch 更新会话。title/archived 为 nil 表示不更新。
// 返回更新后的实体；不存在返回 (nil, nil)。
func (r *ConversationRepo) Patch(
	ctx context.Context, id uuid.UUID, patientID int64, title *string, archived *bool,
) (*entity.Conversation, error) {
	sets := []string{}
	args := []any{}
	if title != nil {
		args = append(args, *title)
		sets = append(sets, fmt.Sprintf("title = $%d", len(args)))
	}
	if archived != nil {
		args = append(args, *archived)
		sets = append(sets, fmt.Sprintf("is_archived = $%d", len(args)))
	}
	if len(sets) == 0 {
		// 无字段更新：直接返回当前实体
		return r.GetByIDForPatient(ctx, id, patientID)
	}
	args = append(args, id, patientID)
	sql := fmt.Sprintf(
		`UPDATE conversations SET %s, updated_at = now()
		 WHERE id = $%d AND patient_id = $%d
		 RETURNING id, patient_id, locked_dept_id, title,
		           is_archived, last_message_at, created_at, updated_at`,
		strings.Join(sets, ", "), len(args)-1, len(args))
	c := &entity.Conversation{}
	row := postgres.Q(ctx, r.pool).QueryRow(ctx, sql, args...)
	if err := row.Scan(
		&c.ID, &c.PatientID, &c.LockedDeptID, &c.Title,
		&c.IsArchived, &c.LastMessageAt, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("patch conversation: %w", err)
	}
	return c, nil
}

// Delete 删除会话（CASCADE messages）。返回受影响行数；0 表示不存在或不属于该患者。
func (r *ConversationRepo) Delete(ctx context.Context, id uuid.UUID, patientID int64) (int64, error) {
	const sql = `DELETE FROM conversations WHERE id = $1 AND patient_id = $2`
	tag, err := postgres.Q(ctx, r.pool).Exec(ctx, sql, id, patientID)
	if err != nil {
		return 0, fmt.Errorf("delete conversation: %w", err)
	}
	return tag.RowsAffected(), nil
}

// LockDept 锁定会话科室（仅当未锁定时执行；已锁定保持原值）。
// 已锁定时返回 nil（幂等）。
func (r *ConversationRepo) LockDept(ctx context.Context, id uuid.UUID, deptID int64) error {
	const sql = `UPDATE conversations SET locked_dept_id = $2, updated_at = now()
	             WHERE id = $1 AND locked_dept_id IS NULL`
	_, err := postgres.Q(ctx, r.pool).Exec(ctx, sql, id, deptID)
	if err != nil {
		return fmt.Errorf("lock dept: %w", err)
	}
	return nil
}

// UpdateTitleIfEmpty 仅当当前 title 为空时设置标题（首条消息前 20 字截断）。
func (r *ConversationRepo) UpdateTitleIfEmpty(ctx context.Context, id uuid.UUID, title string) error {
	const sql = `UPDATE conversations SET title = $2, updated_at = now()
	             WHERE id = $1 AND title = ''`
	_, err := postgres.Q(ctx, r.pool).Exec(ctx, sql, id, title)
	if err != nil {
		return fmt.Errorf("update title: %w", err)
	}
	return nil
}

// TouchLastMessageAt 更新会话最近消息时间戳。
// 用 DB now() 而非 app time.Now()——与 updated_at 一致，且避免多实例时钟漂移导致 last_message_at 乱序。
func (r *ConversationRepo) TouchLastMessageAt(ctx context.Context, id uuid.UUID) error {
	const sql = `UPDATE conversations SET last_message_at = now(), updated_at = now() WHERE id = $1`
	_, err := postgres.Q(ctx, r.pool).Exec(ctx, sql, id)
	if err != nil {
		return fmt.Errorf("touch last_message_at: %w", err)
	}
	return nil
}
