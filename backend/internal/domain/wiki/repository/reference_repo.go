package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"health-nexus/internal/domain/wiki/entity"
	"health-nexus/internal/platform/postgres"
	"health-nexus/internal/shared/constants"
)

// ReferenceRepo 引用授权仓储。
type ReferenceRepo struct {
	pool *pgxpool.Pool
}

// NewReferenceRepo 构造引用授权仓储。
func NewReferenceRepo(pool *pgxpool.Pool) *ReferenceRepo {
	return &ReferenceRepo{pool: pool}
}

// ErrDuplicatePending 同一 (article_id, target_dept_id) 已存在 pending 申请时返回。
// 由 uq_article_refs_pending 部分唯一索引触发；service 层翻译为 409。
var ErrDuplicatePending = errors.New("duplicate pending reference")

// Create 插入引用记录。ref.ID/CreatedAt/UpdatedAt 由 RETURNING 回填。
// 公开文章直接引用时 status=approved，同时设置 reviewer_id（=applicant_id）和 approved_at；
// 非公开文章走审批流程时 status=pending，仅设置 applicant_id。
// 唯一冲突（同 article_id+target_dept_id+pending）由 uq_article_refs_pending 索引保障，
// 冲突时返回 ErrDuplicatePending，service 层翻译为 409。
func (r *ReferenceRepo) Create(ctx context.Context, ref *entity.ArticleReference) error {
	var sql string
	var err error
	if ref.Status == constants.ReferenceStatusApproved {
		// 公开文章直接引用：直接创建 approved 状态，设置 approved_at
		sql = `INSERT INTO article_references
			(article_id, source_dept_id, target_dept_id, status, applicant_id, reviewer_id, approved_at)
			VALUES ($1, $2, $3, $4, $5, $6, now())
			RETURNING id, created_at, updated_at`
		err = postgres.Q(ctx, r.pool).QueryRow(ctx, sql,
			ref.ArticleID, ref.SourceDeptID, ref.TargetDeptID, ref.Status, ref.ApplicantID, ref.ApplicantID,
		).Scan(&ref.ID, &ref.CreatedAt, &ref.UpdatedAt)
	} else {
		sql = `INSERT INTO article_references
			(article_id, source_dept_id, target_dept_id, status, applicant_id)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id, created_at, updated_at`
		err = postgres.Q(ctx, r.pool).QueryRow(ctx, sql,
			ref.ArticleID, ref.SourceDeptID, ref.TargetDeptID, ref.Status, ref.ApplicantID,
		).Scan(&ref.ID, &ref.CreatedAt, &ref.UpdatedAt)
	}
	if err != nil && postgres.IsUniqueViolation(err) {
		return ErrDuplicatePending
	}
	return err
}

// GetByID 按 ID 查询引用申请（含 JOIN 名称字段，与 List 行为一致）。未找到返回 ErrNotFound。
func (r *ReferenceRepo) GetByID(ctx context.Context, id int64) (*entity.ArticleReference, error) {
	const sql = `SELECT ref.id, ref.article_id, ref.source_dept_id, ref.target_dept_id, ref.status,
		ref.applicant_id, ref.reviewer_id, ref.review_comment, ref.approved_at, ref.revoked_at,
		ref.created_at, ref.updated_at,
		COALESCE(a.title, ''), COALESCE(sd.name, ''), COALESCE(td.name, ''), COALESCE(u.username, ''),
		COALESCE(a.status, '')
		FROM article_references ref
		LEFT JOIN articles a ON a.id = ref.article_id
		LEFT JOIN departments sd ON sd.id = ref.source_dept_id
		LEFT JOIN departments td ON td.id = ref.target_dept_id
		LEFT JOIN users u ON u.id = ref.applicant_id
		WHERE ref.id = $1`
	ref, err := scanReferenceWithNames(postgres.Q(ctx, r.pool).QueryRow(ctx, sql, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return ref, nil
}

// HasPending 检查同 article_id + target_dept_id 是否已存在 pending 申请。
// 用于 Apply 时防重复（REQ-WIKI-022，409 冲突）。
func (r *ReferenceRepo) HasPending(ctx context.Context, articleID, targetDeptID int64) (bool, error) {
	const sql = `SELECT EXISTS(SELECT 1 FROM article_references
		WHERE article_id = $1 AND target_dept_id = $2 AND status = $3)`
	var exists bool
	err := postgres.Q(ctx, r.pool).
		QueryRow(ctx, sql, articleID, targetDeptID, constants.ReferenceStatusPending).
		Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check pending reference: %w", err)
	}
	return exists, nil
}

// HasApproved 检查同 article_id + target_dept_id 是否已存在 approved 引用。
// 用于公开文章直接引用时防重复。
func (r *ReferenceRepo) HasApproved(ctx context.Context, articleID, targetDeptID int64) (bool, error) {
	const sql = `SELECT EXISTS(SELECT 1 FROM article_references
		WHERE article_id = $1 AND target_dept_id = $2 AND status = $3)`
	var exists bool
	err := postgres.Q(ctx, r.pool).
		QueryRow(ctx, sql, articleID, targetDeptID, constants.ReferenceStatusApproved).
		Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check approved reference: %w", err)
	}
	return exists, nil
}

