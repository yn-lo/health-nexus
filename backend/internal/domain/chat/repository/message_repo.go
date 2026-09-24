package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"health-nexus/internal/domain/chat/entity"
	"health-nexus/internal/platform/postgres"
)

// MessageRepo 消息仓储。
type MessageRepo struct {
	pool *pgxpool.Pool
}

// NewMessageRepo 构造消息仓储。
func NewMessageRepo(pool *pgxpool.Pool) *MessageRepo {
	return &MessageRepo{pool: pool}
}

// SaveUserMessage 持久化用户消息（result_code 留空）。turnID 标识本轮生成。
func (r *MessageRepo) SaveUserMessage(
	ctx context.Context, convID, turnID uuid.UUID, content string,
) (*entity.Message, error) {
	return r.save(ctx, convID, turnID, "user", content, "", nil)
}

// SaveAssistant 保存完整 assistant 消息（用于危机/拒答等无 LLM 生成场景）。
func (r *MessageRepo) SaveAssistant(
	ctx context.Context, convID uuid.UUID, content, resultCode string, refs []entity.Reference, turnID uuid.UUID,
) (*entity.Message, error) {
	return r.save(ctx, convID, turnID, "assistant", content, resultCode, refs)
}

// SaveAssistantPlaceholder 保存 assistant 占位消息（空内容，待流式生成后 FinalizeAssistant）。
func (r *MessageRepo) SaveAssistantPlaceholder(
	ctx context.Context, convID, turnID uuid.UUID,
) (*entity.Message, error) {
	return r.save(ctx, convID, turnID, "assistant", "", "", nil)
}

func (r *MessageRepo) save(
	ctx context.Context, convID, turnID uuid.UUID, role, content, resultCode string, refs []entity.Reference,
) (*entity.Message, error) {
	const sql = `INSERT INTO messages (conversation_id, turn_id, role, content, result_code, referenced_chunks)
	             VALUES ($1, $2, $3, $4, $5, $6)
	             RETURNING ` + messageColumns
	refsJSON, err := marshalRefs(refs)
	if err != nil {
		return nil, err
	}
	m := &entity.Message{}
	var refsBytes []byte
	var argTurn *uuid.UUID
	if turnID != uuid.Nil {
		argTurn = &turnID
	}
	row := postgres.Q(ctx, r.pool).QueryRow(ctx, sql, convID, argTurn, role, content, resultCode, refsJSON)
	var scanTurn *uuid.UUID
	// Scan 目标数必须与 RETURNING 列数（messageColumns 共 11 列）一致，
	// 缺列会让所有消息写入直接失败（P0：feedback 漏扫导致登录用户问答全挂）。
	if err := row.Scan(
		&m.ID, &m.ConversationID, &scanTurn, &m.Role, &m.Content,
		&m.ResultCode, &refsBytes, &m.CreatedAt, &m.UpdatedAt, &m.Feedback, &m.Seq,
	); err != nil {
		return nil, fmt.Errorf("save message: %w", err)
	}
	if scanTurn != nil {
		m.TurnID = *scanTurn
	}
	m.ReferencedChunks = unmarshalRefs(refsBytes)
	return m, nil
}

// ListByTurn 列出本轮的全部消息（按 seq 升序：先 user 后 assistant）。
// 幂等重放据此定位本轮结果。未找到返回空切片。
func (r *MessageRepo) ListByTurn(
	ctx context.Context, convID, turnID uuid.UUID,
) ([]*entity.Message, error) {
	const sql = `SELECT ` + messageColumns + `
	             FROM messages
	             WHERE conversation_id = $1 AND turn_id = $2
	             ORDER BY seq ASC`
	return r.queryMessages(ctx, sql, convID, turnID)
}

// FinalizeAssistant 流式生成完成后填充内容、result_code、引用切片。
func (r *MessageRepo) FinalizeAssistant(
	ctx context.Context, id uuid.UUID, content, resultCode string, refs []entity.Reference,
) error {
	refsJSON, err := marshalRefs(refs)
	if err != nil {
		return err
	}
	const sql = `UPDATE messages
	             SET content = $2, result_code = $3, referenced_chunks = $4, updated_at = now()
	             WHERE id = $1`
	_, err = postgres.Q(ctx, r.pool).Exec(ctx, sql, id, content, resultCode, refsJSON)
	if err != nil {
		return fmt.Errorf("finalize assistant: %w", err)
	}
	return nil
}

