package service

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/google/uuid"

	"health-nexus/internal/domain/chat/entity"
	"health-nexus/internal/shared/constants"
	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/rag"
)

// loadConversation 取会话并校验归属；不存在返回 (nil, nil)。
func (s *ChatSendService) loadConversation(
	ctx context.Context, convID uuid.UUID, patientID int64,
) (*entity.Conversation, error) {
	conv, err := s.conv.GetByIDForPatient(ctx, convID, patientID)
	if err != nil {
		return nil, fmt.Errorf("load conversation: %w", err)
	}
	return conv, nil
}

// buildSession 统一构建会话：认证用户承载 DB 会话（含科室锁定与持久化）；
// 匿名用户承载 Redis 瞬态会话（多轮上下文，TTL 自动过期，危机不记录）。
// 链条自此只与 Session 交互，不区分身份。
func (s *ChatSendService) buildSession(ctx context.Context, in StreamInput) (*Session, error) {
	if in.Identity.Anon() {
		// 匿名：会话身份独立于设备身份——同设备可拥有多个会话（新建/续聊语义与认证路径一致）。
		sid := anonSessionID(in)
		deptID, err := s.resolveAnonDept(ctx, in, sid)
		if err != nil {
			return nil, err
		}
		if err := s.assertDeptOnline(ctx, deptID); err != nil {
			return nil, err
		}
		return &Session{
			SID:      sid,
			DeptID:   deptID,
			DeptName: s.deptNameByID(ctx, deptID),
			store:    newMemSessionStore(s.ring, in.Identity.DeviceID, sid),
		}, nil
	}
	_, conv, err := s.resolveDeptAndConversation(ctx, in)
	if err != nil {
		return nil, err
	}
	effDeptID := deptIDPtr(in.SelectedDeptID, conv)
	// 未指定/显式"全部科室"（nil）→ 落到默认责任科室，保证统一理解与审查始终有科室语境
	// （chat.default_dept_id / 首个公共科室），避免对常规宣教问题机械追问。
	if effDeptID == nil {
		if d, derr := s.dept.ResolveDefault(ctx); derr != nil {
			slog.WarnContext(ctx, "chat: resolve default dept failed, fall back to all depts", "err", derr)
		} else if d.ID > 0 {
			effDeptID = &d.ID
		}
	}
	if err := s.assertDeptOnline(ctx, effDeptID); err != nil {
		return nil, err
	}
	store := newDBSessionStore(s.conv, s.msg, s.crisis, s.crisisNotifier, s.tx, conv)
	return &Session{
		SID:    conv.ID.String(),
		DeptID: effDeptID,
		// 按最终锁定的检索科室 id 取名：续聊（未带 selected_dept_id）时
		// resolveDeptAndConversation 解析到的可能是患者主科室，其名与锁定科室不一致。
		DeptName: s.deptNameByID(ctx, effDeptID),
		store:    store,
	}, nil
}

// assertDeptOnline 复核检索科室仍在线：科室禁用 = 该科室整体知识库下线，
// 已锁定（或默认）到该科室的会话不得继续静默作答——医疗场景宁可明确失败，
// 也不在已下线的知识域里生成答案。科室不存在（已删除）同样视为不可用。
// nil 表示不限定科室（全部科室检索），由检索层按启用状态过滤，无需校验。
func (s *ChatSendService) assertDeptOnline(ctx context.Context, deptID *int64) error {
	if deptID == nil {
		return nil
	}
	d, err := s.dept.ResolveByID(ctx, *deptID)
	if err != nil {
		return fmt.Errorf("resolve dept online status: %w", err)
	}
	if d.ID == 0 || !d.IsActive {
		return apperrors.Conflict("CHAT_DEPT_INACTIVE", "所选科室的知识库已停用，请返回重新选择科室")
	}
	return nil
}

