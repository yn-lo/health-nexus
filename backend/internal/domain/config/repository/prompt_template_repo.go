package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"health-nexus/internal/domain/config/entity"
	"health-nexus/internal/platform/postgres"
	"health-nexus/internal/shared/pagination"
)

// PromptTemplateRepo 对应 prompt_templates 表。
type PromptTemplateRepo struct {
	pool *pgxpool.Pool
}

// NewPromptTemplateRepo 创建 PromptTemplateRepo。
func NewPromptTemplateRepo(pool *pgxpool.Pool) *PromptTemplateRepo {
	return &PromptTemplateRepo{pool: pool}
}

const promptTemplateColumns = `id, type, version, content, is_active,` +
	` description, department_id, created_at, updated_at`

// List 按 type 和 is_active 过滤（type 空 / isActive nil 表示不过滤），返回分页结果。
// department_id 不作为列表过滤条件（前端按 type 查询即可，科室覆盖由 service 层选择时优先）。
func (r *PromptTemplateRepo) List(
	ctx context.Context, promptType string, isActive *bool, p pagination.Params,
) ([]*entity.PromptTemplate, int64, error) {
	where := " WHERE 1=1"
	args := []any{}
	if promptType != "" {
		args = append(args, promptType)
		where += " AND type = $" + fmt.Sprintf("%d", len(args))
	}
	if isActive != nil {
		args = append(args, *isActive)
		where += " AND is_active = $" + fmt.Sprintf("%d", len(args))
	}

	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM prompt_templates"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count prompt_templates: %w", err)
	}

	listQ := "SELECT " + promptTemplateColumns + " FROM prompt_templates" + where +
		" ORDER BY type, version DESC LIMIT $" + fmt.Sprintf("%d", len(args)+1) +
		" OFFSET $" + fmt.Sprintf("%d", len(args)+2)
	args = append(args, p.PageSize, p.Offset())

	rows, err := r.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query prompt_templates: %w", err)
	}
	defer rows.Close()

	var out []*entity.PromptTemplate
	for rows.Next() {
		t, err := scanPromptTemplate(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, t)
	}
	return out, total, rows.Err()
}

