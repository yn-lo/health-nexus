// Package service 聊天域.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"health-nexus/internal/domain/chat/entity"
	"health-nexus/internal/shared/constants"
	"health-nexus/internal/shared/rag"
)

// anonymousContextTTL 匿名会话上下文在 Redis 中的保留时长。TTL 自动过期，无需清理任务。
const anonymousContextTTL = 12 * time.Hour

// ringStore 匿名会话的瞬态消息环存取能力（消费者定义，ISP）。*redis.RingStore 实现此接口。
// 使用 Redis List 作为环：RPush 追加、LRange 读取、Expire 刷新 TTL。
type ringStore interface {
	RPush(ctx context.Context, key string, values ...string) error
	LRange(ctx context.Context, key string, start, stop int64) ([]string, error)
	Expire(ctx context.Context, key string, ttl time.Duration) error
}

// SessionStore 统一会话持久化能力。认证会话由 DB 实现，匿名会话由 Redis 瞬态环实现。
// RAG 链条只依赖 Session/Store，不感知用户身份——认证与匿名的差异收敛到 Store 实现内部。
type SessionStore interface {
	// History 返回最近 turns 轮消息（时间升序）。excludeID 非 nil 时排除该消息
	// （当前用户消息已先行持久化，不排除会导致 LLM 上下文出现重复提问）。
	History(ctx context.Context, turns int, excludeID *uuid.UUID) ([]*entity.Message, error)
	// SaveUser 持久化一条用户消息，返回消息（含生成的 ID）。
	// deptID 用于 DB 场景的科室锁定（会话未锁定时才锁定）；匿名场景忽略。
	SaveUser(ctx context.Context, content string, deptID *int64) (*entity.Message, error)
	// SaveAssistant 直接保存一条完整 assistant 消息（拒答 / 危机 / 注入场景）。
	SaveAssistant(ctx context.Context, content, resultCode string, refs []entity.Reference) error
	// SaveAssistantPlaceholder 保存 assistant 占位消息并返回其 ID（供后续 FinalizeAssistant 落最终内容）。
	SaveAssistantPlaceholder(ctx context.Context) (*entity.Message, error)
	// FinalizeAssistant 用最终内容更新 assistant 消息（输出安全审查后 / 中断清理路径）。
	FinalizeAssistant(ctx context.Context, id uuid.UUID, content, resultCode string, refs []entity.Reference) error
	// PersistCrisis 命中危机时的持久化 + 医护通知。认证实现落库并通知；匿名实现为空操作。
	// patientID 为触发用户（匿名时无意义），reply 为危机热的回复话术（链路上层安全审查生成）。
	PersistCrisis(ctx context.Context, patientID int64, userContent string, c *rag.Crisis, reply string) error
}

// Session 一次 RAG 流式请求的会话载体，统一认证 / 匿名两类会话。
// 链条只认识 Session：会话标识、检索科室、持久化均由 Session 承载，不出现身份分支。
type Session struct {
	SID    string         // 会话标识：DB 场景为 conversation.ID；匿名场景为 device 派生 uuid。
	DeptID *int64         // 检索科室范围；nil 表示不限科室。
	store  SessionStore
}

// ID 返回会话标识（用于 conversation SSE 事件回传，供前端续传与会话列表）。
func (s *Session) ID() string { return s.SID }

// --- DB 会话 Store（认证用户） ---

// dbSessionStore 会话持久化的 DB 实现，包装既有 repos/tx，保持与原认证路径一致的事务边界与行为。
type dbSessionStore struct {
	conv           ConversationPort
	msg            MessagePort
	crisis         CrisisPort
	crisisNotifier CrisisNotifier
	tx             TxRunner
	convEntity     *entity.Conversation
}

// newDBSessionStore 构造 DB store。convEntity 为当前会话（含 ID / 锁定科室）。
func newDBSessionStore(
	conv ConversationPort, msg MessagePort, crisis CrisisPort, crisisNotifier CrisisNotifier,
	tx TxRunner, convEntity *entity.Conversation,
) *dbSessionStore {
	return &dbSessionStore{
		conv: conv, msg: msg, crisis: crisis, crisisNotifier: crisisNotifier,
		tx: tx, convEntity: convEntity,
	}
}

