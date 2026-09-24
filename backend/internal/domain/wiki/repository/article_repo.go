// Package repository 实现 wiki 域的持久化（手写 SQL + pgx）。
// 事务由 Service 层通过 postgres.TxFromCtx(ctx) 注入；无事务时回退到 pool。
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"health-nexus/internal/domain/wiki/entity"
	"health-nexus/internal/platform/postgres"
	"health-nexus/internal/shared/constants"
)

// ErrNotFound 通用"未找到"哨兵错误，service 层翻译为 404。
var ErrNotFound = errors.New("article not found")

// ErrVersionConflict 乐观锁冲突：带 ExpectedVersion 更新时当前版本与期望版本不一致（并发编辑）。
// service 层翻译为 409。
var ErrVersionConflict = errors.New("article version conflict")

// articleColumns articles 表的核心列（不含 view 字段），用于 INSERT/SELECT。
const articleColumns = `id, title, content, summary, cover_image_url, status, version,
	content_hash, author_id, department_id, reviewer_id, review_comment, view_count, featured_rank,
	is_deleted, allow_reference, review_overdue, review_overdue_at, published_at,
	source, applicable_population, valid_until, content_risk,
	created_at, updated_at`

// ArticleRepo 文章仓储。
type ArticleRepo struct {
	pool *pgxpool.Pool
}

// NewArticleRepo 构造文章仓储。
func NewArticleRepo(pool *pgxpool.Pool) *ArticleRepo {
	return &ArticleRepo{pool: pool}
}

// Create 插入新文章。a.ID/a.CreatedAt/a.UpdatedAt/DepartmentName/AuthorName 由 RETURNING + JOIN 回填。
func (r *ArticleRepo) Create(ctx context.Context, a *entity.Article) error {
	const sql = `WITH ins AS (
		INSERT INTO articles
		(title, content, summary, cover_image_url, status, version, content_hash,
		 author_id, department_id, allow_reference,
		 source, applicable_population, valid_until, content_risk)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING ` + articleColumns + `
	)
	SELECT i.id, i.title, i.content, i.summary, i.cover_image_url, i.status, i.version,
		i.content_hash, i.author_id, i.department_id, i.reviewer_id, i.review_comment, i.view_count, i.featured_rank,
		i.is_deleted, i.allow_reference, i.review_overdue, i.review_overdue_at, i.published_at,
		i.source, i.applicable_population, i.valid_until, i.content_risk,
		i.created_at, i.updated_at,
		COALESCE(d.name, ''), COALESCE(u.username, '')
	FROM ins i
	LEFT JOIN departments d ON d.id = i.department_id
	LEFT JOIN users u ON u.id = i.author_id`
	return postgres.Q(ctx, r.pool).QueryRow(ctx, sql,
		a.Title, a.Content, a.Summary, a.CoverImageURL, a.Status, a.Version, a.ContentHash,
		a.AuthorID, a.DepartmentID, a.AllowReference,
		a.Source, a.ApplicablePopulation, a.ValidUntil, contentRiskOrDefault(a.ContentRisk),
	).Scan(
		&a.ID, &a.Title, &a.Content, &a.Summary, &a.CoverImageURL, &a.Status, &a.Version,
		&a.ContentHash, &a.AuthorID, &a.DepartmentID, &a.ReviewerID, &a.ReviewComment, &a.ViewCount, &a.FeaturedRank,
		&a.IsDeleted, &a.AllowReference, &a.ReviewOverdue, &a.ReviewOverdueAt, &a.PublishedAt,
		&a.Source, &a.ApplicablePopulation, &a.ValidUntil, &a.ContentRisk,
		&a.CreatedAt, &a.UpdatedAt,
		&a.DepartmentName, &a.AuthorName,
	)
}

