package repository

import (
	"context"
	"encoding/json"
	"fmt"

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

// SaveUserMessage 持久化用户消息（result_code 留空）。
func (r *MessageRepo) SaveUserMessage(ctx context.Context, convID uuid.UUID, content string) (*entity.Message, error) {
	return r.save(ctx, convID, "user", content, "", nil)
}

// SaveAssistant 保存完整 assistant 消息（用于危机/拒答等无 LLM 生成场景）。
func (r *MessageRepo) SaveAssistant(
	ctx context.Context, convID uuid.UUID, content, resultCode string, refs []entity.Reference,
) (*entity.Message, error) {
	return r.save(ctx, convID, "assistant", content, resultCode, refs)
}

// SaveAssistantPlaceholder 保存 assistant 占位消息（空内容，待流式生成后 FinalizeAssistant）。
func (r *MessageRepo) SaveAssistantPlaceholder(ctx context.Context, convID uuid.UUID) (*entity.Message, error) {
	return r.save(ctx, convID, "assistant", "", "", nil)
}

func (r *MessageRepo) save(
	ctx context.Context, convID uuid.UUID, role, content, resultCode string, refs []entity.Reference,
) (*entity.Message, error) {
	const sql = `INSERT INTO messages (conversation_id, role, content, result_code, referenced_chunks)
	             VALUES ($1, $2, $3, $4, $5)
	             RETURNING id, conversation_id, role, content, result_code, referenced_chunks, created_at, updated_at`
	refsJSON, err := marshalRefs(refs)
	if err != nil {
		return nil, err
	}
	m := &entity.Message{}
	var refsBytes []byte
	row := postgres.Q(ctx, r.pool).QueryRow(ctx, sql, convID, role, content, resultCode, refsJSON)
	if err := row.Scan(
		&m.ID, &m.ConversationID, &m.Role, &m.Content,
		&m.ResultCode, &refsBytes, &m.CreatedAt, &m.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("save message: %w", err)
	}
	m.ReferencedChunks = unmarshalRefs(refsBytes)
	return m, nil
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

// UpdateFeedback 更新消息反馈（up/down）。
// 通过 conversations.patient_id 子查询校验消息属于该患者（数据隔离）；
// 返回受影响行数，0 表示消息不存在或不属于该患者。
func (r *MessageRepo) UpdateFeedback(ctx context.Context, messageID uuid.UUID, patientID int64, feedback string) (int64, error) {
	const sql = `UPDATE messages SET feedback = $3, updated_at = now()
	             WHERE id = $1
	             AND conversation_id IN (SELECT id FROM conversations WHERE patient_id = $2)`
	tag, err := postgres.Q(ctx, r.pool).Exec(ctx, sql, messageID, patientID, feedback)
	if err != nil {
		return 0, fmt.Errorf("update feedback: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ListByConversation 列出会话消息，按 created_at 降序。
// before 为 nil 时从最新开始；limit 控制单页大小。
// 过滤空 assistant 占位消息：流中断且兜底清理失败时会残留，不应展示给用户。
func (r *MessageRepo) ListByConversation(
	ctx context.Context, convID uuid.UUID, before *uuid.UUID, limit int,
) ([]*entity.Message, error) {
	if before == nil {
		// 首页排序必须与游标分支的 (created_at DESC, id DESC) 全序一致：
		// 仅按 created_at DESC 时，created_at 相同的消息顺序不确定，
		// 取页尾作游标会漏掉/重复相同时间戳的消息（与下方 (created_at, id) 复合游标语义错位）。
		const sql = `SELECT id, conversation_id, role, content, result_code, referenced_chunks, created_at, updated_at, feedback
	             FROM messages WHERE conversation_id = $1
	             AND NOT (role = 'assistant' AND content = '')
	             ORDER BY created_at DESC, id DESC LIMIT $2`
		return r.queryMessages(ctx, sql, convID, limit)
	}
	// H4: 用 (created_at, id) 复合游标避免相同 created_at 时漏消息。
	// Postgres ROW value comparison 要求字段类型一致：created_at TIMESTAMPTZ, id UUID。
	const sql = `SELECT id, conversation_id, role, content, result_code, referenced_chunks, created_at, updated_at, feedback
	             FROM messages WHERE conversation_id = $1
	             AND NOT (role = 'assistant' AND content = '')
	             AND (created_at, id) < (
	                 SELECT created_at, id FROM messages WHERE id = $2 AND conversation_id = $1
	             )
	             ORDER BY created_at DESC, id DESC LIMIT $3`
	return r.queryMessages(ctx, sql, convID, *before, limit)
}

// GetRecentHistory 取最近 turns 轮消息（一轮 = user + assistant）。
// 返回顺序：旧→新。Service 用于查询改写和 LLM 上下文。
// excludeID 非 nil 时排除该消息：当前轮用户消息已先于历史加载持久化，
// 不排除会让 LLM 上下文出现重复提问（原始问题 + 改写问题两条连续 user 消息）。
// 同时过滤空 assistant 占位消息，避免残留占位污染上下文。
func (r *MessageRepo) GetRecentHistory(ctx context.Context, convID uuid.UUID, turns int, excludeID *uuid.UUID) ([]*entity.Message, error) {
	limit := turns * 2
	const base = `SELECT id, conversation_id, role, content, result_code, referenced_chunks, created_at, updated_at, feedback
	             FROM (
	                 SELECT id, conversation_id, role, content, result_code, referenced_chunks, created_at, updated_at, feedback
	                 FROM messages WHERE conversation_id = $1
	                 AND NOT (role = 'assistant' AND content = '')`
	if excludeID != nil {
		const sql = base + `
	                 AND id <> $2
	                 ORDER BY created_at DESC LIMIT $3
	             ) t ORDER BY created_at ASC`
		return r.queryMessages(ctx, sql, convID, *excludeID, limit)
	}
	const sql = base + `
	                 ORDER BY created_at DESC LIMIT $2
	             ) t ORDER BY created_at ASC`
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
		if err := rows.Scan(
			&m.ID, &m.ConversationID, &m.Role, &m.Content,
			&m.ResultCode, &refsBytes, &m.CreatedAt, &m.UpdatedAt, &m.Feedback,
		); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
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
