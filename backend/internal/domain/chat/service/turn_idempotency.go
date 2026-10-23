package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"unicode/utf8"

	"github.com/google/uuid"

	"health-nexus/internal/shared/constants"
	apperrors "health-nexus/internal/shared/errors"
)

// turnRef 幂等登记值：本轮所属会话 + 轮次标识（uuid.UUID 按文本序列化）。
// reserved 为瞬态标志（不参与序列化）：登记值为"进行中"占位时置位，表示另一并发请求已占位。
type turnRef struct {
	SID      string    `json:"sid"`
	TurnID   uuid.UUID `json:"turn_id"`
	reserved bool      `json:"-"`
}

// idempotencyKey 身份作用域 + request_id：跨身份无法命中他人的登记，
// 避免伪造 request_id 读取他人本轮结果。
func idempotencyKey(in StreamInput, requestID string) string {
	if in.Identity.Anon() {
		return "chat_turn:anon:" + in.Identity.DeviceID + ":" + requestID
	}
	return fmt.Sprintf("chat_turn:user:%d:%s", in.Identity.UserID, requestID)
}

// reserveTurn 原子占用 request_id（SET NX），防止同 request_id 并发提交重复创建会话与生成。
//   - 未携带 request_id / 登记能力不可用：跳过（返回 nil，降级为不幂等）；
//   - 占位成功：返回 release 回调（本请求后续失败且未产生终态时清除占位，允许重试）；
//   - 占位失败（已存在）：返回 409——另一并发请求正在进行或已完成（已完成者由 replayTurn 优先回放）。
//
// 必须在创建会话之前调用：原实现"先查后写（普通 SET）"存在竞态，两个并发请求都能通过首次查询，
// 各自创建会话并生成（P2）。
func (s *ChatSendService) reserveTurn(ctx context.Context, in StreamInput) (func(), error) {
	if s.turns == nil || in.RequestID == "" {
		return func() {}, nil
	}
	key := idempotencyKey(in, in.RequestID)
	ok, err := s.turns.PutIfAbsent(ctx, key, turnReservedMarker, idempotencyTTL)
	if err != nil {
		// 占位失败（Redis 故障）不阻断问答：降级为不幂等，与未启用幂等一致。
		slog.WarnContext(ctx, "chat: idempotency reserve failed, continue without it", "err", err)
		return func() {}, nil
	}
	if !ok {
		return nil, apperrors.Conflict("CHAT_TURN_IN_PROGRESS", "会话正在生成中，请稍后重试")
	}
	// 本请求失败（未产生终态）时清除占位，避免异常中断后 request_id 被长期占用无法重试。
	// 成功产生终态时 registerTurn 已覆盖为权威 turnRef，release 的幂等删除不影响。
	return func() {
		if raw, found, lerr := s.turns.Lookup(ctx, key); lerr == nil && found && raw == turnReservedMarker {
			if derr := s.turns.Delete(ctx, key); derr != nil {
				slog.WarnContext(ctx, "chat: idempotency release failed", "err", derr)
			}
		}
	}, nil
}

// registerTurn 登记本轮（request_id → 会话 + 轮次），使重复提交可回放或拒绝。
// 在开始落库前调用：登记成功的 request_id 再次到达时不会重复生成。
// 未携带 request_id 或登记能力不可用时跳过（幂等降级，不阻断问答）。
func (s *ChatSendService) registerTurn(ctx context.Context, in StreamInput, sess *Session, turnID uuid.UUID) error {
	if s.turns == nil || in.RequestID == "" {
		return nil
	}
	payload, err := json.Marshal(turnRef{SID: sess.ID(), TurnID: turnID})
	if err != nil {
		return fmt.Errorf("marshal turn ref: %w", err)
	}
	if err := s.turns.Put(ctx, idempotencyKey(in, in.RequestID), string(payload), idempotencyTTL); err != nil {
		// 登记失败不阻断问答：降级为不幂等（重试可能重复生成），与未启用幂等一致。
		slog.WarnContext(ctx, "chat: idempotency register failed, continue without it", "err", err)
	}
	return nil
}