// Get 按 ID 查询。未找到返回 ErrNotFound。
func (r *PromptTemplateRepo) Get(ctx context.Context, id int64) (*entity.PromptTemplate, error) {
	q := "SELECT " + promptTemplateColumns + " FROM prompt_templates WHERE id = $1"
	row := r.pool.QueryRow(ctx, q, id)
	t, err := scanPromptTemplate(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

// Create 插入新版本。Version 由 SQL 子查询自动递增（同 type + department_id 下 max+1）。
// is_active=true 时先 UPDATE 失活同 type + department_id 范围其他 active 行，再 INSERT 新 active 行。
// UNIQUE(type, version) 冲突由 service 层翻译为 409。
//
// 分两条独立语句而非单条 CTE：CTE 内 UPDATE 和 INSERT 共享同一 snapshot，
// INSERT 时旧 active 行仍在 partial unique index uq_prompt_templates_active_per_type_dept 中，
// 触发 23505。两条独立语句顺序执行可避开 snapshot 限制。
// $1::varchar 显式标注类型：避免 PostgreSQL 在 `VALUES ($1)` 推断 varchar 与
// `WHERE type = $1` 推断 text 之间产生 "inconsistent types deduced" 错误（SQLSTATE 42P08）。
//
// ponytail: 未包裹事务——若第二步 INSERT 失败，已失活的兄弟行无法回滚（admin 可重试），折中。
// 升级路径：service 层用 TxManager.WithTx 包裹两条语句实现原子化。
func (r *PromptTemplateRepo) Create(ctx context.Context, t *entity.PromptTemplate) error {
	if t.IsActive {
		if _, err := r.pool.Exec(ctx,
			`UPDATE prompt_templates SET is_active = FALSE, updated_at = now()
			 WHERE type = $1
			   AND COALESCE(department_id, 0) = COALESCE($2, 0)
			   AND is_active = TRUE`,
			t.Type, t.DepartmentID,
		); err != nil {
			return fmt.Errorf("deactivate siblings: %w", err)
		}
	}
	q := `INSERT INTO prompt_templates (type, version, content, is_active, description, department_id)
		VALUES ($1,
			(SELECT COALESCE(MAX(version), 0) + 1 FROM prompt_templates
			 WHERE type = $1::varchar AND COALESCE(department_id, 0) = COALESCE($5, 0)),
			$2, $3, $4, $5)
		RETURNING ` + promptTemplateColumns
	tmp, err := scanPromptTemplate(r.pool.QueryRow(ctx, q,
		t.Type, t.Content, t.IsActive, t.Description, t.DepartmentID,
	))
	if err != nil {
		return err
	}
	t.ID = tmp.ID
	t.Version = tmp.Version
	t.CreatedAt = tmp.CreatedAt
	t.UpdatedAt = tmp.UpdatedAt
	return nil
}

// UpdateContentAndActive 更新 content 和/或 is_active。
// is_active 由 false→true 时，同 type + department_id 下其他版本自动失活（契约 §6.6.3）。
//
// 激活路径分两条独立语句执行：先 UPDATE 失活兄弟，再 UPDATE 激活目标。
//
//	原 CTE 单语句 UPDATE 在 partial unique index uq_prompt_templates_active_per_type_dept 上
//	触发 23505：单 UPDATE 内同时激活目标 + 失活旧 active 行，索引检查以 snapshot 为准，
//	旧 active 行仍在索引中。
//	content 为 nil 时 pgx 发送无类型 NULL，$2::text cast 仍报 "could not determine data type"
//	（SQLSTATE 42P08），故 content=nil 与非 nil 走两条 SQL。
//
// ponytail: 未包裹事务——若第二步 UPDATE 失败，兄弟已失活无法回滚（admin 可重试），折中。
// 升级路径：service 层用 TxManager.WithTx 包裹两条语句实现原子化。
func (r *PromptTemplateRepo) UpdateContentAndActive(
	ctx context.Context, id int64, content *string, isActive *bool,
) (*entity.PromptTemplate, error) {
	activate := isActive != nil && *isActive

	if activate {
		// 第一步：失活同 type+department_id 范围的其他 active 行。
		//   子查询读目标的 type+dept：目标不存在时子查询返回 NULL，WHERE 不匹配，0 行受影响（安全）。
		if _, err := r.pool.Exec(ctx,
			`UPDATE prompt_templates SET is_active = FALSE, updated_at = now()
			 WHERE type = (SELECT type FROM prompt_templates WHERE id = $1)
			   AND COALESCE(department_id, 0) = COALESCE((SELECT department_id FROM prompt_templates WHERE id = $1), 0)
			   AND is_active = TRUE
			   AND id <> $1`,
			id,
		); err != nil {
			return nil, fmt.Errorf("deactivate siblings: %w", err)
		}
		// 第二步：激活目标行。content 非空时同步更新 content。
		//   分两条 SQL：content 为 nil 时 pgx 发送无类型 NULL，即使 $2::text cast 也会报 42P08。
		//   目标不存在时 0 行受影响，QueryRow 返回 ErrNoRows → ErrNotFound。
		var q string
		var args []any
		if content != nil {
			q = `UPDATE prompt_templates SET
					is_active = TRUE,
					content = $2,
					updated_at = now()
				WHERE id = $1
				RETURNING ` + promptTemplateColumns
			args = []any{id, *content}
		} else {
			q = `UPDATE prompt_templates SET
					is_active = TRUE,
					updated_at = now()
				WHERE id = $1
				RETURNING ` + promptTemplateColumns
			args = []any{id}
		}
		t, err := scanPromptTemplate(r.pool.QueryRow(ctx, q, args...))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("update prompt_template: %w", err)
		}
		return t, nil
	}

	// 非激活路径：仅更新目标行。content/is_active 任一为 nil 时跳过该字段。
	sets := []string{"updated_at = now()"}
	args := []any{}
	if content != nil {
		args = append(args, *content)
		sets = append(sets, fmt.Sprintf("content = $%d", len(args)))
	}
	if isActive != nil {
		args = append(args, *isActive)
		sets = append(sets, fmt.Sprintf("is_active = $%d", len(args)))
	}
	args = append(args, id)
	sql := fmt.Sprintf(`UPDATE prompt_templates SET %s WHERE id = $%d RETURNING %s`,
		strings.Join(sets, ", "), len(args), promptTemplateColumns)

	t, err := scanPromptTemplate(r.pool.QueryRow(ctx, sql, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update prompt_template: %w", err)
	}
	return t, nil
}

// Delete 按 ID 删除任意状态的 prompt 模板。
// 即使 is_active=true 也可删除——系统有硬编码 DefaultSystemPrompt 兜底，删除后自动回退。
// 未找到行时返回 ErrNotFound。
func (r *PromptTemplateRepo) Delete(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx,
		"DELETE FROM prompt_templates WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete prompt_template: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// IsActive 返回指定 ID 的 is_active 状态。未找到返回 ErrNotFound。
func (r *PromptTemplateRepo) IsActive(ctx context.Context, id int64) (bool, error) {
	var isActive bool
	err := r.pool.QueryRow(ctx, "SELECT is_active FROM prompt_templates WHERE id = $1", id).Scan(&isActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("query is_active: %w", err)
	}
	return isActive, nil
}

func scanPromptTemplate(s postgres.Scanner) (*entity.PromptTemplate, error) {
	t := &entity.PromptTemplate{}
	err := s.Scan(
		&t.ID, &t.Type, &t.Version, &t.Content, &t.IsActive, &t.Description, &t.DepartmentID,
		&t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan prompt_template: %w", err)
	}
	return t, nil
}
