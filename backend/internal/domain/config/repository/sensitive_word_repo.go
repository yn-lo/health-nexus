package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"health-nexus/internal/domain/config/entity"
	"health-nexus/internal/shared/pagination"
)

// SensitiveWordRepo 对应 sensitive_words 表。
type SensitiveWordRepo struct {
	pool *pgxpool.Pool
}

// NewSensitiveWordRepo 创建 SensitiveWordRepo。
func NewSensitiveWordRepo(pool *pgxpool.Pool) *SensitiveWordRepo {
	return &SensitiveWordRepo{pool: pool}
}

const sensitiveWordColumns = `id, word, category, is_active, created_at`

// List 按 category 过滤（空表示不过滤），返回分页结果。
func (r *SensitiveWordRepo) List(
	ctx context.Context, category string, p pagination.Params,
) ([]*entity.SensitiveWord, int64, error) {
	where := ""
	args := []any{}
	if category != "" {
		args = append(args, category)
		where = " WHERE category = $" + fmt.Sprintf("%d", len(args))
	}

	var total int64
	countQ := "SELECT COUNT(*) FROM sensitive_words" + where
	if err := r.pool.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count sensitive_words: %w", err)
	}

	listQ := "SELECT " + sensitiveWordColumns + " FROM sensitive_words" + where +
		" ORDER BY created_at DESC LIMIT $" + fmt.Sprintf("%d", len(args)+1) +
		" OFFSET $" + fmt.Sprintf("%d", len(args)+2)
	args = append(args, p.PageSize, p.Offset())

	rows, err := r.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query sensitive_words: %w", err)
	}
	defer rows.Close()

	var out []*entity.SensitiveWord
	for rows.Next() {
		w := &entity.SensitiveWord{}
		if err := rows.Scan(&w.ID, &w.Word, &w.Category, &w.IsActive, &w.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan sensitive_word: %w", err)
		}
		out = append(out, w)
	}
	return out, total, rows.Err()
}

// Create 插入新敏感词。UNIQUE(word, category) 冲突由 service 层翻译为 409。
func (r *SensitiveWordRepo) Create(ctx context.Context, w *entity.SensitiveWord) error {
	q := `INSERT INTO sensitive_words (word, category, is_active)
		VALUES ($1, $2, $3)
		RETURNING id, created_at`
	return r.pool.QueryRow(ctx, q, w.Word, w.Category, w.IsActive).Scan(&w.ID, &w.CreatedAt)
}

// Get 按 ID 查询。未找到返回 ErrNotFound。
func (r *SensitiveWordRepo) Get(ctx context.Context, id int64) (*entity.SensitiveWord, error) {
	q := "SELECT " + sensitiveWordColumns + " FROM sensitive_words WHERE id = $1"
	row := r.pool.QueryRow(ctx, q, id)
	w := &entity.SensitiveWord{}
	err := row.Scan(&w.ID, &w.Word, &w.Category, &w.IsActive, &w.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get sensitive_word: %w", err)
	}
	return w, nil
}

// Update 全量更新（service 层负责合并 patch）。
func (r *SensitiveWordRepo) Update(ctx context.Context, w *entity.SensitiveWord) error {
	q := `UPDATE sensitive_words SET word = $2, category = $3, is_active = $4
		WHERE id = $1`
	tag, err := r.pool.Exec(ctx, q, w.ID, w.Word, w.Category, w.IsActive)
	if err != nil {
		return fmt.Errorf("update sensitive_word: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete 按 ID 删除。未找到返回 ErrNotFound。
func (r *SensitiveWordRepo) Delete(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, "DELETE FROM sensitive_words WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete sensitive_word: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