// replayTurn 处理重复提交（幂等重放）：
//   - 本轮已产生终态 → 回放权威结果（conversation + token + result + done），不重复生成；
//   - 本轮尚无终态（仍在生成中）→ 409，由客户端稍后重试。
//
// 返回 handled=true 表示请求已由幂等逻辑处理完毕（无需继续走生成链路）。
// 登记不可用/损坏时降级为不幂等（handled=false，继续生成）。
func (s *ChatSendService) replayTurn(ctx context.Context, in StreamInput, out SSEWriter) (bool, error) {
	ref := s.lookupTurnRef(ctx, in)
	if ref == nil {
		return false, nil
	}
	if ref.reserved {
		// 已存在"进行中"占位（另一并发请求已原子占用同一 request_id）→ 409，不重复生成。
		return true, apperrors.Conflict("CHAT_TURN_IN_PROGRESS", "会话正在生成中，请稍后重试")
	}
	sess, err := s.sessionByID(ctx, in, ref.SID)
	if err != nil {
		return false, err
	}
	turn, err := sess.store.TurnByID(ctx, ref.TurnID)
	if err != nil {
		return false, fmt.Errorf("load turn: %w", err)
	}
	if turn == nil || turn.Assistant == nil || turn.Assistant.ResultCode == "" {
		return true, apperrors.Conflict("CHAT_TURN_IN_PROGRESS", "会话正在生成中，请稍后重试")
	}
	slog.InfoContext(ctx, "chat: replayed completed turn", "turn_id", ref.TurnID.String())
	if err := out.Write(EventConversation, map[string]string{"conversation_id": sess.ID()}); err != nil {
		return false, err
	}
	if err := out.Write(EventToken, turn.Assistant.Content); err != nil {
		return false, err
	}
	st := &ragStreamState{turnID: ref.TurnID, aiMsgID: turn.Assistant.ID}
	if turn.User != nil {
		st.userMsgID = turn.User.ID
	}
	if err := writeTurnResult(out, st, turn.Assistant.ResultCode, turn.Assistant.ReferencedChunks); err != nil {
		return false, err
	}
	return true, out.Write(EventDone, donePayload())
}

// lookupTurnRef 读取并解析本轮登记。未携带 request_id、登记能力不可用、登记缺失或内容损坏时返回 nil
// （调用方据此降级为不幂等，继续生成）。
func (s *ChatSendService) lookupTurnRef(ctx context.Context, in StreamInput) *turnRef {
	if s.turns == nil || in.RequestID == "" {
		return nil
	}
	raw, ok, err := s.turns.Lookup(ctx, idempotencyKey(in, in.RequestID))
	if err != nil {
		slog.WarnContext(ctx, "chat: idempotency lookup failed, degrade to non-idempotent", "err", err)
		return nil
	}
	if !ok {
		return nil
	}
	// "进行中"占位：另一并发请求已原子占用同一 request_id（尚未产生权威结果）。
	if raw == turnReservedMarker {
		return &turnRef{reserved: true}
	}
	var ref turnRef
	if jerr := json.Unmarshal([]byte(raw), &ref); jerr != nil || ref.TurnID == uuid.Nil {
		slog.WarnContext(ctx, "chat: corrupt idempotency record, degrade to non-idempotent", "err", jerr)
		return nil
	}
	return &ref
}

// clearTurnRegistration 撤销本轮幂等登记（本轮没有服务端持久化结果时，重试应重新生成）。
func (s *ChatSendService) clearTurnRegistration(ctx context.Context, in StreamInput) {
	if s.turns == nil || in.RequestID == "" {
		return
	}
	if err := s.turns.Delete(ctx, idempotencyKey(in, in.RequestID)); err != nil {
		slog.WarnContext(ctx, "chat: idempotency clear failed", "err", err)
	}
}

// sessionByID 按会话标识重建 Session（幂等重放用）：认证=加载自己的会话；匿名=设备命名空间下的会话。
func (s *ChatSendService) sessionByID(ctx context.Context, in StreamInput, sid string) (*Session, error) {
	if in.Identity.Anon() {
		return &Session{SID: sid, DeptID: nil, store: newMemSessionStore(s.ring, in.Identity.DeviceID, sid)}, nil
	}
	convID, err := uuid.Parse(sid)
	if err != nil {
		return nil, apperrors.BadRequest("CHAT_INVALID_CONVERSATION_ID", "conversation_id 格式错误")
	}
	conv, err := s.loadConversation(ctx, convID, in.Identity.UserID)
	if err != nil {
		return nil, err
	}
	if conv == nil {
		return nil, apperrors.NotFound("CHAT_CONVERSATION_NOT_FOUND", "会话不存在或不属于当前用户")
	}
	store := newDBSessionStore(s.conv, s.msg, s.crisis, s.crisisNotifier, s.tx, conv)
	return &Session{
		SID:      conv.ID.String(),
		DeptID:   conv.LockedDeptID,
		DeptName: s.deptNameByID(ctx, conv.LockedDeptID),
		store:    store,
	}, nil
}

// uuidString 返回 uuid 字符串；零值返回空串（匿名为零值，前端保持本地 ID）。
func uuidString(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

// buildLockKey 构造防并发锁 key。认证=user_id + 会话 ID；匿名=会话 ID（已按设备命名空间隔离上下文）。
// sess 为已解析的会话（认证=DB 会话 UUID，匿名=设备内会话标识），保证首轮与后续请求命中同一 key。
func buildLockKey(in StreamInput, sess *Session) string {
	if in.Identity.Anon() {
		return fmt.Sprintf("chat_pending:anon:%s", sess.ID())
	}
	return fmt.Sprintf("chat_pending:%d:%s", in.Identity.UserID, sess.ID())
}

// validateStreamInput 校验消息长度。
func validateStreamInput(in StreamInput) error {
	if in.Message == "" {
		return apperrors.BadRequest("CHAT_MESSAGE_EMPTY", "消息内容不能为空")
	}
	if utf8.RuneCountInString(in.Message) > constants.MaxMessageLength {
		return apperrors.Validation("CHAT_MESSAGE_TOO_LONG", "消息长度超过 2000 字符")
	}
	return nil
}