func (d *dbSessionStore) History(ctx context.Context, turns int, excludeID *uuid.UUID) ([]*entity.Message, error) {
	return d.msg.GetRecentHistory(ctx, d.convEntity.ID, turns, excludeID)
}

func (d *dbSessionStore) SaveUser(ctx context.Context, content string, deptID *int64) (*entity.Message, error) {
	id := d.convEntity.ID
	// 会话未锁定时，仅当明确选择具体科室（>0）才锁定；nil/0 视为"全部科室"。
	if d.convEntity.LockedDeptID == nil && deptID != nil && *deptID > 0 {
		if err := d.conv.LockDept(ctx, id, *deptID); err != nil {
			return nil, err
		}
		dd := *deptID
		d.convEntity.LockedDeptID = &dd
	}
	msg, err := d.msg.SaveUserMessage(ctx, id, content)
	if err != nil {
		return nil, err
	}
	if err := d.conv.UpdateTitleIfEmpty(ctx, id, truncateTitle(content)); err != nil {
		return nil, err
	}
	if err := d.conv.TouchLastMessageAt(ctx, id); err != nil {
		return nil, err
	}
	return msg, nil
}

func (d *dbSessionStore) SaveAssistant(ctx context.Context, content, resultCode string, refs []entity.Reference) error {
	_, err := d.msg.SaveAssistant(ctx, d.convEntity.ID, content, resultCode, refs)
	return err
}

func (d *dbSessionStore) SaveAssistantPlaceholder(ctx context.Context) (*entity.Message, error) {
	return d.msg.SaveAssistantPlaceholder(ctx, d.convEntity.ID)
}

func (d *dbSessionStore) FinalizeAssistant(ctx context.Context, id uuid.UUID, content, resultCode string, refs []entity.Reference) error {
	return d.msg.FinalizeAssistant(ctx, id, content, resultCode, refs)
}

// PersistCrisis 落库用户消息 + 危机事件 + assistant 危机回复（同一事务，含一次性重试）+ 医护通知。
func (d *dbSessionStore) PersistCrisis(
	ctx context.Context, patientID int64, userContent string, c *rag.Crisis, reply string,
) error {
	// crisis 事件高价值（医护端需感知），瞬时 DB 故障不应静默漏报：有界重试 1 次；ctx 取消不重试。
	const crisisPersistRetries = 1
	var crisisEventID int64
	persist := func(ctx context.Context) error {
		return d.tx.WithTx(ctx, func(ctx context.Context) error {
			userMsg, err := d.SaveUser(ctx, userContent, nil)
			if err != nil {
				return err
			}
			e := &entity.CrisisEvent{
				PatientID:        patientID,
				ConversationID:   d.convEntity.ID,
				TriggeredContent: userContent,
				MatchedKeywords:  c.Keywords,
				Level:            c.Level,
			}
			if userMsg != nil {
				mid := userMsg.ID
				e.MessageID = &mid
			}
			ceID, cerr := d.crisis.Create(ctx, e)
			if cerr != nil {
				return cerr
			}
			crisisEventID = ceID
			_, serr := d.msg.SaveAssistant(ctx, d.convEntity.ID, reply, constants.ResultCrisis, nil)
			return serr
		})
	}
	err := persist(ctx)
	for retried := 0; err != nil && retried < crisisPersistRetries && ctx.Err() == nil; retried++ {
		slog.WarnContext(ctx, "chat crisis persistence failed once, retrying",
			"attempt", retried+1, "patient_id", patientID, "conversation_id", d.convEntity.ID, "err", err)
		err = persist(ctx)
	}
	if err != nil {
		slog.ErrorContext(ctx, "chat crisis persistence failed, still pushing hotline",
			"patient_id", patientID, "conversation_id", d.convEntity.ID,
			"keywords", c.Keywords, "err", err)
		return err
	}
	slog.InfoContext(ctx, "chat crisis event created",
		"event_id", crisisEventID, "patient_id", patientID, "conversation_id", d.convEntity.ID, "keywords", c.Keywords)
	// 主动通知：入队 asynq 任务，worker 落库站内通知给 DEPT_ADMIN（fire-and-forget，不阻断 SSE 流）
	if err := d.crisisNotifier.NotifyCrisis(ctx, crisisEventID); err != nil {
		slog.ErrorContext(ctx, "chat crisis notify enqueue failed", "event_id", crisisEventID, "err", err)
	}
	return nil
}

