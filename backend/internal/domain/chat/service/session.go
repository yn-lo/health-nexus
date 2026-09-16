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
// 使用 Redis List 作为有界环：RPush 追加、LTrim 裁剪、LRange 读取、Expire 刷新 TTL、Del 删除。
type ringStore interface {
	RPush(ctx context.Context, key string, values ...string) error
	LRange(ctx context.Context, key string, start, stop int64) ([]string, error)
	LTrim(ctx context.Context, key string, start, stop int64) error
	Expire(ctx context.Context, key string, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
}

// TurnMessages 本轮的两条消息（幂等重放用）：user 与 assistant。
// Assistant 为 nil 或 ResultCode 为空表示本轮尚未产生终态。
type TurnMessages struct {
	User      *entity.Message
	Assistant *entity.Message
}

// TurnWrite 本轮写入参数：本轮用户消息 + 轮次标识（+ DB 场景的科室锁定）。
// 同一轮的 user / assistant 消息共享同 TurnID，用于幂等重放与按轮聚合。
type TurnWrite struct {
	Content string
	TurnID  uuid.UUID
	DeptID  *int64
}

// SessionStore 统一会话持久化能力。认证会话由 DB 实现，匿名会话由 Redis 瞬态环实现。
// RAG 链条只依赖 Session/Store，不感知用户身份——认证与匿名的差异收敛到 Store 实现内部。
type SessionStore interface {
	// History 返回最近 turns 轮消息（时间升序）。excludeID 非 nil 时排除该消息
	// （当前用户消息已先行持久化，不排除会导致 LLM 上下文出现重复提问）。
	History(ctx context.Context, turns int, excludeID *uuid.UUID) ([]*entity.Message, error)
	// SaveUserAndPlaceholder 原子创建本轮的用户消息与 assistant 占位消息（同一事务），
	// 返回用户消息与占位消息 ID。占位消息使每个失败出口都有终态可写，不留下孤立 user 消息。
	SaveUserAndPlaceholder(ctx context.Context, in TurnWrite) (*entity.Message, uuid.UUID, error)
	// FinalizeAssistant 用最终内容更新 assistant 消息（拒答 / 输出安全审查后 / 中断清理路径）。
	// id 为占位消息 ID；turnID 为本轮标识（DB 实现占位行已含 turn_id，匿名实现据此标注新写入的条目）。
	FinalizeAssistant(
		ctx context.Context, id, turnID uuid.UUID, content, resultCode string, refs []entity.Reference,
	) error
	// TurnByID 按轮次取本轮消息（幂等重放用）；无记录返回 (nil, nil)。
	TurnByID(ctx context.Context, turnID uuid.UUID) (*TurnMessages, error)
	// PersistCrisis 命中危机时的持久化 + 医护通知。认证实现落库并通知；匿名实现为空操作。
	// 返回本轮 user / assistant 消息 ID（匿名为 uuid.Nil），供权威结果事件回传前端。
	PersistCrisis(
		ctx context.Context, patientID int64, in TurnWrite, c *rag.Crisis, reply string,
	) (userMsgID, assistantMsgID uuid.UUID, err error)
}

// Session 一次 RAG 流式请求的会话载体，统一认证 / 匿名两类会话。
// 链条只认识 Session：会话标识、检索科室、持久化均由 Session 承载，不出现身份分支。
type Session struct {
	SID    string // 会话标识：DB 场景为 conversation.ID；匿名场景为 device 派生 uuid。
	DeptID *int64 // 检索科室范围；nil 表示不限科室。
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

// SaveUserAndPlaceholder 在**同一事务**内写入用户消息与 assistant 占位消息。
// 本轮自创建起即为 user + assistant 一对，后续任一出口（拒答 / 中断清理 / finalize）
// 只需更新占位，不会留下孤立 user 消息，也不会出现"用户消息已落库但回答终态缺失"。
func (d *dbSessionStore) SaveUserAndPlaceholder(
	ctx context.Context, in TurnWrite,
) (*entity.Message, uuid.UUID, error) {
	lockDept := d.pendingLockDept(in.DeptID)
	var userMsg *entity.Message
	var placeholderID uuid.UUID
	err := d.tx.WithTx(ctx, func(ctx context.Context) error {
		m, err := d.saveUserInTx(ctx, in.Content, in.TurnID, lockDept)
		if err != nil {
			return err
		}
		userMsg = m
		ph, err := d.msg.SaveAssistantPlaceholder(ctx, d.convEntity.ID, in.TurnID)
		if err != nil {
			return err
		}
		placeholderID = ph.ID
		return nil
	})
	if err != nil {
		return nil, uuid.Nil, err
	}
	if lockDept != nil {
		d.convEntity.LockedDeptID = lockDept
	}
	return userMsg, placeholderID, nil
}

// pendingLockDept 会话未锁定时，仅当明确选择具体科室（>0）才需要锁定；nil/0 视为"全部科室"。
func (d *dbSessionStore) pendingLockDept(deptID *int64) *int64 {
	if d.convEntity.LockedDeptID == nil && deptID != nil && *deptID > 0 {
		return deptID
	}
	return nil
}

// saveUserInTx 在调用方事务内完成科室锁定 + 用户消息落库 + 标题 + 活跃时间。
func (d *dbSessionStore) saveUserInTx(
	ctx context.Context, content string, turnID uuid.UUID, lockDept *int64,
) (*entity.Message, error) {
	id := d.convEntity.ID
	if lockDept != nil {
		if err := d.conv.LockDept(ctx, id, *lockDept); err != nil {
			return nil, err
		}
	}
	msg, err := d.msg.SaveUserMessage(ctx, id, turnID, content)
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

func (d *dbSessionStore) FinalizeAssistant(
	ctx context.Context, id, _ uuid.UUID, content, resultCode string, refs []entity.Reference,
) error {
	return d.msg.FinalizeAssistant(ctx, id, content, resultCode, refs)
}

// TurnByID 按轮次取本轮消息（幂等重放定位结果）。
func (d *dbSessionStore) TurnByID(ctx context.Context, turnID uuid.UUID) (*TurnMessages, error) {
	msgs, err := d.msg.ListByTurn(ctx, d.convEntity.ID, turnID)
	if err != nil {
		return nil, err
	}
	if len(msgs) == 0 {
		return nil, nil
	}
	return splitTurn(msgs), nil
}

// splitTurn 把本轮消息拆成 user / assistant（列表按时间升序，取各自的最后一条）。
func splitTurn(msgs []*entity.Message) *TurnMessages {
	out := &TurnMessages{}
	for _, m := range msgs {
		switch m.Role {
		case constants.MessageRoleUser:
			out.User = m
		case constants.MessageRoleAssistant:
			out.Assistant = m
		}
	}
	return out
}

// PersistCrisis 落库用户消息 + 危机事件 + assistant 危机回复（同一事务，含一次性重试）+ 医护通知。
// 返回本轮 user / assistant 消息 ID，供权威结果事件回传前端。
func (d *dbSessionStore) PersistCrisis(
	ctx context.Context, patientID int64, in TurnWrite, c *rag.Crisis, reply string,
) (userMsgID, assistantMsgID uuid.UUID, err error) {
	// crisis 事件高价值（医护端需感知），瞬时 DB 故障不应静默漏报：有界重试 1 次；ctx 取消不重试。
	const crisisPersistRetries = 1
	var crisisEventID int64
	persist := func(ctx context.Context) error {
		return d.tx.WithTx(ctx, func(ctx context.Context) error {
			userMsg, err := d.saveUserInTx(ctx, in.Content, in.TurnID, nil)
			if err != nil {
				return err
			}
			e := &entity.CrisisEvent{
				PatientID:        patientID,
				ConversationID:   d.convEntity.ID,
				TriggeredContent: in.Content,
				MatchedKeywords:  c.Keywords,
				Level:            c.Level,
			}
			if userMsg != nil {
				mid := userMsg.ID
				e.MessageID = &mid
				userMsgID = mid
			}
			ceID, cerr := d.crisis.Create(ctx, e)
			if cerr != nil {
				return cerr
			}
			crisisEventID = ceID
			aiMsg, serr := d.msg.SaveAssistant(ctx, d.convEntity.ID, reply, constants.ResultCrisis, nil, in.TurnID)
			if serr != nil {
				return serr
			}
			if aiMsg != nil {
				assistantMsgID = aiMsg.ID
			}
			return nil
		})
	}
	err = persist(ctx)
	for retried := 0; err != nil && retried < crisisPersistRetries && ctx.Err() == nil; retried++ {
		slog.WarnContext(ctx, "chat crisis persistence failed once, retrying",
			"attempt", retried+1, "patient_id", patientID, "conversation_id", d.convEntity.ID, "err", err)
		err = persist(ctx)
	}
	if err != nil {
		slog.ErrorContext(ctx, "chat crisis persistence failed, still pushing hotline",
			"patient_id", patientID, "conversation_id", d.convEntity.ID,
			"keywords", c.Keywords, "err", err)
		return uuid.Nil, uuid.Nil, err
	}
	slog.InfoContext(ctx, "chat crisis event created",
		"event_id", crisisEventID, "patient_id", patientID, "conversation_id", d.convEntity.ID, "keywords", c.Keywords)
	// 主动通知：入队 asynq 任务，worker 落库站内通知给 DEPT_ADMIN（fire-and-forget，不阻断 SSE 流）
	if nerr := d.crisisNotifier.NotifyCrisis(ctx, crisisEventID); nerr != nil {
		slog.ErrorContext(ctx, "chat crisis notify enqueue failed", "event_id", crisisEventID, "err", nerr)
	}
	return userMsgID, assistantMsgID, nil
}

// --- 匿名会话 Store（Redis 瞬态环） ---

// anonymousMaxMessages 匿名会话环保留的最大消息条数（有界，防止长会话无限增长）。
// 约为单次生成所需历史上限（HistoryTurns*2）的两倍，留出排除当前消息与并发写入的余量。
const anonymousMaxMessages = constants.HistoryTurns * 4

// anonRingKey 匿名会话环 key：以设备标识 + 会话 ID 命名空间隔离，
// 同设备可有多个会话（"新对话"不再复用旧上下文），跨设备携带他人会话 ID 也读不到内容。
func anonRingKey(deviceID, sid string) string {
	return "chat_anon:" + deviceID + ":" + sid
}

// memSessionStore 会话持久化的 Redis 瞬态实现，支撑匿名用户多轮上下文（TTL 自动过期）。
type memSessionStore struct {
	ring ringStore
	key  string // 匿名会话环 key（设备命名空间 + 会话 ID）。
}

func newMemSessionStore(ring ringStore, deviceID, sid string) *memSessionStore {
	return &memSessionStore{ring: ring, key: anonRingKey(deviceID, sid)}
}

func (m *memSessionStore) History(ctx context.Context, turns int, excludeID *uuid.UUID) ([]*entity.Message, error) {
	if m.ring == nil {
		return nil, nil
	}
	// 只读尾部若干条（而非 0~-1 全量）：环本身有界，且避免长会话把整个列表拉回应用层。
	// 多读 1 条——当前轮用户消息已在环尾，排除后仍能取满 turns*2 条。
	start := int64(-(turns*2 + 1))
	if turns <= 0 {
		start = -int64(anonymousMaxMessages)
	}
	raw, err := m.ring.LRange(ctx, m.key, start, -1)
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

// SaveUserAndPlaceholder 匿名实现：用户消息入环，占位消息只在内存中占位（不写入环）。
// 最终内容由 FinalizeAssistant 落环，因此中断路径不会在匿名上下文里留下空占位。
func (m *memSessionStore) SaveUserAndPlaceholder(
	ctx context.Context, in TurnWrite,
) (*entity.Message, uuid.UUID, error) {
	mf := &entity.Message{
		ID:        uuid.New(),
		TurnID:    in.TurnID,
		Role:      constants.MessageRoleUser,
		Content:   in.Content,
		CreatedAt: time.Now(),
	}
	if err := m.push(ctx, mf); err != nil {
		return nil, uuid.Nil, err
	}
	return mf, uuid.New(), nil
}

// TurnByID 匿名实现：在环内查找本轮消息（幂等重放定位结果）。
// 未 finalize 的轮次在环里只有 user 条目 → Assistant 为 nil，调用方据此判"生成中"。
func (m *memSessionStore) TurnByID(ctx context.Context, turnID uuid.UUID) (*TurnMessages, error) {
	if m.ring == nil || turnID == uuid.Nil {
		return nil, nil
	}
	raw, err := m.ring.LRange(ctx, m.key, -int64(anonymousMaxMessages), -1)
	if err != nil {
		return nil, fmt.Errorf("anon turn lookup: %w", err)
	}
	msgs := make([]*entity.Message, 0, len(raw))
	for _, s := range raw {
		var mf entity.Message
		if jerr := json.Unmarshal([]byte(s), &mf); jerr != nil {
			continue
		}
		if mf.TurnID == turnID {
			msg := mf
			msgs = append(msgs, &msg)
		}
	}
	if len(msgs) == 0 {
		return nil, nil
	}
	return splitTurn(msgs), nil
}

// FinalizeAssistant 匿名实现：把最终内容作为一条新的 assistant 消息写入环（占位未入环）。
func (m *memSessionStore) FinalizeAssistant(
	ctx context.Context, _, turnID uuid.UUID, content, resultCode string, refs []entity.Reference,
) error {
	return m.push(ctx, &entity.Message{
		ID:               uuid.New(),
		TurnID:           turnID,
		Role:             constants.MessageRoleAssistant,
		Content:          content,
		ResultCode:       resultCode,
		ReferencedChunks: refs,
		CreatedAt:        time.Now(),
	})
}

// PersistCrisis 匿名实现：不汇报不记录，返回零值 ID（链路上已下发心理援助热线）。
func (m *memSessionStore) PersistCrisis(
	context.Context, int64, TurnWrite, *rag.Crisis, string,
) (userMsgID, assistantMsgID uuid.UUID, err error) {
	return uuid.Nil, uuid.Nil, nil
}

// push 序列化消息追加到环、裁剪为有界长度并刷新 TTL。
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
	// LTrim 保留尾部 anonymousMaxMessages 条：列表长度有界，避免持续使用无限增长；
	// TTL 每轮续期只保证过期回收，不能限制总量，故必须显式裁剪。
	if err := m.ring.LTrim(ctx, m.key, -int64(anonymousMaxMessages), -1); err != nil {
		slog.WarnContext(ctx, "anon ring trim failed (list may grow beyond bound)", "err", err)
	}
	if err := m.ring.Expire(ctx, m.key, anonymousContextTTL); err != nil {
		slog.WarnContext(ctx, "anon ring expire failed (TTL not refreshed)", "err", err)
	}
	return nil
}
