package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"health-nexus/internal/platform/postgres"
)

// CrisisOutboxRepo 危机通知 outbox 仓储（P1）。
// 危机事件创建时在同一事务内写入 outbox 记录；relay 周期扫描未处理记录投递通知任务，
// 保证 Redis / 入队瞬时故障时"医护通知"不会静默丢失（原实现仅记日志，无补投）。
type CrisisOutboxRepo struct {
	pool *pgxpool.Pool
}

// NewCrisisOutboxRepo 构造危机通知 outbox 仓储。
func NewCrisisOutboxRepo(pool *pgxpool.Pool) *CrisisOutboxRepo {
	return &CrisisOutboxRepo{pool: pool}
}

// CrisisOutboxRecord 待投递的危机通知记录。
type CrisisOutboxRecord struct {
	ID        int64
	EventID   int64
	CreatedAt time.Time
}

// ListPending 查询未处理的记录（processed=false），按 created_at 升序，限制条数。
func (r *CrisisOutboxRepo) ListPending(ctx context.Context, limit int) ([]CrisisOutboxRecord, error) {
	const sql = `SELECT id, event_id, created_at
		FROM crisis_outbox
		WHERE processed = false
		ORDER BY created_at ASC
		LIMIT $1`
	rows, err := postgres.Q(ctx, r.pool).Query(ctx, sql, limit)
	if err != nil {
		return nil, fmt.Errorf("list pending crisis outbox: %w", err)
	}
	defer rows.Close()
	out := make([]CrisisOutboxRecord, 0)
	for rows.Next() {
		var rec CrisisOutboxRecord
		if err := rows.Scan(&rec.ID, &rec.EventID, &rec.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan crisis outbox record: %w", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// MarkProcessed 标记记录已处理。
func (r *CrisisOutboxRepo) MarkProcessed(ctx context.Context, id int64) error {
	const sql = `UPDATE crisis_outbox SET processed = true, processed_at = now() WHERE id = $1`
	if _, err := postgres.Q(ctx, r.pool).Exec(ctx, sql, id); err != nil {
		return fmt.Errorf("mark crisis outbox processed: %w", err)
	}
	return nil
}
