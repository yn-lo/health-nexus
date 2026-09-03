package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"health-nexus/internal/domain/config/entity"
	"health-nexus/internal/platform/postgres"
)

// SafetyMessageRepo 对应 safety_messages 表。表 UNIQUE(type) 约束由 schema.sql 建立。
type SafetyMessageRepo struct {
	pool *pgxpool.Pool
}

// NewSafetyMessageRepo 创建 SafetyMessageRepo。
func NewSafetyMessageRepo(pool *pgxpool.Pool) *SafetyMessageRepo {
	return &SafetyMessageRepo{pool: pool}
}

// ListAll 返回全部安全话术行。service 层按 type 聚合为单例视图。
func (r *SafetyMessageRepo) ListAll(ctx context.Context) ([]*entity.SafetyMessage, error) {
	sql := `SELECT id, type, content, is_active, updated_at FROM safety_messages ORDER BY type`
	rows, err := r.pool.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("query safety_messages: %w", err)
	}
	defer rows.Close()

	var out []*entity.SafetyMessage
	for rows.Next() {
		m := &entity.SafetyMessage{}
		if err := rows.Scan(&m.ID, &m.Type, &m.Content, &m.IsActive, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan safety_message: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Upsert 按 type 更新或插入。依赖 schema.sql 的 UNIQUE(type) 索引，
// 单条 INSERT ... ON CONFLICT 原子完成，消除 UPDATE-then-INSERT 的并发竞态（FIX-7）。
// 感知 ctx 内事务：UpdateSafetyMessages 在事务内批量调用时复用同一连接（Medium 1）。
func (r *SafetyMessageRepo) Upsert(ctx context.Context, msgType, content string) error {
	_, err := postgres.Q(ctx, r.pool).Exec(ctx,
		`INSERT INTO safety_messages (type, content) VALUES ($1, $2)
		 ON CONFLICT (type) DO UPDATE SET content = EXCLUDED.content, updated_at = now()`,
		msgType, content,
	)
	if err != nil {
		return fmt.Errorf("upsert safety_message: %w", err)
	}
	return nil
}
