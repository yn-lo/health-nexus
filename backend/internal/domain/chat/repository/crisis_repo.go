package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"health-nexus/internal/domain/chat/entity"
	"health-nexus/internal/platform/postgres"
)

// ErrNotFound 危机事件未找到。与 wiki repo 模式一致，service 层用 errors.Is 判断（D-MED-05 修复）。
var ErrNotFound = errors.New("crisis event not found")

// CrisisFilter 危机事件列表过滤条件。指针字段为 nil 表示不过滤。
type CrisisFilter struct {
	Handled *bool  // 是否已处理
	Level   string // 级别（空字符串表示不过滤）
	DeptID  int64  // 科室 ID（0 表示不过滤），通过 conversations.locked_dept_id 匹配
}

// CrisisListRow 列表行（含患者用户名，从 JOIN users 取得）。
type CrisisListRow struct {
	entity.CrisisEvent
	PatientName string
}

// CrisisRepo 危机事件仓储。
type CrisisRepo struct {
	pool *pgxpool.Pool
}

// NewCrisisRepo 构造危机事件仓储。
func NewCrisisRepo(pool *pgxpool.Pool) *CrisisRepo {
	return &CrisisRepo{pool: pool}
}

// Create 创建危机事件，返回新记录 ID。
func (r *CrisisRepo) Create(ctx context.Context, e *entity.CrisisEvent) (int64, error) {
	const sql = `INSERT INTO crisis_events
	             (patient_id, conversation_id, message_id, triggered_content, matched_keywords, level)
	             VALUES ($1, $2, $3, $4, $5, $6)
	             RETURNING id`
	var id int64
	row := postgres.Q(ctx, r.pool).QueryRow(ctx, sql,
		e.PatientID, e.ConversationID, e.MessageID, e.TriggeredContent, e.MatchedKeywords, e.Level)
	if err := row.Scan(&id); err != nil {
		return 0, fmt.Errorf("create crisis event: %w", err)
	}
	return id, nil
}

// GetByID 按 ID 取危机事件；不存在返回 (nil, ErrNotFound)（D-MED-05 修复，对齐 wiki repo 哨兵错误模式）。
// LockedDeptID 派生自 conversations.locked_dept_id（未锁定科室为 0），供科室归属校验。
func (r *CrisisRepo) GetByID(ctx context.Context, id int64) (*entity.CrisisEvent, error) {
	const sql = `SELECT c.id, c.patient_id, c.conversation_id, c.message_id, c.triggered_content, c.matched_keywords, c.level,
	                    c.is_handled, c.handler_id, c.handled_at, c.handle_note, c.created_at,
	                    COALESCE(conv.locked_dept_id, 0)
	             FROM crisis_events c
	             LEFT JOIN conversations conv ON conv.id = c.conversation_id
	             WHERE c.id = $1`
	e := &entity.CrisisEvent{}
	row := postgres.Q(ctx, r.pool).QueryRow(ctx, sql, id)
	if err := row.Scan(
		&e.ID, &e.PatientID, &e.ConversationID, &e.MessageID,
		&e.TriggeredContent, &e.MatchedKeywords, &e.Level,
		&e.IsHandled, &e.HandlerID, &e.HandledAt, &e.HandleNote, &e.CreatedAt,
		&e.LockedDeptID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get crisis event: %w", err)
	}
	return e, nil
}

// List 列出危机事件（含患者用户名）。按 created_at DESC 排序。
// 当 filter.DeptID > 0 时，通过 JOIN conversations 按 locked_dept_id 过滤科室。
func (r *CrisisRepo) List(
	ctx context.Context, filter CrisisFilter, limit, offset int,
) ([]*CrisisListRow, int64, error) {
	where := []string{}
	args := []any{}
	joinClause := ""
	if filter.Handled != nil {
		args = append(args, *filter.Handled)
		where = append(where, fmt.Sprintf("c.is_handled = $%d", len(args)))
	}
	if filter.Level != "" {
		args = append(args, filter.Level)
		where = append(where, fmt.Sprintf("c.level = $%d", len(args)))
	}
	if filter.DeptID > 0 {
		args = append(args, filter.DeptID)
		where = append(where, fmt.Sprintf("conv.locked_dept_id = $%d", len(args)))
		joinClause = " JOIN conversations conv ON conv.id = c.conversation_id"
	}
	whereSQL := "1=1"
	if len(where) > 0 {
		whereSQL = strings.Join(where, " AND ")
	}

	var total int64
	countSQL := fmt.Sprintf(`SELECT count(*) FROM crisis_events c%s WHERE %s`, joinClause, whereSQL)
	if err := postgres.Q(ctx, r.pool).QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count crisis events: %w", err)
	}

	args = append(args, limit, offset)
	listSQL := fmt.Sprintf(
		`SELECT c.id, c.patient_id, c.conversation_id, c.message_id,
		        c.triggered_content, c.matched_keywords, c.level,
		        c.is_handled, c.handler_id, c.handled_at, c.handle_note, c.created_at,
		        u.username AS patient_name
		 FROM crisis_events c
		 LEFT JOIN users u ON u.id = c.patient_id
		 %s
		 WHERE %s
		 ORDER BY c.created_at DESC LIMIT $%d OFFSET $%d`,
		joinClause, whereSQL, len(args)-1, len(args))
	rows, err := postgres.Q(ctx, r.pool).Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list crisis events: %w", err)
	}
	defer rows.Close()
	out := []*CrisisListRow{}
	for rows.Next() {
		row := &CrisisListRow{}
		if err := rows.Scan(
			&row.CrisisEvent.ID, &row.CrisisEvent.PatientID,
			&row.CrisisEvent.ConversationID, &row.CrisisEvent.MessageID,
			&row.CrisisEvent.TriggeredContent, &row.CrisisEvent.MatchedKeywords,
			&row.CrisisEvent.Level, &row.CrisisEvent.IsHandled,
			&row.CrisisEvent.HandlerID, &row.CrisisEvent.HandledAt,
			&row.CrisisEvent.HandleNote, &row.CrisisEvent.CreatedAt,
			&row.PatientName,
		); err != nil {
			return nil, 0, fmt.Errorf("scan crisis event: %w", err)
		}
		out = append(out, row)
	}
	return out, total, rows.Err()
}

// MarkHandled 标记事件已处理。已处理返回 (false, nil) 供 Handler 返回 409。
func (r *CrisisRepo) MarkHandled(ctx context.Context, id, handlerID int64, note string) (bool, error) {
	const sql = `UPDATE crisis_events SET is_handled = TRUE, handler_id = $2, handled_at = now(), handle_note = $3
	             WHERE id = $1 AND is_handled = FALSE`
	tag, err := postgres.Q(ctx, r.pool).Exec(ctx, sql, id, handlerID, note)
	if err != nil {
		return false, fmt.Errorf("mark crisis handled: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