// deptNameByID 按科室 id 取名（best-effort，供统一理解与审查作为语境提示）。
// deptID 为 nil（不限科室）返回空串；解析失败仅告警并返回空串——科室名缺失时审查侧
// 退化为"未指定"，不得因此阻断问答（检索范围仍由 DeptID 决定）。
func (s *ChatSendService) deptNameByID(ctx context.Context, deptID *int64) string {
	if deptID == nil {
		return ""
	}
	d, err := s.dept.ResolveByID(ctx, *deptID)
	if err != nil {
		slog.WarnContext(ctx, "chat: resolve dept name by id failed", "dept_id", *deptID, "err", err)
		return ""
	}
	return d.Name
}

// resolveAnonDept 解析匿名会话的检索科室范围（REQ-CHAT-019）：
//   - 首轮携带 selected_dept_id（>0）：校验其属于公共启用科室，通过后锁定并持久化，
//     续聊（仅带 conversation_id）复用锁定值，避免匿名用户绕过科室选择或携带任意 id；
//   - selected_dept_id 为 0/nil：不限科室（返回 nil），并清除续聊会话上的锁定值。
//
// 原实现直接 DeptID: nil 忽略用户选择，且不校验 id 是否存在（P2）。
func (s *ChatSendService) resolveAnonDept(
	ctx context.Context, in StreamInput, sid string,
) (*int64, error) {
	if in.ConversationID != nil {
		// 续聊：以首轮锁定的科室为准；仅在明确携带新选择时更新锁定。
		if in.SelectedDeptID == nil {
			if id := s.loadAnonDept(ctx, in.Identity.DeviceID, sid); id != nil {
				return id, nil
			}
			// 旧会话无锁定记录 → 落到默认责任科室并补写锁定。
			return s.storeDefaultAnonDept(ctx, in, sid)
		}
	}
	dept, err := s.dept.ResolveForAnonymous(ctx, in.SelectedDeptID)
	if err != nil {
		return nil, err
	}
	if dept.ID <= 0 {
		// 显式"全部科室"（0/nil）→ 落到默认责任科室。chat 前端已取消"全部科室"，
		// 此处为旧客户端/历史请求兜底，保证不会以"无科室语境"进入审查。
		return s.storeDefaultAnonDept(ctx, in, sid)
	}
	id := dept.ID
	s.storeAnonDept(ctx, in.Identity.DeviceID, sid, &id)
	return &id, nil
}

// storeDefaultAnonDept 把匿名会话锁到默认责任科室（chat.default_dept_id / 首个公共启用科室）。
// 无可用科室时清除锁定并返回 nil（保持"不限科室"的旧降级）。
func (s *ChatSendService) storeDefaultAnonDept(
	ctx context.Context, in StreamInput, sid string,
) (*int64, error) {
	d, err := s.dept.ResolveDefault(ctx)
	if err != nil {
		return nil, err
	}
	if d.ID <= 0 {
		s.storeAnonDept(ctx, in.Identity.DeviceID, sid, nil)
		return nil, nil
	}
	id := d.ID
	s.storeAnonDept(ctx, in.Identity.DeviceID, sid, &id)
	return &id, nil
}

// anonDeptKey 匿名会话锁定科室的 Redis key（与消息环同命名空间，TTL 一致）。
func anonDeptKey(deviceID, sid string) string {
	return "chat_anon_dept:" + deviceID + ":" + sid
}

// loadAnonDept 读取匿名会话锁定的科室；无记录或能力不可用时返回 nil（不限科室）。
func (s *ChatSendService) loadAnonDept(ctx context.Context, deviceID, sid string) *int64 {
	if s.turns == nil {
		return nil
	}
	raw, ok, err := s.turns.Lookup(ctx, anonDeptKey(deviceID, sid))
	if err != nil || !ok {
		return nil
	}
	id, perr := strconv.ParseInt(raw, 10, 64)
	if perr != nil || id <= 0 {
		return nil
	}
	return &id
}

// storeAnonDept 持久化/清除匿名会话锁定科室（尽力而为，失败仅告警不阻断问答）。
func (s *ChatSendService) storeAnonDept(ctx context.Context, deviceID, sid string, deptID *int64) {
	if s.turns == nil {
		return
	}
	if deptID == nil {
		if err := s.turns.Delete(ctx, anonDeptKey(deviceID, sid)); err != nil {
			slog.WarnContext(ctx, "chat: clear anon dept failed", "err", err)
		}
		return
	}
	val := strconv.FormatInt(*deptID, 10)
	if err := s.turns.Put(ctx, anonDeptKey(deviceID, sid), val, anonymousContextTTL); err != nil {
		slog.WarnContext(ctx, "chat: persist anon dept failed", "err", err)
	}
}