// --- 匿名会话 Store（Redis 瞬态环） ---

// memSessionStore 会话持久化的 Redis 瞬态实现，支撑匿名用户多轮上下文（TTL 自动过期）。
type memSessionStore struct {
	ring ringStore
	key  string // 匿名会话环 key（基于 device 派生的 uuid）。
}

func newMemSessionStore(ring ringStore, sid string) *memSessionStore {
	return &memSessionStore{ring: ring, key: "chat_anon:" + sid}
}

func (m *memSessionStore) History(ctx context.Context, turns int, excludeID *uuid.UUID) ([]*entity.Message, error) {
	if m.ring == nil {
		return nil, nil
	}
	raw, err := m.ring.LRange(ctx, m.key, 0, -1)
	if err != nil {
		return nil, fmt.Errorf("anon history lrange: %w", err)
	}
	msgs := make([]*entity.Message, 0, len(raw))
	for _, s := range raw {
		var mf entity.Message
		if jerr := json.Unmarshal([]byte(s), &mf); jerr != nil {
			slog.WarnContext(ctx, "anon history: corrupt entry dropped", "err", jerr)
			continue
		}
		if excludeID != nil && mf.ID == *excludeID {
			continue
		}
		msgs = append(msgs, &mf)
	}
	if turns <= 0 || len(msgs) <= turns*2 {
		return msgs, nil
	}
	return msgs[len(msgs)-turns*2:], nil
}

func (m *memSessionStore) SaveUser(ctx context.Context, content string, _ *int64) (*entity.Message, error) {
	mf := &entity.Message{ID: uuid.New(), Role: constants.MessageRoleUser, Content: content, CreatedAt: time.Now()}
	if err := m.push(ctx, mf); err != nil {
		return nil, err
	}
	return mf, nil
}

func (m *memSessionStore) SaveAssistant(ctx context.Context, content, resultCode string, refs []entity.Reference) error {
	mf := &entity.Message{ID: uuid.New(), Role: constants.MessageRoleAssistant, Content: content, ResultCode: resultCode, ReferencedChunks: refs, CreatedAt: time.Now()}
	return m.push(ctx, mf)
}

func (m *memSessionStore) SaveAssistantPlaceholder(ctx context.Context) (*entity.Message, error) {
	return &entity.Message{ID: uuid.New(), Role: constants.MessageRoleAssistant, CreatedAt: time.Now()}, nil
}

func (m *memSessionStore) FinalizeAssistant(ctx context.Context, id uuid.UUID, content, resultCode string, refs []entity.Reference) error {
	return m.SaveAssistant(ctx, content, resultCode, refs)
}

func (m *memSessionStore) PersistCrisis(ctx context.Context, _ int64, _ string, _ *rag.Crisis, _ string) error {
	// 匿名危机不汇报、不记录：命中时链路上已下发心理援助热线，此处为空操作。
	return nil
}

// push 序列化消息追加到环并刷新 TTL。
func (m *memSessionStore) push(ctx context.Context, mf *entity.Message) error {
	if m.ring == nil {
		// 无 Redis 环（如测试）时退化为单轮：不持久化（历史为空，逻辑等价旧匿名行为）。
		return nil
	}
	data, err := json.Marshal(mf)
	if err != nil {
		return err
	}
	if err := m.ring.RPush(ctx, m.key, string(data)); err != nil {
		return fmt.Errorf("anon ring push: %w", err)
	}
	if err := m.ring.Expire(ctx, m.key, anonymousContextTTL); err != nil {
		slog.WarnContext(ctx, "anon ring expire failed (TTL not refreshed)", "err", err)
	}
	return nil
}