// RevokeByArticle 批量撤销指定文章的所有 approved 引用（文章删除/归档时调用）。
// 返回受影响的行数。
func (r *ReferenceRepo) RevokeByArticle(ctx context.Context, articleID int64) (int64, error) {
	const sql = `UPDATE article_references
		SET status = $2, revoked_at = now(), updated_at = now()
		WHERE article_id = $1 AND status = $3`
	tag, err := postgres.Q(ctx, r.pool).Exec(ctx, sql,
		articleID, constants.ReferenceStatusRevoked, constants.ReferenceStatusApproved)
	if err != nil {
		return 0, fmt.Errorf("revoke references by article: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ListFilter 引用列表过滤参数。零值表示不过滤。
type ListFilter struct {
	Status      string
	Direction   string  // constants.ReferenceDirectionOutgoing/Incoming；空表示两者都查
	CurrentDept int64   // 当前用户科室，用于方向过滤与 DEPT_ADMIN 隔离
	DeptIDs     []int64 // 可见科室集合（SUPER_ADMIN 为 nil 表示全部）；非 nil 时限制 source/target 在集合内
}

// List 列出引用申请（含 JOIN 名称），分页。total 为符合条件的总数。
func (r *ReferenceRepo) List(
	ctx context.Context, f ListFilter, limit, offset int,
) ([]*entity.ArticleReference, int64, error) {
	where, args := buildListWhere(f)

	var total int64
	countSQL := `SELECT count(*) FROM article_references ref WHERE ` + where
	if err := postgres.Q(ctx, r.pool).QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count references: %w", err)
	}

	args = append(args, limit, offset)
	listSQL := fmt.Sprintf(`SELECT ref.id, ref.article_id, ref.source_dept_id, ref.target_dept_id, ref.status,
		ref.applicant_id, ref.reviewer_id, ref.review_comment, ref.approved_at, ref.revoked_at,
		ref.created_at, ref.updated_at,
		COALESCE(a.title, ''), COALESCE(sd.name, ''), COALESCE(td.name, ''), COALESCE(u.username, ''),
		COALESCE(a.status, '')
		FROM article_references ref
		LEFT JOIN articles a ON a.id = ref.article_id
		LEFT JOIN departments sd ON sd.id = ref.source_dept_id
		LEFT JOIN departments td ON td.id = ref.target_dept_id
		LEFT JOIN users u ON u.id = ref.applicant_id
		WHERE %s
		ORDER BY ref.created_at DESC
		LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args))
	rows, err := postgres.Q(ctx, r.pool).Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list references: %w", err)
	}
	defer rows.Close()

	out := make([]*entity.ArticleReference, 0)
	for rows.Next() {
		ref, err := scanReferenceWithNames(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, ref)
	}
	return out, total, rows.Err()
}

// UpdateStatus 引用状态迁移（乐观锁：仅当 fromStatus 匹配时更新）。
// 不存在或状态不匹配返回 ErrNotFound（service 层区分 404/409）。
func (r *ReferenceRepo) UpdateStatus(
	ctx context.Context, id int64, fromStatus, toStatus string, opts RefStatusOpts,
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
	if opts.SetApprovedAt {
		sets = append(sets, "approved_at = now()")
	}
	if opts.SetRevokedAt {
		sets = append(sets, "revoked_at = now()")
	}
	sql := fmt.Sprintf(`UPDATE article_references SET %s WHERE id = $1 AND status = $2`,
		strings.Join(sets, ", "))
	tag, err := postgres.Q(ctx, r.pool).Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("update reference status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RefStatusOpts 引用状态更新附加选项。
type RefStatusOpts struct {
	ReviewerID    *int64
	ReviewComment *string
	SetApprovedAt bool // toStatus=approved 时设 approved_at=now()
	SetRevokedAt  bool // toStatus=revoked 时设 revoked_at=now()
}

func scanReferenceWithNames(s postgres.Scanner) (*entity.ArticleReference, error) {
	ref := &entity.ArticleReference{}
	err := s.Scan(
		&ref.ID, &ref.ArticleID, &ref.SourceDeptID, &ref.TargetDeptID, &ref.Status,
		&ref.ApplicantID, &ref.ReviewerID, &ref.ReviewComment, &ref.ApprovedAt, &ref.RevokedAt,
		&ref.CreatedAt, &ref.UpdatedAt,
		&ref.ArticleTitle, &ref.SourceDeptName, &ref.TargetDeptName, &ref.ApplicantName,
		&ref.SourceArticleStatus,
	)
	if err != nil {
		return nil, fmt.Errorf("scan reference with names: %w", err)
	}
	return ref, nil
}
