package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"health-nexus/internal/domain/base/entity"
	"health-nexus/internal/platform/postgres"
)

// NotificationRepo 站内通知仓储，基于 pgx 手写 SQL。
type NotificationRepo struct {
	pool *pgxpool.Pool
}

// NewNotificationRepo 构造通知仓储。
func NewNotificationRepo(pool *pgxpool.Pool) *NotificationRepo {
	return &NotificationRepo{pool: pool}
}

const notificationColumns = `id, recipient_role, recipient_dept_id, type, title, body, ref_id, is_read, created_at`

// deptVisibility 科室可见性子句：广播通知（recipient_dept_id IS NULL）对所有科室可见，
// 定向通知仅对 recipient_dept_id = $2 可见；$2 为 NULL 时仅广播通知可见。
const deptVisibility = `(recipient_dept_id IS NULL OR ($2::BIGINT IS NOT NULL AND recipient_dept_id = $2))`

const createNotificationQuery = `
INSERT INTO notifications (recipient_role, recipient_dept_id, type, title, body, ref_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, created_at`

// Create 插入一条通知，通过 RETURNING 回填 ID/CreatedAt。
func (r *NotificationRepo) Create(ctx context.Context, n *entity.Notification) error {
	tx, _ := postgres.TxFromCtx(ctx)
	exec := r.pool.QueryRow
	if tx != nil {
		exec = tx.QueryRow
	}
	err := exec(ctx, createNotificationQuery,
		n.RecipientRole, n.RecipientDeptID, n.Type, n.Title, n.Body, n.RefID,
	).Scan(&n.ID, &n.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert notification: %w", err)
	}
	n.IsRead = false
	return nil
}

const listForRoleQuery = `
SELECT ` + notificationColumns + `
FROM notifications
WHERE recipient_role = $1 AND ` + deptVisibility + `
ORDER BY is_read ASC, created_at DESC
LIMIT $3`

// ListForRole 返回指定角色+科室可见的通知，未读优先、其后按时间倒序。
func (r *NotificationRepo) ListForRole(
	ctx context.Context, role string, deptID *int64, limit int,
) ([]*entity.Notification, error) {
	rows, err := r.pool.Query(ctx, listForRoleQuery, role, deptID, limit)
	if err != nil {
		return nil, fmt.Errorf("query notifications: %w", err)
	}
	defer rows.Close()

	result := make([]*entity.Notification, 0)
	for rows.Next() {
		n := &entity.Notification{}
		if err := rows.Scan(
			&n.ID, &n.RecipientRole, &n.RecipientDeptID, &n.Type, &n.Title,
			&n.Body, &n.RefID, &n.IsRead, &n.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan notification: %w", err)
		}
		result = append(result, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate notifications: %w", err)
	}
	return result, nil
}

const markReadQuery = `
UPDATE notifications SET is_read = true
WHERE id = $1 AND recipient_role = $2 AND ` + deptVisibility

// MarkRead 将单条通知标记为已读（含角色+科室校验，防 IDOR）。
func (r *NotificationRepo) MarkRead(ctx context.Context, id int64, role string, deptID *int64) error {
	tx, _ := postgres.TxFromCtx(ctx)
	exec := r.pool.Exec
	if tx != nil {
		exec = tx.Exec
	}
	tag, err := exec(ctx, markReadQuery, id, role, deptID)
	if err != nil {
		return fmt.Errorf("mark notification read: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("mark notification read: no rows affected (id=%d, role=%s)", id, role)
	}
	return nil
}

const markAllReadQuery = `
UPDATE notifications SET is_read = true
WHERE recipient_role = $1 AND ` + deptVisibility + ` AND NOT is_read`

// MarkAllRead 将指定角色+科室可见的全部未读通知标记为已读。
func (r *NotificationRepo) MarkAllRead(ctx context.Context, role string, deptID *int64) error {
	tx, _ := postgres.TxFromCtx(ctx)
	exec := r.pool.Exec
	if tx != nil {
		exec = tx.Exec
	}
	if _, err := exec(ctx, markAllReadQuery, role, deptID); err != nil {
		return fmt.Errorf("mark all notifications read: %w", err)
	}
	return nil
}

const unreadCountQuery = `
SELECT COUNT(*) FROM notifications
WHERE recipient_role = $1 AND ` + deptVisibility + ` AND NOT is_read`

// UnreadCount 返回指定角色+科室可见的未读通知数量。
func (r *NotificationRepo) UnreadCount(ctx context.Context, role string, deptID *int64) (int, error) {
	var count int
	if err := r.pool.QueryRow(ctx, unreadCountQuery, role, deptID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count unread notifications: %w", err)
	}
	return count, nil
}
