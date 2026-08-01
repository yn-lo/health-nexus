package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"health-nexus/internal/domain/config/entity"
	"health-nexus/internal/platform/postgres"
	"health-nexus/internal/shared/pagination"
)

// SafetyRuleRepo 对应 safety_rules 表。
type SafetyRuleRepo struct {
	pool *pgxpool.Pool
}

// NewSafetyRuleRepo 创建 SafetyRuleRepo。
func NewSafetyRuleRepo(pool *pgxpool.Pool) *SafetyRuleRepo {
	return &SafetyRuleRepo{pool: pool}
}

const safetyRuleColumns = `id, name, category, pattern, action, replacement,` +
	` is_active, description, created_at, updated_at`

// List 返回分页结果。category 空表示不过滤。
func (r *SafetyRuleRepo) List(
	ctx context.Context, category string, p pagination.Params,
) ([]*entity.SafetyRule, int64, error) {
	where := ""
	args := []any{}
	if category != "" {
		args = append(args, category)
		where = " WHERE category = $" + fmt.Sprintf("%d", len(args))
	}

	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM safety_rules"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count safety_rules: %w", err)
	}

	q := "SELECT " + safetyRuleColumns + " FROM safety_rules" + where +
		" ORDER BY created_at DESC LIMIT $" + fmt.Sprintf("%d", len(args)+1) +
		" OFFSET $" + fmt.Sprintf("%d", len(args)+2)
	args = append(args, p.PageSize, p.Offset())

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query safety_rules: %w", err)
	}
	defer rows.Close()

	var out []*entity.SafetyRule
	for rows.Next() {
		rule, err := scanSafetyRule(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, rule)
	}
	return out, total, rows.Err()
}

// Get 按 ID 查询。未找到返回 ErrNotFound。
func (r *SafetyRuleRepo) Get(ctx context.Context, id int64) (*entity.SafetyRule, error) {
	q := "SELECT " + safetyRuleColumns + " FROM safety_rules WHERE id = $1"
	row := r.pool.QueryRow(ctx, q, id)
	rule, err := scanSafetyRule(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return rule, nil
}

// Create 插入新安全规则。
func (r *SafetyRuleRepo) Create(ctx context.Context, rule *entity.SafetyRule) error {
	q := `INSERT INTO safety_rules (name, category, pattern, action, replacement, is_active, description)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at, updated_at`
	return r.pool.QueryRow(ctx, q,
		rule.Name, rule.Category, rule.Pattern, rule.Action, rule.Replacement, rule.IsActive, rule.Description,
	).Scan(&rule.ID, &rule.CreatedAt, &rule.UpdatedAt)
}

// Update 全量更新。
func (r *SafetyRuleRepo) Update(ctx context.Context, rule *entity.SafetyRule) error {
	q := `UPDATE safety_rules SET
		name = $2, category = $3, pattern = $4, action = $5,
		replacement = $6, is_active = $7, description = $8, updated_at = now()
		WHERE id = $1
		RETURNING created_at, updated_at`
	tag, err := r.pool.Exec(ctx, q,
		rule.ID, rule.Name, rule.Category, rule.Pattern, rule.Action, rule.Replacement, rule.IsActive, rule.Description,
	)
	if err != nil {
		return fmt.Errorf("update safety_rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete 按 ID 删除。未找到返回 ErrNotFound。
func (r *SafetyRuleRepo) Delete(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, "DELETE FROM safety_rules WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete safety_rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanSafetyRule(s postgres.Scanner) (*entity.SafetyRule, error) {
	rule := &entity.SafetyRule{}
	err := s.Scan(
		&rule.ID, &rule.Name, &rule.Category, &rule.Pattern, &rule.Action,
		&rule.Replacement, &rule.IsActive, &rule.Description, &rule.CreatedAt, &rule.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan safety_rule: %w", err)
	}
	return rule, nil
}