// UpdateFeedback 更新消息反馈（三态：solved/partial/unsolved）。
// 通过 conversations.patient_id 子查询校验消息属于该患者（数据隔离）；
// 返回受影响行数，0 表示消息不存在或不属于该患者。
func (r *MessageRepo) UpdateFeedback(
	ctx context.Context, messageID uuid.UUID, patientID int64, feedback string,
) (int64, error) {
	const sql = `UPDATE messages SET feedback = $3, updated_at = now()
	             WHERE id = $1
	             AND conversation_id IN (SELECT id FROM conversations WHERE patient_id = $2)`
	tag, err := postgres.Q(ctx, r.pool).Exec(ctx, sql, messageID, patientID, feedback)
	if err != nil {
		return 0, fmt.Errorf("update feedback: %w", err)
	}
	return tag.RowsAffected(), nil
}

// FeedbackCountRow 反馈三态汇总聚合行。
type FeedbackCountRow struct {
	Total    int64
	Solved   int64
	Partial  int64
	Unsolved int64
}

// FeedbackRow 最近反馈条目（医护端统计用）。
type FeedbackRow struct {
	MessageID      uuid.UUID
	ConversationID uuid.UUID
	Feedback       string
	Content        string
	CreatedAt      time.Time
}

// FeedbackSummary 汇总三态反馈计数（仅统计 feedback 非空的消息）。
func (r *MessageRepo) FeedbackSummary(ctx context.Context) (FeedbackCountRow, error) {
	const sql = `SELECT COUNT(*) AS total,
	                    COUNT(*) FILTER (WHERE feedback = 'solved')   AS solved,
	                    COUNT(*) FILTER (WHERE feedback = 'partial')  AS partial,
	                    COUNT(*) FILTER (WHERE feedback = 'unsolved') AS unsolved
	             FROM messages
	             WHERE feedback IS NOT NULL`
	var row FeedbackCountRow
	err := postgres.Q(ctx, r.pool).
		QueryRow(ctx, sql).
		Scan(&row.Total, &row.Solved, &row.Partial, &row.Unsolved)
	if err != nil {
		return FeedbackCountRow{}, fmt.Errorf("feedback summary: %w", err)
	}
	return row, nil
}