// anonSessionID 解析匿名会话标识：请求携带 conversation_id 表示续聊该会话，否则分配全新会话。
// 不得再由 device_id 稳定派生——否则"新对话"会复用同一标识、读到上一段的 Redis 历史。
func anonSessionID(in StreamInput) string {
	if in.ConversationID != nil {
		return in.ConversationID.String()
	}
	return uuid.NewString()
}

// resolveDeptAndConversation 非匿名用户的科室范围校验 + 会话加载/创建。
func (s *ChatSendService) resolveDeptAndConversation(
	ctx context.Context, in StreamInput,
) (rag.Department, *entity.Conversation, error) {
	dept, err := s.dept.ResolveForPatient(ctx, in.Identity.UserID, in.SelectedDeptID)
	if err != nil {
		return rag.Department{}, nil, fmt.Errorf("resolve dept: %w", err)
	}
	conv, err := s.loadOrPrepareConversation(ctx, in, &dept)
	if err != nil {
		return rag.Department{}, nil, err
	}
	return dept, conv, nil
}

// writeEmergencyNotice 紧急症状预提醒：命中时以独立提示事件下发（REQ-CHAT-010）。
// 独立提示不进入答案正文——正文即持久化内容，混入提示会导致刷新后展示不一致。
func (s *ChatSendService) writeEmergencyNotice(ctx context.Context, in StreamInput, out SSEWriter) error {
	if hits := s.safetyIn.EmergencyCheck(ctx, in.Message); len(hits) > 0 {
		return out.Write(EventNotice, noticePayload{
			Kind: NoticeEmergency,
			Text: s.safetyIn.EmergencyMessage(),
		})
	}
	return nil
}

// PurgeAnonSession 清除匿名会话的服务端瞬态上下文（匿名"删除对话"入口）。
// 环 key 含设备标识命名空间，携带他人会话 ID 不会命中他人上下文。
func (s *ChatSendService) PurgeAnonSession(ctx context.Context, deviceID, conversationID string) error {
	if s.ring == nil {
		return nil // 无 Redis 环时匿名上下文本就不存在
	}
	if err := s.ring.Del(ctx, anonRingKey(deviceID, conversationID)); err != nil {
		return fmt.Errorf("purge anon session: %w", err)
	}
	return nil
}