// GetByID 按 ID 查询文章（含所有状态，供 staff 端使用）。
// 未找到返回 (nil, ErrNotFound)。
func (r *ArticleRepo) GetByID(ctx context.Context, id int64) (*entity.Article, error) {
	sql := `SELECT ` + articleColumns + ` FROM articles WHERE id = $1 AND is_deleted = false`
	a, err := scanArticle(postgres.Q(ctx, r.pool).QueryRow(ctx, sql, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

// LockVersion 事务内锁定文章行（FOR UPDATE）并返回当前版本。
// 供向量化 Worker 在写入切片前复核版本：并发发布/更新会在锁上排队，
// 保证同文章的"读版本 → 写切片"串行化，防止旧任务覆盖新版本切片。
// 必须在调用方事务内执行（postgres.Q 取 ctx 事务）；未找到返回 ErrNotFound。
func (r *ArticleRepo) LockVersion(ctx context.Context, id int64) (int, error) {
	const sql = `SELECT version FROM articles WHERE id = $1 AND is_deleted = false FOR UPDATE`
	var v int
	err := postgres.Q(ctx, r.pool).QueryRow(ctx, sql, id).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("lock article version: %w", err)
	}
	return v, nil
}

// GetPublishedByID 取已发布文章详情（含 department_name/author_name JOIN）。
// 原子 +1 阅读量（CTE UPDATE+JOIN）；契约 §4.2 规定每次访问 +1，未定义去重。
// 可见性与检索层对齐：published，或 pending 且曾发布（published_at 非空）——
// 后者是"已发布文章被修改、正在重新审核"，其旧切片仍被 RAG 引用，原文必须可读，
// 否则点击引用会 404（P2）。draft/archived/deleted 一律不可见。
// 未找到（不存在/不可见/已删除）返回 (nil, ErrNotFound)。
func (r *ArticleRepo) GetPublishedByID(ctx context.Context, id int64) (*entity.Article, error) {
	const sql = `WITH bumped AS (
		UPDATE articles SET view_count = view_count + 1, updated_at = now()
		WHERE id = $1 AND is_deleted = false
		  AND (status = $2 OR (status = $3 AND published_at IS NOT NULL))
		RETURNING ` + articleColumns + `
	)
	SELECT b.id, b.title, b.content, b.summary, b.cover_image_url, b.status, b.version,
		b.content_hash, b.author_id, b.department_id, b.reviewer_id, b.review_comment, b.view_count, b.featured_rank,
		b.is_deleted, b.allow_reference, b.review_overdue, b.review_overdue_at, b.published_at,
		b.source, b.applicable_population, b.valid_until, b.content_risk,
		b.created_at, b.updated_at,
		COALESCE(d.name, ''), COALESCE(u.username, '')
	FROM bumped b
	LEFT JOIN departments d ON d.id = b.department_id
	LEFT JOIN users u ON u.id = b.author_id`
	row := postgres.Q(ctx, r.pool).QueryRow(ctx, sql, id,
		constants.ArticleStatusPublished, constants.ArticleStatusPending)
	a, err := scanArticleWithNames(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

// ListPublishedFilter 已发布文章列表过滤参数。DepartmentID 为 nil 表示不限定科室。
type ListPublishedFilter struct {
	DepartmentID   *int64
	AllowReference *bool  // nil = 不限定；true = 仅公开文章
	ExcludeDeptID  *int64 // nil = 不排除；非 nil = 排除指定科室的文章
	Search         string // 非空时按 title/summary ILIKE 模糊匹配（前端搜索）
}

// ListPublished 列出已发布文章（含 department_name JOIN），分页。total 为符合条件的总数。
func (r *ArticleRepo) ListPublished(
	ctx context.Context, f ListPublishedFilter, limit, offset int,
) ([]*entity.Article, int64, error) {
	where := "a.status = $1 AND a.is_deleted = false"
	args := []any{constants.ArticleStatusPublished}
	if f.DepartmentID != nil {
		args = append(args, *f.DepartmentID)
		where += fmt.Sprintf(" AND a.department_id = $%d", len(args))
	}
	if f.AllowReference != nil {
		args = append(args, *f.AllowReference)
		where += fmt.Sprintf(" AND a.allow_reference = $%d", len(args))
	}
	if f.ExcludeDeptID != nil {
		args = append(args, *f.ExcludeDeptID)
		where += fmt.Sprintf(" AND a.department_id != $%d", len(args))
	}
	if s := strings.TrimSpace(f.Search); s != "" {
		args = append(args, "%"+s+"%")
		where += fmt.Sprintf(" AND (a.title ILIKE $%d OR a.summary ILIKE $%d)", len(args), len(args))
	}

	var total int64
	countSQL := `SELECT count(*) FROM articles a WHERE ` + where
	if err := postgres.Q(ctx, r.pool).QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count published articles: %w", err)
	}

	args = append(args, limit, offset)
	listSQL := fmt.Sprintf(`SELECT a.id, a.title, '' AS content, a.summary, a.cover_image_url, a.status, a.version,
		a.content_hash, a.author_id, a.department_id, a.reviewer_id, a.review_comment, a.view_count, a.featured_rank,
		a.is_deleted, a.allow_reference, a.review_overdue, a.review_overdue_at, a.published_at,
		a.source, a.applicable_population, a.valid_until, a.content_risk,
		a.created_at, a.updated_at,
		COALESCE(d.name, ''), COALESCE(u.username, '')
	FROM articles a
	LEFT JOIN departments d ON d.id = a.department_id
	LEFT JOIN users u ON u.id = a.author_id
	WHERE %s
	ORDER BY a.published_at DESC NULLS LAST, a.created_at DESC
	LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args))
	rows, err := postgres.Q(ctx, r.pool).Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list published articles: %w", err)
	}
	defer rows.Close()

	out := make([]*entity.Article, 0)
	for rows.Next() {
		a, err := scanArticleWithNames(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}

func (r *ArticleRepo) ListFeatured(ctx context.Context, departmentID *int64, limit int) ([]*entity.Article, error) {
	where := "a.status = $1 AND a.is_deleted = false"
	args := []any{constants.ArticleStatusPublished}
	if departmentID != nil {
		args = append(args, *departmentID)
		where += fmt.Sprintf(" AND a.department_id = $%d", len(args))
	}
	args = append(args, limit)
	listSQL := fmt.Sprintf(`SELECT a.id, a.title, '' AS content, a.summary, a.cover_image_url, a.status, a.version,
		a.content_hash, a.author_id, a.department_id, a.reviewer_id, a.review_comment, a.view_count, a.featured_rank,
		a.is_deleted, a.allow_reference, a.review_overdue, a.review_overdue_at, a.published_at,
		a.source, a.applicable_population, a.valid_until, a.content_risk,
		a.created_at, a.updated_at,
		COALESCE(d.name, ''), COALESCE(u.username, '')
	FROM articles a
	LEFT JOIN departments d ON d.id = a.department_id
	LEFT JOIN users u ON u.id = a.author_id
	WHERE %s
	ORDER BY CASE WHEN a.featured_rank > 0 THEN 0 ELSE 1 END,
			a.featured_rank ASC, a.view_count DESC, a.published_at DESC NULLS LAST
		LIMIT $%d`, where, len(args))
	rows, err := postgres.Q(ctx, r.pool).Query(ctx, listSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("list featured articles: %w", err)
	}
	defer rows.Close()
	out := make([]*entity.Article, 0, limit)
	for rows.Next() {
		a, err := scanArticleWithNames(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListStaffFilter staff 端文章列表过滤参数。Status/DepartmentID 为空/nil 表示不限定。
type ListStaffFilter struct {
	Status       string
	DepartmentID *int64
}

// ListForStaff 列出 staff 可见的文章（含 department_name JOIN），分页。
// 调用方负责传入 DepartmentID 进行数据隔离（REQ-SEC-001）。
func (r *ArticleRepo) ListForStaff(
	ctx context.Context, f ListStaffFilter, limit, offset int,
) ([]*entity.Article, int64, error) {
	where := "a.is_deleted = false"
	args := []any{}
	// status=deleted 时改用 is_deleted=true 过滤：SoftDelete 同时写 is_deleted=true + status=deleted，
	// is_deleted 已隐含 status，无需叠加 a.status='deleted'（避免双重过滤冗余）。
	if f.Status == constants.ArticleStatusDeleted {
		where = "a.is_deleted = true"
	} else if f.Status != "" {
		args = append(args, f.Status)
		where += fmt.Sprintf(" AND a.status = $%d", len(args))
	}
	if f.DepartmentID != nil {
		args = append(args, *f.DepartmentID)
		where += fmt.Sprintf(" AND a.department_id = $%d", len(args))
	}

	var total int64
	countSQL := `SELECT count(*) FROM articles a WHERE ` + where
	if err := postgres.Q(ctx, r.pool).QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count staff articles: %w", err)
	}

	args = append(args, limit, offset)
	listSQL := fmt.Sprintf(`SELECT a.id, a.title, '' AS content, a.summary, a.cover_image_url, a.status, a.version,
		a.content_hash, a.author_id, a.department_id, a.reviewer_id, a.review_comment, a.view_count, a.featured_rank,
		a.is_deleted, a.allow_reference, a.review_overdue, a.review_overdue_at, a.published_at,
		a.source, a.applicable_population, a.valid_until, a.content_risk,
		a.created_at, a.updated_at,
		COALESCE(d.name, ''), COALESCE(u.username, '')
	FROM articles a
	LEFT JOIN departments d ON d.id = a.department_id
	LEFT JOIN users u ON u.id = a.author_id
	WHERE %s
	ORDER BY a.created_at DESC
	LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args))
	rows, err := postgres.Q(ctx, r.pool).Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list staff articles: %w", err)
	}
	defer rows.Close()

	out := make([]*entity.Article, 0)
	for rows.Next() {
		a, err := scanArticleWithNames(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}

// UpdateFields 更新文章可变字段（不含阅读量）。仅更新非 nil 字段。
// 内容变更（ContentHash 非 nil）时按行当前状态判定：published → pending 并递增版本（重新审核，P1）。
// 返回更新后的实体（含 department_name/author_name JOIN）；不存在返回 ErrNotFound。
func (r *ArticleRepo) UpdateFields(ctx context.Context, id int64, fields UpdateFields) (*entity.Article, error) {
	b := &updateBuilder{sets: []string{"updated_at = now()"}}
	b.applyUpdateFields(fields)

	// 乐观锁：传入 ExpectedVersion 时追加 version 守卫，防止并发编辑互相覆盖（丢失更新）。
	versionClause := ""
	if fields.ExpectedVersion != nil {
		b.args = append(b.args, *fields.ExpectedVersion)
		versionClause = fmt.Sprintf(" AND version = $%d", len(b.args))
	}

	b.args = append(b.args, id)
	sql := fmt.Sprintf(`WITH updated AS (
		UPDATE articles SET %s WHERE id = $%d AND is_deleted = false%s
		RETURNING `+articleColumns+`
	)
	SELECT u.id, u.title, u.content, u.summary, u.cover_image_url, u.status, u.version,
		u.content_hash, u.author_id, u.department_id, u.reviewer_id, u.review_comment, u.view_count, u.featured_rank,
		u.is_deleted, u.allow_reference, u.review_overdue, u.review_overdue_at, u.published_at,
		u.source, u.applicable_population, u.valid_until, u.content_risk,
		u.created_at, u.updated_at,
		COALESCE(d.name, ''), COALESCE(u2.username, '')
	FROM updated u
	LEFT JOIN departments d ON d.id = u.department_id
	LEFT JOIN users u2 ON u2.id = u.author_id`, strings.Join(b.sets, ", "), len(b.args), versionClause)
	a, err := scanArticleWithNames(postgres.Q(ctx, r.pool).QueryRow(ctx, sql, b.args...))
	if errors.Is(err, pgx.ErrNoRows) {
		// 带版本守卫时 0 行可能是"文章不存在"或"版本已被他人改动"，同事务内二次读取区分。
		if fields.ExpectedVersion != nil {
			if _, gerr := r.GetByID(ctx, id); gerr == nil {
				return nil, ErrVersionConflict
			} else if !errors.Is(gerr, ErrNotFound) {
				return nil, gerr
			}
		}
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

// updateBuilder 累积 UPDATE 的 SET 子句与参数（按序生成 $N 占位符）。
type updateBuilder struct {
	sets []string
	args []any
}

// add 追加 "col = $N" 子句（占位符序号即参数下标 + 1）。
func (b *updateBuilder) add(col string, v any) {
	b.args = append(b.args, v)
	b.sets = append(b.sets, fmt.Sprintf("%s = $%d", col, len(b.args)))
}

// applyUpdateFields 按非 nil 字段追加 SET 子句（顺序固定，便于测试与审阅）。
// ponytail: 逐字段 if 展开而非反射——字段少、类型各异，反射带来的收益不抵可读性损失。
func (b *updateBuilder) applyUpdateFields(f UpdateFields) {
	if f.Title != nil {
		b.add("title", *f.Title)
	}
	if f.Content != nil {
		b.add("content", *f.Content)
	}
	if f.Summary != nil {
		b.add("summary", *f.Summary)
	}
	if f.CoverImageURL != nil {
		b.add("cover_image_url", *f.CoverImageURL)
	}
	if f.AllowReference != nil {
		b.add("allow_reference", *f.AllowReference)
	}
	if f.ContentHash != nil {
		b.add("content_hash", *f.ContentHash)
	}
	if f.Source != nil {
		b.add("source", *f.Source)
	}
	if f.ApplicablePopulation != nil {
		b.add("applicable_population", *f.ApplicablePopulation)
	}
	if f.ValidUntil != nil {
		b.add("valid_until", *f.ValidUntil)
	}
	// ClearValidUntil 显式清空有效期（客户端提交空字符串），与"未提交该字段"区分。
	if f.ClearValidUntil {
		b.sets = append(b.sets, "valid_until = NULL")
	}
	if f.ContentRisk != nil {
		b.add("content_risk", contentRiskOrDefault(*f.ContentRisk))
	}
	// 内容变更不得停留在已发布状态（P1）：状态迁移必须在 UPDATE 语句内按行**当前**状态判定，
	// 不能依赖服务端读取时的状态快照——否则"作者读到 pending、管理员随后审核发布、
	// 作者的写才提交"会让未审核的新正文保持 published 并进入向量化队列，绕过审核。
	// 仅当行当前为 published 时回退 pending 并递增版本；draft/pending/archived 保持原状态与版本。
	if f.ReReviewOnContentChange {
		b.sets = append(b.sets,
			fmt.Sprintf("status = CASE WHEN status = '%s' THEN '%s' ELSE status END",
				constants.ArticleStatusPublished, constants.ArticleStatusPending),
			fmt.Sprintf("version = CASE WHEN status = '%s' THEN version + 1 ELSE version END",
				constants.ArticleStatusPublished))
	} else if f.IncrementVersion {
		b.sets = append(b.sets, "version = version + 1")
	}
}

// UpdateFields 文章更新字段（指针为 nil 表示不更新）。
type UpdateFields struct {
	Title          *string
	Content        *string
	Summary        *string
	CoverImageURL  *string
	AllowReference *bool
	ContentHash    *string
	// ReReviewOnContentChange 本次更新包含内容变更（ContentHash 非 nil）：由 UPDATE 语句按行
	// 当前状态判定——published 则回退 pending 并递增版本（重新审核）。判定必须在 SQL 内完成，
	// 不能依赖服务端读取时的状态快照（P1 并发编辑绕过审核）。
	ReReviewOnContentChange bool
	// 知识条目元数据（P1）。
	Source               *string
	ApplicablePopulation *string
	ValidUntil           *time.Time
	// ClearValidUntil 显式把 valid_until 置空（与 ValidUntil 为 nil 的"不更新"区分）。
	ClearValidUntil  bool
	ContentRisk      *string
	IncrementVersion bool
	ExpectedVersion  *int // 非 nil 时启用乐观锁：仅当当前 version 与之相等才更新，否则 ErrVersionConflict
}

// UpdateStatus 状态机迁移：更新 status，可选设置 reviewer_id/review_comment/published_at。
// 仅当当前状态匹配 fromStatus 时才更新（乐观锁，防并发状态漂移）。
// 不存在或状态不匹配返回 ErrNotFound（service 层据 fromStatus 区分 404/409）。
func (r *ArticleRepo) UpdateStatus(
	ctx context.Context, id int64, fromStatus, toStatus string, opts StatusUpdateOpts,
) error {
	sets := []string{"status = $3", "updated_at = now()"}
	args := []any{id, fromStatus, toStatus}
	if opts.ReviewerID != nil {
		args = append(args, *opts.ReviewerID)
		sets = append(sets, fmt.Sprintf("reviewer_id = $%d", len(args)))
	}
	if opts.ReviewComment != nil {
		args = append(args, *opts.ReviewComment)
		sets = append(sets, fmt.Sprintf("review_comment = $%d", len(args)))
	}
	if opts.SetPublishedAt {
		sets = append(sets, "published_at = now()")
	}
	// 重新审核通过即清除复审逾期标记：否则高风险逾期文章即使重新审核发布，
	// 仍因 review_overdue=true 被检索层排除（P2）。审核动作本身已代表"已重新复审"。
	if opts.ClearReviewOverdue {
		sets = append(sets, "review_overdue = false", "review_overdue_at = NULL")
	}
	sql := fmt.Sprintf(`UPDATE articles SET %s WHERE id = $1 AND status = $2 AND is_deleted = false`,
		strings.Join(sets, ", "))
	tag, err := postgres.Q(ctx, r.pool).Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("update article status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// StatusUpdateOpts 状态更新附加选项。
type StatusUpdateOpts struct {
	ReviewerID     *int64
	ReviewComment  *string
	SetPublishedAt bool // toStatus=published 时设 published_at=now()
	// ClearReviewOverdue 审核通过时清除复审逾期标记（review_overdue=false, review_overdue_at=NULL）。
	ClearReviewOverdue bool
}

func (r *ArticleRepo) SetFeaturedRank(ctx context.Context, id int64, rank int) error {
	const sql = `UPDATE articles
		SET featured_rank = CASE
			WHEN id = $1 THEN $2
			WHEN department_id = (SELECT department_id FROM articles WHERE id = $1)
				AND featured_rank = $2 THEN 0
			ELSE featured_rank
		END,
		updated_at = now()
		WHERE id = $1 OR ($2 > 0 AND department_id = (SELECT department_id FROM articles WHERE id = $1)` +
		` AND featured_rank = $2)`
	tag, err := postgres.Q(ctx, r.pool).Exec(ctx, sql, id, rank)
	if err != nil {
		return fmt.Errorf("set article featured rank: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SoftDelete 软删除（is_deleted=true，status=deleted）。仅对未删除文章生效。
func (r *ArticleRepo) SoftDelete(ctx context.Context, id int64) error {
	tag, err := postgres.Q(ctx, r.pool).Exec(ctx,
		`UPDATE articles SET is_deleted = true, status = $2, updated_at = now()
		 WHERE id = $1 AND is_deleted = false`, id, constants.ArticleStatusDeleted)
	if err != nil {
		return fmt.Errorf("soft delete article: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkOverdue 批量标记复审逾期：按内容风险区分有效期（P1）。
//   - 高风险（用药/检查准备/高风险护理）：发布满 90 天即标记；
//   - 普通宣教：发布满 180 天标记；
//   - 显式设置 valid_until 且已过期：无论风险等级一律标记。
//
// 同事务内 review_overdue=true, review_overdue_at=now()。返回被标记的文章 ID 列表（供调用方入队通知）。
// REQ-WIKI-017/018：定期扫描由 asynq PeriodicTask 触发。
// 仅 published 状态触发复审——archived 已退出公开检索且按 REQ-WIKI-001 是终态，不应再发复审通知。
func (r *ArticleRepo) MarkOverdue(ctx context.Context) ([]int64, error) {
	const sql = `UPDATE articles
		SET review_overdue = true, review_overdue_at = now(), updated_at = now()
		WHERE review_overdue = false
		  AND is_deleted = false
		  AND status = $1
		  AND (
		        (content_risk = $2 AND published_at < now() - interval '90 days')
		     OR (content_risk <> $2 AND published_at < now() - interval '180 days')
		     OR (valid_until IS NOT NULL AND valid_until < now())
		  )
		RETURNING id`
	rows, err := postgres.Q(ctx, r.pool).Query(ctx, sql, constants.ArticleStatusPublished, entity.ContentRiskHigh)
	if err != nil {
		return nil, fmt.Errorf("mark overdue articles: %w", err)
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan overdue article id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// UnmarkOverdue 回滚单篇文章的复审逾期标记（用于通知入队失败时回滚，下次扫描可重试，REQ-WIKI-018）。
// ponytail: 不引入 outbox 表——入队失败时回滚 review_overdue 标志，下次扫描重新选中并重试入队，折中；
// 升级路径：引入 outbox 表保证最终一致性（同时支持 worker 兜底重试）。
func (r *ArticleRepo) UnmarkOverdue(ctx context.Context, id int64) error {
	const sql = `UPDATE articles
		SET review_overdue = false, review_overdue_at = NULL, updated_at = now()
		WHERE id = $1`
	_, err := postgres.Q(ctx, r.pool).Exec(ctx, sql, id)
	if err != nil {
		return fmt.Errorf("unmark overdue article: %w", err)
	}
	return nil
}

// scanArticle 扫描文章核心列（不含 department_name/author_name）。
func scanArticle(s postgres.Scanner) (*entity.Article, error) {
	a := &entity.Article{}
	err := s.Scan(
		&a.ID, &a.Title, &a.Content, &a.Summary, &a.CoverImageURL, &a.Status, &a.Version,
		&a.ContentHash, &a.AuthorID, &a.DepartmentID, &a.ReviewerID, &a.ReviewComment, &a.ViewCount, &a.FeaturedRank,
		&a.IsDeleted, &a.AllowReference, &a.ReviewOverdue, &a.ReviewOverdueAt, &a.PublishedAt,
		&a.Source, &a.ApplicablePopulation, &a.ValidUntil, &a.ContentRisk,
		&a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan article: %w", err)
	}
	return a, nil
}

func scanArticleWithNames(s postgres.Scanner) (*entity.Article, error) {
	a := &entity.Article{}
	err := s.Scan(
		&a.ID, &a.Title, &a.Content, &a.Summary, &a.CoverImageURL, &a.Status, &a.Version,
		&a.ContentHash, &a.AuthorID, &a.DepartmentID, &a.ReviewerID, &a.ReviewComment, &a.ViewCount, &a.FeaturedRank,
		&a.IsDeleted, &a.AllowReference, &a.ReviewOverdue, &a.ReviewOverdueAt, &a.PublishedAt,
		&a.Source, &a.ApplicablePopulation, &a.ValidUntil, &a.ContentRisk,
		&a.CreatedAt, &a.UpdatedAt,
		&a.DepartmentName, &a.AuthorName,
	)
	if err != nil {
		return nil, fmt.Errorf("scan article with names: %w", err)
	}
	return a, nil
}

// contentRiskOrDefault 内容风险等级空值时按普通处理（建库默认值与列默认值一致）。
func contentRiskOrDefault(v string) string {
	if v == "" {
		return entity.ContentRiskNormal
	}
	return v
}