// RecentFeedback 最近带反馈的消息（按反馈时间 updated_at 倒序，最多 limit 条）。
func (r *MessageRepo) RecentFeedback(ctx context.Context, limit int) ([]FeedbackRow, error) {
	const sql = `SELECT id, conversation_id, feedback, content, created_at
	             FROM messages
	             WHERE feedback IS NOT NULL
	             ORDER BY updated_at DESC
	             LIMIT $1`
	rows, err := postgres.Q(ctx, r.pool).Query(ctx, sql, limit)
	if err != nil {
		return nil, fmt.Errorf("recent feedback: %w", err)
	}
	defer rows.Close()
	out := make([]FeedbackRow, 0)
	for rows.Next() {
		var row FeedbackRow
		if err := rows.Scan(
			&row.MessageID, &row.ConversationID, &row.Feedback, &row.Content, &row.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan feedback row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate feedback rows: %w", err)
	}
	return out, nil
}

// messageColumns 消息查询列（各查询 Scan 顺序一致）。
const messageColumns = `id, conversation_id, turn_id, role, content, result_code,
	referenced_chunks, created_at, updated_at, feedback, seq`

// ListByConversation 列出会话消息，按 seq 降序（新→旧）。
// before 为 nil 时从最新开始；limit 控制单页大小。
// 过滤空 assistant 占位消息：流中断且兜底清理失败时会残留，不应展示给用户。
func (r *MessageRepo) ListByConversation(
	ctx context.Context, convID uuid.UUID, before *uuid.UUID, limit int,
) ([]*entity.Message, error) {
	if before == nil {
		// 首页与游标分支统一按 seq 定序：seq 单调递增且全局唯一，排序为全序，
		// 同一轮 user/assistant 的 created_at 相同也不会出现"答案在问题前"。
		sql := `SELECT ` + messageColumns + `
	             FROM messages WHERE conversation_id = $1
	             AND NOT (role = 'assistant' AND content = '')
	             ORDER BY seq DESC LIMIT $2`
		return r.queryMessages(ctx, sql, convID, limit)
	}
	// 游标分页：以游标消息的 seq 为界取更旧消息。seq 单调，单字段游标即全序
	// （替代旧 (created_at, id) 复合游标——同事务消息 created_at 相同，id 为随机 UUID 不可靠）。
	sql := `SELECT ` + messageColumns + `
	             FROM messages WHERE conversation_id = $1
	             AND NOT (role = 'assistant' AND content = '')
	             AND seq < (SELECT seq FROM messages WHERE id = $2 AND conversation_id = $1)
	             ORDER BY seq DESC LIMIT $3`
	return r.queryMessages(ctx, sql, convID, *before, limit)
}

// GetRecentHistory 取最近 turns 轮消息（一轮 = user + assistant）。
// 返回顺序：旧→新（按 seq，同轮消息 created_at 相同时仍保证先 user 后 assistant）。
// Service 用于查询改写和 LLM 上下文。
// excludeID 非 nil 时排除该消息：当前轮用户消息已先于历史加载持久化，
// 不排除会让 LLM 上下文出现重复提问（原始问题 + 改写问题两条连续 user 消息）。
// 同时过滤空 assistant 占位消息，避免残留占位污染上下文。
func (r *MessageRepo) GetRecentHistory(
	ctx context.Context, convID uuid.UUID, turns int, excludeID *uuid.UUID,
) ([]*entity.Message, error) {
	limit := turns * 2
	const base = `SELECT ` + messageColumns + `
	             FROM (
	                 SELECT ` + messageColumns + `
	                 FROM messages WHERE conversation_id = $1
	                 AND NOT (role = 'assistant' AND content = '')`
	if excludeID != nil {
		const sql = base + `
	                 AND id <> $2
	                 ORDER BY seq DESC LIMIT $3
	             ) t ORDER BY seq ASC`
		return r.queryMessages(ctx, sql, convID, *excludeID, limit)
	}
	const sql = base + `
	                 ORDER BY seq DESC LIMIT $2
	             ) t ORDER BY seq ASC`
	return r.queryMessages(ctx, sql, convID, limit)
}

func (r *MessageRepo) queryMessages(ctx context.Context, sql string, args ...any) ([]*entity.Message, error) {
	rows, err := postgres.Q(ctx, r.pool).Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()
	out := []*entity.Message{}
	for rows.Next() {
		m := &entity.Message{}
		var refsBytes []byte
		var turn *uuid.UUID
		if err := rows.Scan(
			&m.ID, &m.ConversationID, &turn, &m.Role, &m.Content,
			&m.ResultCode, &refsBytes, &m.CreatedAt, &m.UpdatedAt, &m.Feedback, &m.Seq,
		); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		if turn != nil {
			m.TurnID = *turn
		}
		m.ReferencedChunks = unmarshalRefs(refsBytes)
		out = append(out, m)
	}
	return out, rows.Err()
}

// marshalRefs 序列化引用切片为 JSONB 兼容的 []byte；nil/空切片编码为 `[]`。
func marshalRefs(refs []entity.Reference) ([]byte, error) {
	if refs == nil {
		refs = []entity.Reference{}
	}
	b, err := json.Marshal(refs)
	if err != nil {
		return nil, fmt.Errorf("marshal references: %w", err)
	}
	return b, nil
}

// unmarshalRefs 反序列化 JSONB 为引用切片；解码失败返回空切片（不抛错避免单条消息加载失败）。
// ponytail: 解码失败静默降级为空切片——损坏数据不应让整列消息读不出来；前端会展示空引用，折中。
func unmarshalRefs(b []byte) []entity.Reference {
	if len(b) == 0 {
		return []entity.Reference{}
	}
	var refs []entity.Reference
	if err := json.Unmarshal(b, &refs); err != nil {
		return []entity.Reference{}
	}
	if refs == nil {
		return []entity.Reference{}
	}
	return refs
}
