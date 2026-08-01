package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"health-nexus/internal/domain/config/entity"
)

// ConfigAuditLogRepo 对应 config_audit_logs 表（AC-SEC-06：审计日志只允许 Create/List）。
type ConfigAuditLogRepo struct {
	pool *pgxpool.Pool
}

// NewConfigAuditLogRepo 创建 ConfigAuditLogRepo。
func NewConfigAuditLogRepo(pool *pgxpool.Pool) *ConfigAuditLogRepo {
	return &ConfigAuditLogRepo{pool: pool}
}

// Create 插入一条审计日志。best-effort：失败由调用方决定是否记录告警，不影响主流程。
func (r *ConfigAuditLogRepo) Create(ctx context.Context, log *entity.ConfigAuditLog) error {
	q := `INSERT INTO config_audit_logs (action, entity_type, entity_id, operator_id, operator_role, changes)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at`
	return r.pool.QueryRow(ctx, q,
		log.Action, log.EntityType, log.EntityID, log.OperatorID, log.OperatorRole, log.Changes,
	).Scan(&log.ID, &log.CreatedAt)
}

// ListByEntity 按 (entity_type, entity_id) 分页查询审计日志。
// entityID 为 0 时按 entity_type + entity_id IS NULL 过滤（单例配置审计记录的 entity_id 为 NULL）。
func (r *ConfigAuditLogRepo) ListByEntity(
	ctx context.Context, entityType string, entityID int64, page, pageSize int,
) ([]*entity.ConfigAuditLog, int, error) {
	where := " WHERE entity_type = $1"
	args := []any{entityType}
	if entityID > 0 {
		args = append(args, entityID)
		where += fmt.Sprintf(" AND entity_id = $%d", len(args))
	} else {
		// 单例审计（rag_config/safety_messages 等仅 1 行的配置）：entity_id 列为 NULL。
		where += " AND entity_id IS NULL"
	}

	var total int
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM config_audit_logs"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count config_audit_logs: %w", err)
	}

	if pageSize <= 0 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * pageSize
	args = append(args, pageSize, offset)
	q := "SELECT id, action, entity_type, entity_id, operator_id," +
		" operator_role, changes, created_at FROM config_audit_logs" +
		where + fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query config_audit_logs: %w", err)
	}
	defer rows.Close()

	var out []*entity.ConfigAuditLog
	for rows.Next() {
		l := &entity.ConfigAuditLog{}
		if err := rows.Scan(
			&l.ID, &l.Action, &l.EntityType, &l.EntityID,
			&l.OperatorID, &l.OperatorRole, &l.Changes, &l.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan config_audit_log: %w", err)
		}
		out = append(out, l)
	}
	return out, total, rows.Err()
}
