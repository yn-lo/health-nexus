package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"health-nexus/internal/domain/wiki/entity"
	"health-nexus/internal/platform/postgres"
)

// AuditLogRepo 文章审计日志仓储。
// 不可变：仅提供 Create，无 Update/Delete（AC-SEC-06，REQ-WIKI-002）。
type AuditLogRepo struct {
	pool *pgxpool.Pool
}

// NewAuditLogRepo 构造审计日志仓储。
func NewAuditLogRepo(pool *pgxpool.Pool) *AuditLogRepo {
	return &AuditLogRepo{pool: pool}
}

// Create 插入审计日志。log.ID/CreatedAt 由 RETURNING 回填。
// 调用方需保证 OperatorID/Action/FromStatus/ToStatus 等字段已填充。
func (r *AuditLogRepo) Create(ctx context.Context, log *entity.ArticleAuditLog) error {
	const sql = `INSERT INTO article_audit_logs
		(article_id, operator_id, action, from_status, to_status, summary, reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at`
	return postgres.Q(ctx, r.pool).QueryRow(ctx, sql,
		log.ArticleID, log.OperatorID, log.Action, log.FromStatus, log.ToStatus, log.Summary, log.Reason,
	).Scan(&log.ID, &log.CreatedAt)
}

// ListByArticle 列出文章的审计日志（按时间倒序），供审计回看。limit=0 表示不限制。
func (r *AuditLogRepo) ListByArticle(
	ctx context.Context, articleID int64, limit int,
) ([]*entity.ArticleAuditLog, error) {
	sql := `SELECT id, article_id, operator_id, action, from_status, to_status, summary, reason, created_at
		FROM article_audit_logs WHERE article_id = $1 ORDER BY created_at DESC`
	args := []any{articleID}
	if limit > 0 {
		sql += fmt.Sprintf(" LIMIT $%d", len(args)+1)
		args = append(args, limit)
	}
	rows, err := postgres.Q(ctx, r.pool).Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list audit_logs: %w", err)
	}
	defer rows.Close()

	out := make([]*entity.ArticleAuditLog, 0)
	for rows.Next() {
		log := &entity.ArticleAuditLog{}
		if err := rows.Scan(&log.ID, &log.ArticleID, &log.OperatorID, &log.Action,
			&log.FromStatus, &log.ToStatus, &log.Summary, &log.Reason, &log.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audit_log: %w", err)
		}
		out = append(out, log)
	}
	return out, rows.Err()
}
