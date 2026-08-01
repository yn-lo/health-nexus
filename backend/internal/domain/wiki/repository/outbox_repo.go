package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"health-nexus/internal/platform/postgres"
)

// OutboxRepo 向量化任务 outbox 仓储。
// 事务内写入 outbox 记录，由 relay 进程扫描未处理记录并投递到 asynq，
// 保证文章发布/更新后向量化任务最终一致投递（替代事务外直接 Enqueue 的 fire-and-forget 模式）。
type OutboxRepo struct {
	pool *pgxpool.Pool
}

func NewOutboxRepo(pool *pgxpool.Pool) *OutboxRepo {
	return &OutboxRepo{pool: pool}
}

// Insert 在事务内插入一条 outbox 记录（article_id + created_at）。
// 由 ArticleService 在 Approve/Update/Unarchive 事务内调用，保证与状态迁移原子性。
func (r *OutboxRepo) Insert(ctx context.Context, articleID int64) error {
	const sql = `INSERT INTO vectorize_outbox (article_id) VALUES ($1)`
	_, err := postgres.Q(ctx, r.pool).Exec(ctx, sql, articleID)
	if err != nil {
		return fmt.Errorf("insert vectorize outbox: %w", err)
	}
	return nil
}

// PendingRecord 待投递的 outbox 记录。
type PendingRecord struct {
	ID        int64
	ArticleID int64
	CreatedAt time.Time
}

// ListPending 查询未处理的 outbox 记录（processed=false），按 created_at 升序，限制条数。
// 由 relay 进程定期调用。
func (r *OutboxRepo) ListPending(ctx context.Context, limit int) ([]PendingRecord, error) {
	const sql = `SELECT id, article_id, created_at
		FROM vectorize_outbox
		WHERE processed = false
		ORDER BY created_at ASC
		LIMIT $1`
	rows, err := postgres.Q(ctx, r.pool).Query(ctx, sql, limit)
	if err != nil {
		return nil, fmt.Errorf("list pending outbox: %w", err)
	}
	defer rows.Close()
	out := make([]PendingRecord, 0)
	for rows.Next() {
		var rec PendingRecord
		if err := rows.Scan(&rec.ID, &rec.ArticleID, &rec.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan outbox record: %w", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// MarkProcessed 标记 outbox 记录为已处理。
func (r *OutboxRepo) MarkProcessed(ctx context.Context, id int64) error {
	const sql = `UPDATE vectorize_outbox SET processed = true, processed_at = now() WHERE id = $1`
	_, err := postgres.Q(ctx, r.pool).Exec(ctx, sql, id)
	if err != nil {
		return fmt.Errorf("mark outbox processed: %w", err)
	}
	return nil
}