// loadOrPrepareConversation 取已有会话（含所属 + 锁定科室校验），或新建会话（独立 tx 内 Create）。
// 新会话提前创建——使后续防并发锁 lockKey 基于真实 conv.ID，避免 uuid.Nil 造成同一用户新会话串行化，
// 以及创建后真实 ID 与 lockKey 不一致导致的同会话双流并发漏洞。
// ponytail: 代价——若后续用户消息持久化失败会留下空会话，折中；
// 这比 uuid.Nil lockKey 导致的并发漏洞（同会话双流、消息乱序、危机事件重复）影响小得多。
// 升级路径：若空会话成为问题，可在 lock 失败时标记会话为 archived 或由后台清理任务回收。
// ponytail: 孤儿会话清理——在 cleanupRAGStream 中，若 placeholder 为空且会话无其他消息，
// 可标记会话为 archived。当前未实现，因空会话对用户不可见（无消息时不展示在列表中）。
func (s *ChatSendService) loadOrPrepareConversation(
	ctx context.Context, in StreamInput, dept *rag.Department,
) (*entity.Conversation, error) {
	if in.ConversationID == nil {
		// 新会话：以 selected_dept_id 锁定（若 provided 且 > 0），nil 或 0 表示不限定科室。
		var lockedDeptID *int64
		if in.SelectedDeptID != nil && *in.SelectedDeptID > 0 {
			lockedDeptID = in.SelectedDeptID
		}
		// SelectedDeptID 为 nil 或 0 时 lockedDeptID 保持 nil——检索全部科室。
		var newConv *entity.Conversation
		err := s.tx.WithTx(ctx, func(ctx context.Context) error {
			c, err := s.conv.Create(ctx, in.Identity.UserID, lockedDeptID)
			if err != nil {
				return fmt.Errorf("create conversation: %w", err)
			}
			newConv = c
			return nil
		})
		if err != nil {
			return nil, err
		}
		return newConv, nil
	}
	conv, err := s.loadConversation(ctx, *in.ConversationID, in.Identity.UserID)
	if err != nil {
		return nil, err
	}
	if conv == nil {
		return nil, apperrors.NotFound("CHAT_CONVERSATION_NOT_FOUND", "会话不存在或不属于当前用户")
	}
	// 会话锁定后禁止切换科室（含切到"全部科室"），保持多轮上下文一致性：
	//   - 已锁定具体科室：请求必须为 nil 或等于锁定值；
	//   - 全部科室会话（locked_dept_id=NULL）：请求必须为 nil 或 0，禁止再锁定具体科室。
	// 后端兜底；前端在 openDeptPicker 同步锁定切换入口（CHAT_DEPT_LOCKED）。
	if conv.LockedDeptID != nil {
		if in.SelectedDeptID != nil && *in.SelectedDeptID != *conv.LockedDeptID {
			return nil, apperrors.Conflict("CHAT_DEPT_LOCKED", "会话中禁止切换知识库")
		}
		dept.ID = *conv.LockedDeptID
	} else if in.SelectedDeptID != nil && *in.SelectedDeptID > 0 {
		return nil, apperrors.Conflict("CHAT_DEPT_LOCKED", "会话中禁止切换知识库")
	}
	return conv, nil
}

// handleCrisis 命中危机（规则层关键词，或 LLM 层判定自伤风险）：
// 持久化危机（认证=落库危机事件并通知医护；匿名=空操作——不汇报不记录）并下发危机热线。
// 持久化失败不阻断 SSE——无论成败都推送 crisis 热线，确保患者收到救命信息（REQ-CHAT-008 / R7-1）。
// 结果码 CRISIS 经 result 事件回传，前端据此渲染（不再混入提示事件）。
func (s *ChatSendService) handleCrisis(
	ctx context.Context, in StreamInput, sess *Session, c *rag.Crisis, out SSEWriter, st *ragStreamState,
) error {
	hotline := s.safetyIn.CrisisResponse()
	// 匿名会话不记录危机、不通知医护：必须明确告知患者服务边界，
	// 避免患者误以为"医护已收到并正在处理"而放弃自救（P1）。
	if in.Identity.Anon() {
		hotline += "\n\n" + rag.AnonymousCrisisNote()
	}
	// DB store 内部含一次性重试并记录告警；匿名 store 为空操作。不阻断热线下发。
	userID, aiID, err := sess.store.PersistCrisis(
		ctx, in.Identity.UserID, TurnWrite{Content: in.Message, TurnID: st.turnID}, c, hotline,
	)
	if err != nil {
		slog.ErrorContext(ctx, "chat crisis persist failed, still pushing hotline", "err", err)
	}
	st.userMsgID, st.aiMsgID = userID, aiID
	if aiID == uuid.Nil {
		// 无服务端持久化结果（匿名危机不记录）：撤销本轮登记，避免后续重试被判"生成中"。
		s.clearTurnRegistration(ctx, in)
	}

	// 无论事务是否成功，都推送 crisis 热线（心理援助话术，已含热线号码）。
	if err := out.Write(EventCrisis, map[string]any{"answer": hotline}); err != nil {
		return err
	}
	if err := writeTurnResult(out, st, constants.ResultCrisis, nil); err != nil {
		return err
	}
	// SSE 协议（spec §3.1）：done=[DONE] 终止流。
	if err := out.Write(EventDone, donePayload()); err != nil {
		return err
	}
	slog.InfoContext(ctx, "chat: request completed", "result_code", constants.ResultCrisis)
	return nil
}
