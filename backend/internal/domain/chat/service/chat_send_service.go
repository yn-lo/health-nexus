// Package service 实现 chat 域业务编排：RAG 流式问答、会话管理、危机事件处理。
// 事务边界在本层开启（postgres.TxManager.WithTx），Repository 接收 ctx 内的 tx。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"health-nexus/internal/domain/chat/entity"
	"health-nexus/internal/platform/llm"
	"health-nexus/internal/platform/redis"
	"health-nexus/internal/shared/constants"
	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/identity"
	"health-nexus/internal/shared/rag"
)

// SSE 事件名（spec §3.1）。语义拆分：正文修正（answer_replaced）与独立提示（notice）
// 不再共用一个事件，避免"提示被拼进答案正文"导致实时展示与持久化内容不一致。
const (
	EventConversation = "conversation"
	EventToken        = "token"
	EventReferences   = "references"
	EventCrisis       = "crisis"
	EventDone         = "done"
	EventError        = "error"
	// EventAnswer 正文修正：mode=replace 覆盖已累积正文；mode=append 追加到正文末尾。
	// 生命周期与答案一致——修正后的正文即持久化内容。
	EventAnswer = "answer_replaced"
	// EventNotice 面向用户的独立提示（紧急就医提醒 / 超时提示）：不进入答案正文，仅作 UI 提示。
	EventNotice = "notice"
	// EventResult 本轮权威结果：真实消息 ID、最终 result_code、最终引用。
	// 前端据此替换本地乐观消息，无需"猜结果码 + 整页回拉"。
	EventResult = "result"
)

// notice 事件的 kind（前端据此选择展示样式）。
const (
	NoticeEmergency = "emergency"
	NoticeTimeout   = "timeout"
)

// 正文修正模式（answer_replaced 事件 mode 字段）。
const (
	answerModeReplace = "replace"
	answerModeAppend  = "append"
)

// noticePayload 独立提示载荷。
type noticePayload struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// answerPayload 正文修正载荷。
type answerPayload struct {
	Mode string `json:"mode"`
	Text string `json:"text"`
}

// turnResultPayload 本轮权威结果载荷。
type turnResultPayload struct {
	TurnID             string             `json:"turn_id"`
	UserMessageID      string             `json:"user_message_id"`
	AssistantMessageID string             `json:"assistant_message_id"`
	ResultCode         string             `json:"result_code"`
	References         []entity.Reference `json:"references"`
}

// SSEWriter SSE 事件写入接口，由 handler 层实现（消费者定义在 service 端）。
// 每个 token / 引用 / 危机事件通过 Write 推送给客户端并立即 flush。
type SSEWriter interface {
	Write(event string, data any) error
}

// StreamInput 流式问答输入。
type StreamInput struct {
	Identity       identity.Identity // 请求身份载体（认证用户或匿名设备），见 shared/identity
	ConversationID *uuid.UUID        // nil = 新建会话
	SelectedDeptID *int64            // nil = 不限定；会话已锁定时必须与锁定值一致
	Message        string
	// RequestID 客户端生成的幂等标识（本次发送的多次重试复用同一值）：
	// 同一 request_id 已完成 → 回放权威结果；进行中 → 409；为空则不启用幂等。
	RequestID string
}

// chatPendingLockTTL 会话并发锁 TTL：5 分钟覆盖单次 LLM 流式生成最坏时长。
const chatPendingLockTTL = 5 * time.Minute

// llmStreamTimeout LLM 流式调用硬 deadline：4 分钟，留 1 分钟余量给收尾事务 + SSE flush。
// 与 chatPendingLockTTL 对齐，覆盖 LLM 服务 stall（首字节后中途 hang）场景，避免 goroutine 泄漏。
const llmStreamTimeout = 4 * time.Minute

// idempotencyTTL 幂等登记保留时长：覆盖客户端自动重试与短时断线重连窗口。
// 过期后同一 request_id 视为新请求（前端每次发送都会生成新的 request_id）。
const idempotencyTTL = 15 * time.Minute

// ChatSendService RAG 核心服务，编排阶段 1（输入安全 + 用户消息持久化）、
// 阶段 2（检索 + 流式生成）、阶段 3（AI 消息持久化 + SSE 结束事件）。
type ChatSendService struct {
	dept           rag.DepartmentResolver
	safetyIn       rag.InputSafetyFilter
	safetyOut      rag.OutputSafetyFilter
	assessor       rag.Assessor       // 每轮统一理解与审查（意图 + 风险 + 检索改写）
	outputReviewer rag.OutputReviewer // 高风险答案的生成后语义审核（可为 nil：跳过并告警）
	knowledge      rag.KnowledgeSearcher
	llm            llm.Streamer
	promptProvider rag.SystemPromptProvider // 可为 nil：降级为 defaultSystemPrompt
	conv           ConversationPort
	msg            MessagePort
	crisis         CrisisPort
	crisisNotifier CrisisNotifier // 危机事件主动通知（入队 asynq 任务，落库站内通知给 DEPT_ADMIN）
	locker         LockProvider
	tx             TxRunner
	ring           ringStore    // 匿名会话瞬态上下文环（Redis）；nil 时匿名退化为单轮（无历史）
	turns          TurnRegistry // 请求幂等登记；nil 时不启用幂等（重复提交会重复生成）
}

// CrisisNotifier 危机事件主动通知接口（入队 asynq 任务，由 worker 落库站内通知）。
type CrisisNotifier interface {
	NotifyCrisis(ctx context.Context, eventID int64) error
}

// NewChatSendService 构造 RAG 服务。
// 跨域依赖（dept/knowledge/safetyIn/safetyOut/assessor/outputReviewer/promptProvider）通过 interface 注入。
// assessor 可为 nil：视为审查不可用，每轮按"无法判断"降级（受限兜底），不会放行为安全。
// outputReviewer 可为 nil：高风险答案跳过语义审核并记录告警（不阻断普通宣教）。
// promptProvider 可为 nil：降级为 defaultSystemPrompt（保持修复前行为，便于测试与渐进接入）。
// turns 可为 nil：不启用请求幂等（重复提交会重复生成，行为与修复前一致）。
func NewChatSendService(
	dept rag.DepartmentResolver,
	safetyIn rag.InputSafetyFilter,
	safetyOut rag.OutputSafetyFilter,
	assessor rag.Assessor,
	outputReviewer rag.OutputReviewer,
	knowledge rag.KnowledgeSearcher,
	llmStreamer llm.Streamer,
	conv ConversationPort,
	msg MessagePort,
	crisis CrisisPort,
	crisisNotifier CrisisNotifier,
	locker LockProvider,
	tx TxRunner,
	ring ringStore,
	turns TurnRegistry,
	promptProvider rag.SystemPromptProvider,
) *ChatSendService {
	return &ChatSendService{
		dept: dept, safetyIn: safetyIn, safetyOut: safetyOut,
		assessor: assessor, outputReviewer: outputReviewer, knowledge: knowledge,
		llm:  llmStreamer,
		conv: conv, msg: msg, crisis: crisis, crisisNotifier: crisisNotifier,
		locker: locker, tx: tx, ring: ring, turns: turns,
		promptProvider: promptProvider,
	}
}

// Stream 执行 RAG 三阶段流式问答。
// 认证用户与匿名用户统一为 Session 后进入同一链条；差异收敛到 Session.Store 实现
// （认证=DB 会话，匿名=Redis 瞬态会话）。链条自此不感知用户身份。
// 错误统一返回 AppError 由 handler 写 SSE error 事件或 HTTP 错误响应。
func (s *ChatSendService) Stream(ctx context.Context, in StreamInput, out SSEWriter) error {
	// 输入校验
	if err := validateStreamInput(in); err != nil {
		return err
	}

	// (0) 请求幂等（重复提交）：同一 request_id 已完成 → 回放权威结果；进行中 → 409。
	// 须在会话准备之前：重复请求不应再次创建会话或再次生成。
	if handled, err := s.replayTurn(ctx, in, out); err != nil || handled {
		return err
	}

	// (1)~(2) 会话准备（REQ-CHAT-019）：统一构建 Session（认证=DB 会话+科室锁定；匿名=Redis 瞬态会话，不限科室）。
	sess, err := s.buildSession(ctx, in)
	if err != nil {
		return err
	}

	// (3) 防并发锁（REQ-NFR-012）——key 随身份（认证=user+conv；匿名=device 会话）。
	// 须在 conversation 事件之前获取：锁失败（并发生成）时 wroteAny=false，handler 回退 HTTP 409
	// （符合 Conflict 语义，客户端可据此重试）；若先发 conversation 事件，错误会降级为 SSE error 事件（HTTP 200）。
	unlock, err := s.acquireStreamLock(ctx, in, sess)
	if err != nil {
		return err
	}
	defer unlock()

	// 本轮状态（幂等登记与权威结果共用同一 turn_id）。
	st := &ragStreamState{turnID: uuid.New()}

	// (2.5) 回传会话 ID（升级/已有均下发，锁获取成功后首个 SSE 事件）：认证为会话 UUID，匿名为设备内会话 id。
	// 前端据此更新 URL 与后续请求的 conversation_id，匿名用户据此维持多轮上下文标识。
	if err := out.Write(EventConversation, map[string]string{"conversation_id": sess.ID()}); err != nil {
		return err
	}

	// (4) 紧急症状预提醒（REQ-CHAT-010）：独立提示事件，不进入答案正文。
	if err := s.writeEmergencyNotice(ctx, in, out); err != nil {
		return err
	}

	// (5) 规则层安全审查（零延迟快筛，REQ-NFR-005/007）。
	// 只做明确高风险表达的快速保护（自杀关键词 / Prompt 注入），命中即拦截；
	// 未命中不代表安全——语义审查由 (7) 的统一理解与审查每轮完成。
	if decision, crisis := s.safetyIn.CheckRules(ctx, in.Message); decision == rag.DecisionBlock {
		return s.handleRuleBlocked(ctx, in, sess, out, crisis, st)
	}

	// (6) LLM 就绪性预检
	if !s.llm.IsReady() {
		return apperrors.ServiceUnavailable("CHAT_LLM_UNAVAILABLE", "AI 服务暂不可用，请稍后重试")
	}

	// (7) 统一理解与审查（每轮执行）：意图 + 多分类风险 + 是否缺关键信息 + 检索改写。
	// 不再"命中少量关键词片段才让 LLM 审查"——首轮可能是普通宣教，风险常出现在后续轮次。
	// AI 负责判断，后端按固定优先级执行动作（Assessment.Action）。
	assessment, aerr := s.assess(ctx, sess, in)
	if aerr != nil {
		// 审查不可用（超时/解析失败/字段非法）：按"无法判断"降级，绝不回退为"安全放行"。
		slog.WarnContext(ctx, "chat: assessment unavailable, degrading to restricted reply", "err", aerr)
		if err := s.registerTurn(ctx, in, sess, st.turnID); err != nil {
			return err
		}
		return s.handleFixedReply(ctx, in, sess, out, rag.AssessmentFailedMessage(), constants.ResultRejected, st)
	}

	// 动作分流：所有分支都先登记本轮（结果可被重复提交回放）。
	if handled, derr := s.dispatchByAssessment(ctx, in, sess, out, assessment, st); handled {
		return derr
	}

	// 普通宣教：登记本轮后进入检索 + 生成链路。
	if err := s.registerTurn(ctx, in, sess, st.turnID); err != nil {
		return err
	}
	return s.stageRAG(ctx, in, sess, out, st, assessment)
}

// acquireStreamLock 获取会话防并发锁，返回释放函数。
// key 必须基于已解析的 sess.ID()：新会话首轮请求未携带 conversation_id，
// 若沿用请求中的原始 ID（"new" 占位），前端拿到会话 ID 后的第二个请求会落到另一个 key，同会话互斥失效。
// 并发生成时返回 Conflict（客户端可据此重试）。
func (s *ChatSendService) acquireStreamLock(
	ctx context.Context, in StreamInput, sess *Session,
) (func(), error) {
	lockKey := buildLockKey(in, sess)
	unlock, err := s.locker.Lock(ctx, lockKey, chatPendingLockTTL)
	if err != nil {
		if errors.Is(err, redis.ErrLockNotAcquired) {
			return nil, apperrors.Conflict("CHAT_CONCURRENT_STREAM", "会话正在生成中，请稍后重试")
		}
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	// E2E EDGE-CONC-001 修复：unlock 失败不再静默吞错——锁泄漏会导致同用户后续请求
	// 持续 409 至 TTL（5min）过期，必须有日志可观测（根因定位依赖此告警）。
	return func() {
		if uerr := unlock(); uerr != nil {
			slog.WarnContext(ctx, "chat: release pending lock failed, will expire by TTL",
				"lock_key", lockKey, "ttl", chatPendingLockTTL.String(), "err", uerr)
		}
	}, nil
}

// dispatchByAssessment 按统一审查的动作分流到固定话术 / 危机流程。
// 返回 handled=true 表示本轮已处理完毕（调用方直接返回 err）；false 表示应继续走检索 + 生成链路。
// 所有分支都先登记本轮，使重复提交可回放权威结果。
func (s *ChatSendService) dispatchByAssessment(
	ctx context.Context, in StreamInput, sess *Session, out SSEWriter,
	assessment rag.Assessment, st *ragStreamState,
) (bool, error) {
	register := func() error { return s.registerTurn(ctx, in, sess, st.turnID) }
	switch assessment.Action() {
	case rag.ActionCrisis:
		if err := register(); err != nil {
			return true, err
		}
		return true, s.handleCrisis(ctx, in, sess, &rag.Crisis{
			Keywords: assessmentCrisisKeywords(assessment),
			Level:    constants.CrisisLevelHigh,
		}, out, st)
	case rag.ActionEmergency:
		// 疑似急症：固定急救指引，不再继续生成可能与提醒冲突的日常护理答案（REQ-CHAT-010）。
		if err := register(); err != nil {
			return true, err
		}
		return true, s.handleFixedReply(ctx, in, sess, out, rag.EmergencyGuidance(), constants.ResultRejected, st)
	case rag.ActionReject:
		if err := register(); err != nil {
			return true, err
		}
		return true, s.handleFixedReply(ctx, in, sess, out,
			s.safetyIn.RejectionMessage(), constants.ResultIntercepted, st)
	case rag.ActionRestricted:
		// 个体化诊疗 / 用药调整：只给固定边界说明与求助渠道，不做个体化生成。
		if err := register(); err != nil {
			return true, err
		}
		return true, s.handleFixedReply(ctx, in, sess, out,
			rag.RestrictedCareMessage(), constants.ResultRejected, st)
	case rag.ActionClarify:
		// 关键信息不足：先澄清，不检索不生成（避免用不适用的一般性资料作答）。
		if err := register(); err != nil {
			return true, err
		}
		return true, s.handleFixedReply(ctx, in, sess, out,
			clarificationText(assessment.ClarificationQuestion), constants.ResultRejected, st)
	}
	return false, nil
}

// assessmentCrisisKeywords 构造危机事件的命中关键词：优先记录模型给出的原话依据，
// 便于医护端复现判定依据；无依据时退化为标记值。
func assessmentCrisisKeywords(a rag.Assessment) []string {
	if len(a.RiskEvidence) > 0 {
		return a.RiskEvidence
	}
	return []string{llmSelfHarmMarker}
}

// clarificationText 拼装澄清话术：固定前缀 + 具体澄清问题。
// 模型未给出具体问题时退回通用澄清话术（审查已成功，不得误报为"审查失败"）。
func clarificationText(question string) string {
	q := strings.TrimSpace(question)
	if q == "" {
		return rag.ClarificationFallback()
	}
	return rag.ClarificationPrompt() + q
}

// assess 执行统一理解与审查。
// 历史在用户消息落库之前读取（本轮尚未写入，无需排除）；历史加载失败不阻断审查，降级为仅当前输入。
func (s *ChatSendService) assess(
	ctx context.Context, sess *Session, in StreamInput,
) (rag.Assessment, error) {
	if s.assessor == nil {
		return rag.Assessment{}, fmt.Errorf("%w: assessor not injected", rag.ErrAssessmentInvalid)
	}
	history, err := sess.store.History(ctx, constants.HistoryTurns, nil)
	if err != nil {
		slog.WarnContext(ctx, "chat: load history for assess failed, use current message only", "err", err)
		history = nil
	}
	return s.assessor.AssessAndRewrite(ctx, in.Message, toAssessTurns(history))
}

// toAssessTurns 把历史消息转为审查输入（只保留角色与内容）。
func toAssessTurns(history []*entity.Message) []rag.AssessTurn {
	if len(history) == 0 {
		return nil
	}
	out := make([]rag.AssessTurn, 0, len(history))
	for _, m := range history {
		out = append(out, rag.AssessTurn{Role: m.Role, Content: m.Content})
	}
	return out
}

// handleRuleBlocked 规则层命中后的分流：危机关键词 → 危机链路；注入 → 拒答。
// 两者都先登记本轮（结果可被重复提交回放）。
func (s *ChatSendService) handleRuleBlocked(
	ctx context.Context, in StreamInput, sess *Session, out SSEWriter, c *rag.Crisis, st *ragStreamState,
) error {
	if err := s.registerTurn(ctx, in, sess, st.turnID); err != nil {
		return err
	}
	if c != nil {
		return s.handleCrisis(ctx, in, sess, c, out, st)
	}
	return s.handleFixedReply(ctx, in, sess, out, s.safetyIn.RejectionMessage(), constants.ResultRejected, st)
}

// llmSelfHarmMarker 统一审查判定自伤风险但未给出原话依据时，写入危机事件的标记值
// （供医护端区分判定来源）。
const llmSelfHarmMarker = "llm_self_harm"

// turnRef 幂等登记值：本轮所属会话 + 轮次标识（uuid.UUID 按文本序列化）。
type turnRef struct {
	SID    string    `json:"sid"`
	TurnID uuid.UUID `json:"turn_id"`
}

// idempotencyKey 身份作用域 + request_id：跨身份无法命中他人的登记，
// 避免伪造 request_id 读取他人本轮结果。
func idempotencyKey(in StreamInput, requestID string) string {
	if in.Identity.Anon() {
		return "chat_turn:anon:" + in.Identity.DeviceID + ":" + requestID
	}
	return fmt.Sprintf("chat_turn:user:%d:%s", in.Identity.UserID, requestID)
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
	var ref turnRef
	if jerr := json.Unmarshal([]byte(raw), &ref); jerr != nil || ref.TurnID == uuid.Nil {
		slog.WarnContext(ctx, "chat: corrupt idempotency record, degrade to non-idempotent", "err", jerr)
		return nil
	}
	return &ref
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
	return &Session{SID: conv.ID.String(), DeptID: conv.LockedDeptID, store: store}, nil
}

// donePayload SSE 流结束标记（spec §3.1：data 为字面量 [DONE]）。
func donePayload() string { return "[DONE]" }

// writeTurnResult 推送本轮权威结果：真实消息 ID、最终 result_code、最终引用。
// 前端据此替换本地乐观消息（含反馈目标 ID），不再按内容猜测或整页回拉。
func writeTurnResult(out SSEWriter, st *ragStreamState, resultCode string, refs []entity.Reference) error {
	if refs == nil {
		refs = []entity.Reference{}
	}
	return out.Write(EventResult, turnResultPayload{
		TurnID:             st.turnID.String(),
		UserMessageID:      uuidString(st.userMsgID),
		AssistantMessageID: uuidString(st.aiMsgID),
		ResultCode:         resultCode,
		References:         refs,
	})
}

// uuidString 返回 uuid 字符串；零值返回空串（匿名为零值，前端保持本地 ID）。
func uuidString(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

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
// 匿名用户承载 Redis 瞬态会话（多轮上下文，TTL 自动过期，不限科室，危机不记录）。
// 链条自此只与 Session 交互，不区分身份。
func (s *ChatSendService) buildSession(ctx context.Context, in StreamInput) (*Session, error) {
	if in.Identity.Anon() {
		// 匿名：会话身份独立于设备身份——同设备可拥有多个会话（新建/续聊语义与认证路径一致）。
		sid := anonSessionID(in)
		return &Session{SID: sid, DeptID: nil, store: newMemSessionStore(s.ring, in.Identity.DeviceID, sid)}, nil
	}
	_, conv, err := s.resolveDeptAndConversation(ctx, in)
	if err != nil {
		return nil, err
	}
	store := newDBSessionStore(s.conv, s.msg, s.crisis, s.crisisNotifier, s.tx, conv)
	return &Session{
		SID:    conv.ID.String(),
		DeptID: deptIDPtr(in.SelectedDeptID, conv),
		store:  store,
	}, nil
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

// buildLockKey 构造防并发锁 key。认证=user_id + 会话 ID；匿名=会话 ID（已按设备命名空间隔离上下文）。
// sess 为已解析的会话（认证=DB 会话 UUID，匿名=设备内会话标识），保证首轮与后续请求命中同一 key。
func buildLockKey(in StreamInput, sess *Session) string {
	if in.Identity.Anon() {
		return fmt.Sprintf("chat_pending:anon:%s", sess.ID())
	}
	return fmt.Sprintf("chat_pending:%d:%s", in.Identity.UserID, sess.ID())
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

// clearTurnRegistration 撤销本轮幂等登记（本轮没有服务端持久化结果时，重试应重新生成）。
func (s *ChatSendService) clearTurnRegistration(ctx context.Context, in StreamInput) {
	if s.turns == nil || in.RequestID == "" {
		return
	}
	if err := s.turns.Delete(ctx, idempotencyKey(in, in.RequestID)); err != nil {
		slog.WarnContext(ctx, "chat: idempotency clear failed", "err", err)
	}
}

// handleFixedReply 固定话术应答：落库用户消息 + assistant 终态，并把话术作为**本轮答案正文**下发
// （answer_replaced），使前端展示与持久化内容一致；结果码经 result 事件回传。
// 用于统一审查的各个非生成分支（拒答 / 受限说明 / 急症指引 / 澄清 / 审查不可用兜底）——
// 这些分支一律使用代码内固定话术，不调用生成模型，避免与安全提示冲突的内容流出。
// resultCode 区分：规则层拒答 ResultRejected，语义层拦截 ResultIntercepted。
func (s *ChatSendService) handleFixedReply(
	ctx context.Context, in StreamInput, sess *Session, out SSEWriter, reply, resultCode string, st *ragStreamState,
) error {
	// 用户消息 + 占位同一事务落库，再把占位写成固定终态——不产生孤立 user 消息。
	userMsg, aiMsgID, err := sess.store.SaveUserAndPlaceholder(ctx, TurnWrite{
		Content: in.Message, TurnID: st.turnID, DeptID: sess.DeptID,
	})
	if err != nil {
		return err
	}
	st.userMsgID, st.aiMsgID = userMsg.ID, aiMsgID
	if err := sess.store.FinalizeAssistant(ctx, aiMsgID, st.turnID, reply, resultCode, nil); err != nil {
		return err
	}
	if err := out.Write(EventAnswer, answerPayload{Mode: answerModeReplace, Text: reply}); err != nil {
		return err
	}
	if err := writeTurnResult(out, st, resultCode, nil); err != nil {
		return err
	}
	if err := out.Write(EventDone, donePayload()); err != nil {
		return err
	}
	slog.InfoContext(ctx, "chat: request completed", "result_code", resultCode)
	return nil
}

// ragCleanupTimeout defer 清理用独立超时--请求 ctx 可能已取消，故用 context.Background() + 5s。
const ragCleanupTimeout = 5 * time.Second

// ragStreamState 阶段 2.7 流式生成的可变状态，供 streamLLMTokens 累积、
// cleanupRAGStream 在 defer 中据 streamCompleted/finalized 决定清理路径。
// content 与 pending 分离：content 是"已通过输出安全审查、已推送给客户端"的内容（也即最终持久化内容），
// pending 是尚未凑满一句的尾部缓冲——未审查内容绝不发给客户端。
type ragStreamState struct {
	turnID          uuid.UUID // 本轮生成标识：user / assistant 消息与权威结果事件共享
	userMsgID       uuid.UUID // 本轮用户消息 ID（权威结果事件回传，供前端替换本地乐观消息）
	aiMsgID         uuid.UUID
	chunks          []rag.Chunk
	content         strings.Builder
	pending         strings.Builder
	streamCompleted bool
	finalized       bool
	partial         bool // LLM 超时/中断导致答案不完整
	safetyChanged   bool // 流式过程中输出审查替换过内容
	// deferred 高风险内容不流式先发：生成结果先累积，经生成后语义审核通过后一次性下发。
	deferred bool
}

// len 已产生（含未凑满一句的尾部）的答案长度，用于空流判断。
func (st *ragStreamState) len() int { return st.content.Len() + st.pending.Len() }

// sentenceBoundaries 流式输出审查的分句边界（句末标点与换行）。
// 不含半角句点：安全规则模式不含这些字符，整句入审不会漏检；反之在句中切分可能切断规则匹配。
const sentenceBoundaries = "。！？!?；;\n"

// lastSentenceBoundary 返回最后一个分句边界字符之后的字节位置（无边界时返回 -1）。
func lastSentenceBoundary(s string) int {
	idx := strings.LastIndexAny(s, sentenceBoundaries)
	if idx < 0 {
		return -1
	}
	_, size := utf8.DecodeRuneInString(s[idx:])
	return idx + size
}

// emitSafeSentences 将 pending 中已完整的句子逐句送输出审查后再推送（REQ-CHAT-012~014）。
// 关键：审查发生在内容推送给患者之前——违规语句永远不会以原文出现在客户端。
// 仅"替换/拦截"动作就地生效（blocked），"追加免责声明"留到流结束统一处理，避免每句重复追加。
func (s *ChatSendService) emitSafeSentences(
	ctx context.Context, out SSEWriter, st *ragStreamState,
) error {
	buffered := st.pending.String()
	idx := lastSentenceBoundary(buffered)
	if idx < 0 {
		return nil
	}
	return s.emitReviewed(ctx, out, st, buffered[:idx], buffered[idx:])
}

// flushPendingTail 流结束时审查并推送尾部不足一句的剩余内容。
func (s *ChatSendService) flushPendingTail(ctx context.Context, out SSEWriter, st *ragStreamState) error {
	tail := st.pending.String()
	st.pending.Reset()
	if tail == "" {
		return nil
	}
	return s.emitReviewed(ctx, out, st, tail, "")
}

// emitReviewed 审查 chunk 并推送；rest 为留在 pending 中待下一次审查的尾部。
// deferred（高风险内容）时只累积不推送：完整生成后统一语义审核，通过才随 finalizeRAGOutput 下发。
func (s *ChatSendService) emitReviewed(
	ctx context.Context, out SSEWriter, st *ragStreamState, chunk, rest string,
) error {
	if st.deferred {
		st.content.WriteString(chunk)
		st.pending.Reset()
		st.pending.WriteString(rest)
		return nil
	}
	emit := chunk
	if res := s.safetyOut.Validate(ctx, chunk); res.Blocked && res.Final != chunk {
		emit = res.Final
		st.safetyChanged = true
	}
	if err := out.Write("token", emit); err != nil {
		return err
	}
	st.content.WriteString(emit)
	st.pending.Reset()
	st.pending.WriteString(rest)
	return nil
}

// stageRAG 阶段 1（持久化用户消息 + assistant 占位）+ 阶段 2（检索 + 流式生成）+ 阶段 3（持久化 AI 消息）。
// 认证/匿名统一走此链：持久化全部委托 sess.store（认证=DB 会话，匿名=Redis 瞬态环），链条不感知身份。
// st 为本轮状态（turn_id / 消息 ID / 生成结果），由 Stream 创建并贯穿至权威结果事件。
// 编排各子阶段：prepareRAGContext（历史/改写/检索）→ streamLLMTokens（流式审查推送）→ finalizeRAGOutput（复核 + 落库），
// defer 委托 cleanupRAGStream 处理中断路径的孤儿占位消息清理。
func (s *ChatSendService) stageRAG(
	ctx context.Context, in StreamInput, sess *Session, out SSEWriter, st *ragStreamState, assessment rag.Assessment,
) error {
	// 阶段 1：用户消息 + assistant 占位在同一事务内落库（DB 实现含科室锁定；Redis 实现入环），
	// 两者共享本轮 turn_id。本轮自创建起即为 user + assistant 一对：检索/改写/生成中途失败时
	// 占位被 defer 清理或 finalizeRejection 写成终态，不留下孤立 user 消息。
	userMsg, aiMsgID, err := sess.store.SaveUserAndPlaceholder(ctx, TurnWrite{
		Content: in.Message, TurnID: st.turnID, DeptID: sess.DeptID,
	})
	if err != nil {
		return err
	}
	st.userMsgID, st.aiMsgID = userMsg.ID, aiMsgID
	defer func() { s.cleanupRAGStream(ctx, sess, st) }()

	// 阶段 2.1~2.4：历史加载/裁剪 + 检索（检索失败/空结果在内部降级为拒答）
	// 传入当前用户消息 ID：历史加载须排除它（已单独作为 UserMessage 传入 LLM，避免重复提问）。
	// 检索用统一审查给出的独立问题（assessment.StandaloneQuery）；生成用患者原话（originalQuery）。
	originalQuery, history, chunks, err := s.prepareRAGContext(ctx, in, sess, out, userMsg.ID, st, assessment)
	if err != nil {
		if errors.Is(err, errRejectionHandled) {
			return nil // finalizeRejection 已写终态并推送 result + done，无需继续
		}
		return err
	}
	st.chunks = chunks

	// 阶段 2.5：推送引用切片到前端（spec §3.1：data 为裸数组）
	if err := out.Write(EventReferences, chunks); err != nil {
		return err
	}

	// 阶段 2.7：流式生成。st 承载可变状态，defer 委托 cleanupRAGStream 处理中断路径。
	// streamCompleted 区分两种中断：
	//   - 流未完成（LLM 不可用 / chunk.Err / out.Write 失败）：清理为拒答，避免空 content 孤儿消息。
	//   - 流已完成但后续步骤因 ctx 取消失败：保留真实答案（经输出安全审查），不覆盖为拒答。
	// finalized 阻止 defer 重复清理——正常路径 finalize 成功后置 true。

	// 高风险内容（涉及用药/个体化）不做流式先发：st.deferred=true 时完整生成后在 finalizeRAGOutput
	// 统一做语义审核，通过才展示——流式内容一旦下发，事后修正无法消除先前影响。
	st.deferred = requiresFullReview(assessment, in.Message)

	slog.InfoContext(ctx, "chat: LLM stream started",
		"history_turns", len(history)/2, "chunks", len(chunks), "deferred", st.deferred)
	startTime := time.Now()
	if err := s.streamLLMTokens(ctx, s.buildSystemPrompt(ctx), originalQuery, history, chunks, out, st); err != nil {
		return err
	}

	// High 2: LLM 流正常结束但未产生任何 token（如 LLM 服务返回空 stream）。
	// 若不显式拦截，placeholder 会被 finalize 为空 content + ResultAnswered（违反 REQ-CHAT-003）。
	// 显式 finalize placeholder 为 RejectionMessage + REJECTED，并设置 finalized=true 阻止 defer 重复清理。
	if st.content.Len() == 0 {
		return s.handleEmptyStream(ctx, sess, st, out)
	}

	slog.InfoContext(ctx, "chat: LLM stream completed",
		"tokens", st.content.Len(), "duration_ms", time.Since(startTime).Milliseconds())

	// 阶段 2.8 + 阶段 3：输出侧安全审查 + 持久化 finalize AI 消息
	if err := s.finalizeRAGOutput(ctx, sess, st, out, in.Message); err != nil {
		return err
	}
	slog.InfoContext(ctx, "chat: request completed", "result_code", constants.ResultAnswered)
	return nil
}

// medicationIntentRe 患者提问中出现用药相关意图的信号（剂量/加量/停药等）。
// 用于判断本轮是否必须"完整生成 + 审核后展示"：这类问题的答案一旦越界（给出个体化剂量方案），
// 事后修正无法消除已展示内容的影响。
var medicationIntentRe = regexp.MustCompile(
	`(剂量|用量|加量|减量|停药|换药|加倍|翻倍|加服|少吃|多吃|mg|毫克|几片|吃几)`)

// requiresFullReview 判断本轮答案是否必须完整生成并审核通过后再展示。
func requiresFullReview(a rag.Assessment, message string) bool {
	if a.MedicationChange || a.IndividualizedDx {
		return true
	}
	return medicationIntentRe.MatchString(message)
}

// handleEmptyStream LLM 流正常结束但未产生任何 token：显式 finalize placeholder 为拒答。
// finalized=true 阻止 defer 重复清理（否则 cleanupRAGStream 会再次清理并产生误导日志）。
func (s *ChatSendService) handleEmptyStream(
	ctx context.Context, sess *Session, st *ragStreamState, out SSEWriter,
) error {
	slog.WarnContext(ctx, "llm stream returned empty content, degrading to rejection")
	st.streamCompleted = false
	systemErr := s.safetyIn.SystemErrorMessage()
	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), ragCleanupTimeout)
	if ferr := sess.store.FinalizeAssistant(
		cleanupCtx, st.aiMsgID, st.turnID, systemErr, constants.ResultRejected, nil,
	); ferr != nil {
		slog.ErrorContext(ctx, "finalize empty stream placeholder failed", "err", ferr)
	}
	cancelCleanup()
	st.finalized = true
	// 兜底话术即本轮答案（与落库一致），作为正文下发。
	if err := out.Write(EventAnswer, answerPayload{Mode: answerModeReplace, Text: systemErr}); err != nil {
		return err
	}
	if err := writeTurnResult(out, st, constants.ResultRejected, nil); err != nil {
		return err
	}
	return out.Write(EventDone, donePayload())
}

// prepareRAGContext 阶段 2.1~2.4：加载并裁剪历史、按统一审查给出的独立问题检索。
// currentUserMsgID 为当前轮用户消息 ID：阶段 1 已将其持久化，历史加载须排除，
// 否则 LLM 上下文出现两条连续 user 消息（原始问题 + 改写问题）。
// 检索问题来自 assessment.StandaloneQuery（审查阶段已保留否定/时间/人群等限定），
// 生成侧仍用患者原话（in.Message）——改写偏差不污染答案。
// 检索失败或无结果时降级为拒答（finalizeRejection），返回其错误供 stageRAG 直接透传。
func (s *ChatSendService) prepareRAGContext(
	ctx context.Context, in StreamInput, sess *Session,
	out SSEWriter, currentUserMsgID uuid.UUID, st *ragStreamState, assessment rag.Assessment,
) (originalQuery string, history []*entity.Message, chunks []rag.Chunk, err error) {
	// 阶段 2.1：历史消息（最近 N 轮，排除当前轮用户消息）
	history, err = sess.store.History(ctx, constants.HistoryTurns, &currentUserMsgID)
	if err != nil {
		return "", nil, nil, fmt.Errorf("load history: %w", err)
	}

	// 阶段 2.1b：生成阶段 Token 预算兜底（REQ-CHAT-006-A）。
	// 检索改写已移至统一审查（其内部分别裁剪历史），故此处按生成预算对历史做一次 FIFO 裁剪。
	// ponytail: 近似 token 估算见 estimateTokens；上限——中文重场景估算偏低，极端情况下突破实际 LLM 上下文窗口，
	// 由 LLM 服务端报 context_length_exceeded 错误兜底（已在 streamErr 路径处理为 CHAT_LLM_UNAVAILABLE）。
	beforeTrim := len(history)
	history = trimHistoryForTokens(history, in.Message, constants.TokenBudgetGenerate)
	if beforeTrim != len(history) {
		slog.InfoContext(ctx, "chat: history trimmed for generation",
			"turns_before", beforeTrim/2, "turns_after", len(history)/2)
	}

	// 阶段 2.2：检索问题 = 统一审查产出的独立问题（已保留原始限定，仅用于检索）。
	// Validate 已保证 retrieve 动作下非空；此处兜底为原话，防御异常输入。
	retrievalQuery := strings.TrimSpace(assessment.StandaloneQuery)
	if retrievalQuery == "" {
		retrievalQuery = in.Message
	}

	// 阶段 2.3：检索（跨域 wiki 域）。
	// TopK=0 让 RAGConfig.TopK 接管——修死 硬编码 DefaultTopK 使管理员配置 top_k 对 chat 失效。
	chunks, err = s.knowledge.SearchSimilarChunks(ctx, rag.SearchQuery{
		Query: retrievalQuery, DeptID: sess.DeptID, TopK: 0,
	})
	if err != nil {
		// 检索失败降级为拒答：阶段 1 用户消息已持久化，
		// 若直接返回 503 会留下无 assistant 回复的孤儿 user 消息，污染会话历史。
		// 用系统异常话术而非"知识库没有答案"——检索故障不等于没有资料（避免误导患者与医护）。
		slog.ErrorContext(ctx, "knowledge search failed, degrading to rejection", "err", err)
		return "", nil, nil, s.finalizeRejection(ctx, sess, st, out, s.safetyIn.SystemErrorMessage())
	}

	// 阶段 2.4：无检索结果拒答（REQ-CHAT-003）。
	// 0 命中分两种话术，用审查的 out_of_domain 分流：
	//   - 域外问题 → 说清能力边界并引导回业务范围（"知识库暂无内容"会让患者以为资料缺失）；
	//   - 业务内问题但缺资料 → 提示知识库暂无相关内容。
	// 注意"能不能答"始终由检索命中这一客观事实决定，模型的域外判定只决定措辞，
	// 因此模型误判域外也不会丢掉本院本可回答的内容（如院区地图/就医引导）。
	if len(chunks) == 0 {
		noHitMsg := s.safetyIn.NoKnowledgeMessage()
		if assessment.OutOfDomain {
			noHitMsg = rag.OutOfDomainMessage()
		}
		slog.WarnContext(ctx, "knowledge search returned 0 chunks, degrading to rejection",
			"query_len", len(retrievalQuery), "dept_id", sess.DeptID, "out_of_domain", assessment.OutOfDomain)
		return "", nil, nil, s.finalizeRejection(ctx, sess, st, out, noHitMsg)
	}

	slog.InfoContext(ctx, "chat: RAG search completed",
		"chunks", len(chunks), "query_len", len(retrievalQuery))
	// 生成侧返回用户原话（in.Message），检索改写仅用于召回；
	// 如此 LLM 忠实于患者原始表述，改写扩写偏差不再污染答案（references 仍指向改写检索结果）。
	return in.Message, history, chunks, nil
}

// streamLLMTokens 阶段 2.7：LLM 流式调用 + 分句审查推送 + 中断/超时检测。
// 每个 token 先进入 pending 缓冲，凑满一句经输出安全审查后立即推送并累积到 st.content（供 finalize 落库）；
// 流是否正常完成写入 st.streamCompleted，异常中断置 st.partial（答案不完整）。
// LLM 流式调用加 per-request deadline（R7-4 修复）：chat_pending_lock TTL 5min 覆盖最坏时长，
// 此处 4min 留 1min 余量给收尾事务 + SSE flush。无此 deadline 时，LLM 服务 stall（首字节后中途 hang）
// 会导致 goroutine + 连接 + Redis 锁泄漏。
func (s *ChatSendService) streamLLMTokens(
	ctx context.Context, systemPrompt, query string,
	history []*entity.Message, chunks []rag.Chunk, out SSEWriter, st *ragStreamState,
) error {
	streamCtx, cancelStream := context.WithTimeout(ctx, llmStreamTimeout)
	defer cancelStream()
	streamCh, streamErr := s.llm.StreamChat(streamCtx, llm.ChatRequest{
		SystemPrompt:  systemPrompt,
		History:       toLLMMessages(history),
		UserMessage:   query,
		ContextChunks: chunkContents(chunks),
	})
	if streamErr != nil {
		return apperrors.ServiceUnavailable("CHAT_LLM_UNAVAILABLE", "AI 服务暂不可用，请稍后重试")
	}
	for chunk := range streamCh {
		if chunk.Err != nil {
			slog.ErrorContext(ctx, "llm stream error", "err", chunk.Err)
			// 已有内容：答案是被中断的片段，落库须为 PARTIAL（不能因"发过内容"就当成本轮生成完成）。
			if st.len() > 0 {
				st.partial = true
				// 已产生但未凑满一句的尾部一并审查后推送，避免客户端看到的比服务端保留的少。
				if err := s.flushPendingTail(ctx, out, st); err != nil {
					return err
				}
			}
			return apperrors.ServiceUnavailable("CHAT_LLM_UNAVAILABLE", "AI 服务暂不可用，请稍后重试")
		}
		if chunk.Done {
			break
		}
		st.pending.WriteString(chunk.Token)
		if err := s.emitSafeSentences(ctx, out, st); err != nil {
			// 客户端断开：已推送内容视为不完整答案。
			st.partial = true
			return err
		}
	}
	// LLM 流正常结束（Done break 或 channel 关闭）：审查并推送尾部不足一句的内容。
	if err := s.flushPendingTail(ctx, out, st); err != nil {
		st.partial = true
		return err
	}
	// 后续终止原因检测委托 checkStreamTermination。
	st.streamCompleted = true
	return s.checkStreamTermination(ctx, streamCtx, out, st)
}

// checkStreamTermination 流退出后的终止原因检测：客户端断开（ctx 取消）与 LLM stall（streamCtx 超时）。
// 客户端断开时 ctx 被取消，LLM goroutine 关闭 channel 但不投递错误，for-range 正常退出，
// 此处提前返回避免对不完整内容做无效的输出审查和事务，同时标记 partial（答案未生成完）。
// R8-1: streamCtx 超时（LLM stall）检测——父 ctx 仍存活，故 ctx.Err() 无法捕获 streamCtx 超时；
// 空 content 标记 streamCompleted=false 让 defer 写拒答并返回 503，部分 content 推送截断提示后按 PARTIAL finalize。
func (s *ChatSendService) checkStreamTermination(
	ctx, streamCtx context.Context, out SSEWriter, st *ragStreamState,
) error {
	if err := ctx.Err(); err != nil {
		if st.content.Len() > 0 {
			st.partial = true
		}
		return fmt.Errorf("stream cancelled: %w", err)
	}
	if err := streamCtx.Err(); err != nil {
		if st.content.Len() == 0 {
			st.streamCompleted = false
			return apperrors.ServiceUnavailable("CHAT_LLM_TIMEOUT", "AI 服务响应超时，请稍后重试")
		}
		slog.WarnContext(ctx, "llm stream timed out with partial content",
			"tokens", st.content.Len(), "err", err)
		st.partial = true
		// 截断提示作为独立提示下发（不进正文）；"回答不完整"由 result_code=PARTIAL 持久表达。
		if err := out.Write(EventNotice, noticePayload{
			Kind: NoticeTimeout,
			Text: "（响应超时，以上为部分内容，完整回答请稍后重试）",
		}); err != nil {
			return err
		}
	}
	return nil
}

// finalizeRAGOutput 阶段 2.8（输出侧安全审查复核，REQ-CHAT-012~014）+ 阶段 3（持久化 finalize AI 消息）。
// 先按需执行生成后语义审核（高风险内容），再对最终内容做正则复核并补齐"追加免责声明"
// （追加动作留在流结束统一处理，避免每句重复追加）。
// 正文被修改时推送 answer_replaced，前端据此修正已累积的正文，使展示与持久化一致：
//   - mode=replace：越权内容已替换为安全话术，text 为完整安全话术。
//   - mode=append：追加免责声明，text 为追加部分。
//
// 最后推送 result（真实消息 ID + 最终 result_code + 最终引用）+ done。
func (s *ChatSendService) finalizeRAGOutput(
	ctx context.Context, sess *Session, st *ragStreamState, out SSEWriter, question string,
) error {
	content := st.content.String()

	// 阶段 2.8a：高风险内容的生成后语义审核（是否越过宣教边界 / 关键结论是否有资料支持）。
	// deferred 内容此前未下发，审核不通过即可整体替换，不会出现"已展示再修正"。
	if st.deferred {
		review, rerr := s.reviewAnswer(ctx, question, content, st.chunks)
		if rerr != nil {
			// 无法确认安全：不展示未经审核的答案（审查不可用 ≠ 安全）。
			slog.ErrorContext(ctx, "chat: deferred answer review unavailable, degrading", "err", rerr)
			return swallowRejection(s.finalizeRejection(ctx, sess, st, out, rag.AssessmentFailedMessage()))
		}
		if !review.Pass() {
			alt := strings.TrimSpace(review.RevisedAnswer)
			if alt == "" {
				alt = rag.RestrictedCareMessage()
			}
			slog.WarnContext(ctx, "chat: deferred answer rejected by semantic review",
				"boundary_ok", review.BoundaryOK, "evidence_supported", review.EvidenceSupported,
				"reason", review.Reason)
			return swallowRejection(s.finalizeRejection(ctx, sess, st, out, alt))
		}
		// 审核通过：完整答案一次性下发（此前刻意未流式发送）。
		if err := out.Write(EventAnswer, answerPayload{Mode: answerModeReplace, Text: content}); err != nil {
			return err
		}
	}

	out2 := s.safetyOut.Validate(ctx, content)
	final := out2.Final
	if out2.Changed {
		slog.InfoContext(ctx, "chat: output safety triggered",
			"blocked", out2.Blocked, "stream_replaced", st.safetyChanged,
			"orig_len", len(content), "final_len", len(final))
		mode := answerModeReplace
		text := final
		if !out2.Blocked {
			// 仅追加声明场景：截取追加部分
			mode = answerModeAppend
			if strings.HasPrefix(final, content) {
				text = final[len(content):]
			} else {
				// 防御：前缀不匹配（不应发生），降级为 replace 避免前端追加错误内容
				mode = answerModeReplace
				text = final
			}
		}
		if err := out.Write(EventAnswer, answerPayload{Mode: mode, Text: text}); err != nil {
			return err
		}
	} else {
		slog.InfoContext(ctx, "chat: output safety passed")
	}

	// 阶段 3：持久化 finalize AI 消息（DB=更新占位行；Redis=入环）
	resultCode := constants.ResultAnswered
	if out2.Blocked || st.safetyChanged {
		resultCode = constants.ResultIntercepted
	} else if st.partial {
		resultCode = constants.ResultPartial
	}
	if err := sess.store.FinalizeAssistant(
		ctx, st.aiMsgID, st.turnID, final, resultCode, toEntityRefs(st.chunks),
	); err != nil {
		return fmt.Errorf("finalize ai message: %w", err)
	}
	st.finalized = true // 阻止 defer 清理——已成功 finalize

	if err := writeTurnResult(out, st, resultCode, toEntityRefs(st.chunks)); err != nil {
		return err
	}
	// spec §3.1：done 事件 data 为字面量 [DONE]，标记流结束
	return out.Write(EventDone, donePayload())
}

// swallowRejection 吞掉 errRejectionHandled 哨兵：finalizeRejection 已完成 result + done 下发，
// 调用方（stageRAG）不应把它当错误继续上抛（否则 handler 会再写一个 error 事件）。
func swallowRejection(err error) error {
	if err == nil || errors.Is(err, errRejectionHandled) {
		return nil
	}
	return err
}

// reviewAnswer 生成后语义审核：未注入审核器时跳过（记录告警），保证未配置环境下普通宣教仍可用。
func (s *ChatSendService) reviewAnswer(
	ctx context.Context, question, answer string, chunks []rag.Chunk,
) (rag.OutputReview, error) {
	if s.outputReviewer == nil {
		slog.WarnContext(ctx, "chat: output reviewer not injected, skip semantic review")
		return rag.OutputReview{BoundaryOK: true, EvidenceSupported: true}, nil
	}
	return s.outputReviewer.ReviewAnswer(ctx, question, answer, chunkContents(chunks))
}

// cleanupRAGStream defer 清理：正常路径（finalized=true）直接返回；
// 否则用 context.Background() + 超时执行清理——请求 ctx 可能在客户端断开时已取消，
// 此时用原 ctx 清理会因 context.Canceled 而失败，留下孤儿消息。
// 落库内容取 st.content（已经输出审查、且已推送给客户端的内容），保证 DB 与客户端所见一致。
//   - streamCompleted=true：LLM 流已完成，用真实答案 finalize；partial 表示答案被截断。
//   - streamCompleted=false：LLM 流未完成，清理为拒答，避免遗留空 content 的孤儿消息。
//
// 持久化经 sess.store 收敛身份（DB=更新占位行；Redis=入环，最终一致多轮上下文）。
func (s *ChatSendService) cleanupRAGStream(ctx context.Context, sess *Session, st *ragStreamState) {
	if st.finalized {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), ragCleanupTimeout)
	defer cancel()
	content := st.content.String()
	if st.streamCompleted {
		out2 := s.safetyOut.Validate(cleanupCtx, content)
		resultCode := constants.ResultAnswered
		if out2.Blocked || st.safetyChanged {
			resultCode = constants.ResultIntercepted
		} else if st.partial {
			resultCode = constants.ResultPartial
		}
		if ferr := sess.store.FinalizeAssistant(
			cleanupCtx, st.aiMsgID, st.turnID, out2.Final, resultCode, toEntityRefs(st.chunks),
		); ferr != nil {
			slog.ErrorContext(ctx, "cleanup finalized stream failed", "err", ferr)
		}
		return
	}
	if content != "" {
		out2 := s.safetyOut.Validate(cleanupCtx, content)
		if out2.Blocked {
			// 兜底：逐句审查后仍有越权内容（如违规表述跨句边界），放弃该片段改存系统兜底话术。
			if ferr := sess.store.FinalizeAssistant(
				cleanupCtx, st.aiMsgID, st.turnID, s.safetyIn.SystemErrorMessage(), constants.ResultRejected, nil,
			); ferr != nil {
				slog.ErrorContext(ctx, "cleanup unsafe partial failed", "err", ferr)
			}
			return
		}
		if ferr := sess.store.FinalizeAssistant(
			cleanupCtx, st.aiMsgID, st.turnID, out2.Final, constants.ResultPartial, toEntityRefs(st.chunks),
		); ferr != nil {
			slog.ErrorContext(ctx, "cleanup partial content failed", "err", ferr)
		}
		return
	}
	if ferr := sess.store.FinalizeAssistant(
		cleanupCtx, st.aiMsgID, st.turnID, s.safetyIn.SystemErrorMessage(), constants.ResultRejected, nil,
	); ferr != nil {
		slog.ErrorContext(ctx, "cleanup orphan placeholder failed", "err", ferr)
	}
}

// errRejectionHandled 拒答已处理哨兵错误。finalizeRejection 推送 result + done 后返回此值，
// 调用方（prepareRAGContext -> stageRAG）据此停止后续 RAG 流程，避免重复推送事件。
var errRejectionHandled = errors.New("rejection already handled")

// finalizeRejection 把本轮 assistant 占位写成拒答终态并推送正文修正 + result + done。
// 复用占位（而非新增一条 assistant 消息）——保证"本轮"始终是一对 user + assistant，不产生孤立消息。
// msg 为具体拒答话术（无知识 / 系统异常等），作为本轮答案正文下发（与落库一致）。
func (s *ChatSendService) finalizeRejection(
	ctx context.Context, sess *Session, st *ragStreamState, out SSEWriter, msg string,
) error {
	if err := sess.store.FinalizeAssistant(ctx, st.aiMsgID, st.turnID, msg, constants.ResultRejected, nil); err != nil {
		return err
	}
	st.finalized = true
	if err := out.Write(EventAnswer, answerPayload{Mode: answerModeReplace, Text: msg}); err != nil {
		return err
	}
	if err := writeTurnResult(out, st, constants.ResultRejected, nil); err != nil {
		return err
	}
	if err := out.Write(EventDone, donePayload()); err != nil {
		return err
	}
	return errRejectionHandled
}

// defaultSystemPrompt 已移至 constants.DefaultSystemPrompt。

// buildSystemPrompt 构造 system prompt（含安全约束 + 参考资料标识）。
// promptProvider 非 nil 且返回非空 prompt 时使用配置版本；否则降级为 defaultSystemPrompt。
// ponytail: provider 出错或返回空时静默降级——config 域故障不应导致 chat 流程不可用，折中。
// 升级路径：若需区分"无配置"与"配置加载失败"，可让 provider 返回 sentinel error。
func (s *ChatSendService) buildSystemPrompt(ctx context.Context) string {
	if s.promptProvider != nil {
		if prompt, err := s.promptProvider.GetSystemPrompt(ctx); err == nil && prompt != "" {
			slog.InfoContext(ctx, "chat: system prompt source=db")
			return prompt
		}
	}
	slog.InfoContext(ctx, "chat: system prompt source=default")
	return constants.DefaultSystemPrompt
}

// estimateTokens 估算文本的 token 数。
// ponytail: 真实 tokenizer 需依赖 tiktoken-go（引入新依赖），折中；
// 当前按字符类别加权近似——中文 1.5 token/字符（实际均值 1-2）、英文 0.25 token/字符（4 字符≈1 token）、
// 数字与符号 0.5 token/字符、空格不计。中文场景比 utf8.RuneCountInString/2 更接近实际，避免超长上下文突破 LLM 窗口。
// 上限——仍是估算，极端文本（混合未登录词、罕见 Unicode）可能偏差；
// 升级路径：阶段 2 接入 tiktoken-go 后改为精确估算。
func estimateTokens(text string) int {
	var tokens float64
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			tokens += 1.5
		case unicode.IsLetter(r):
			tokens += 0.25
		case unicode.IsDigit(r):
			tokens += 0.5
		case unicode.IsSpace(r):
			// 空格不计
		default:
			tokens += 0.5
		}
	}
	return int(math.Ceil(tokens))
}

// estimateHistoryTokens 估算"当前查询 + 历史"总 token 数。
// 用于改写/生成阶段的 Token 预算兜底（REQ-CHAT-006-A）。
func estimateHistoryTokens(history []*entity.Message, currentQuery string) int {
	total := estimateTokens(currentQuery)
	for _, m := range history {
		total += estimateTokens(m.Content)
	}
	return total
}

// trimHistoryForTokens FIFO 丢弃最早历史轮次直到 token 数 ≤ budget 或 history 长度 < 2。
// 每轮 = user + assistant 两条消息，丢弃 history[:2] 即丢一轮（GetRecentHistory 已按旧→新排序）。
// ponytail: 不裁剪中间——保留时间连续性，避免历史断档误导 LLM；
// 上限——若单条消息已超 budget，循环会清空 history 至 <2（仅保留当前查询，LLM 上下文为空）。
func trimHistoryForTokens(history []*entity.Message, currentQuery string, budget int) []*entity.Message {
	for len(history) >= 2 && estimateHistoryTokens(history, currentQuery) > budget {
		history = history[2:]
	}
	return history
}

// truncateTitle 取前 N 个 rune 作为标题（REQ-CHAT-018）。
func truncateTitle(msg string) string {
	r := []rune(msg)
	if len(r) > entity.TitleMaxLen {
		r = r[:entity.TitleMaxLen]
	}
	return string(r)
}

// toLLMMessages 将 entity.Message 转换为 llm.Message。
func toLLMMessages(in []*entity.Message) []llm.Message {
	out := make([]llm.Message, 0, len(in))
	for _, m := range in {
		out = append(out, llm.Message{Role: m.Role, Content: m.Content})
	}
	return out
}

// chunkContents 提取切片内容用于 system prompt 上下文，过滤空/纯空白内容。
func chunkContents(chunks []rag.Chunk) []string {
	out := make([]string, 0, len(chunks))
	for _, c := range chunks {
		if strings.TrimSpace(c.Content) == "" {
			continue
		}
		out = append(out, c.Content)
	}
	return out
}

// toEntityRefs 将 rag.Chunk 转换为 entity.Reference（持久化到 messages.referenced_chunks）。
func toEntityRefs(chunks []rag.Chunk) []entity.Reference {
	out := make([]entity.Reference, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, entity.Reference{
			ChunkID:      c.ChunkID,
			ArticleID:    c.ArticleID,
			ArticleTitle: c.ArticleTitle,
			Content:      c.Content,
			Score:        c.Score,
		})
	}
	return out
}

// deptIDPtr 取检索使用的 deptID。用户显式选择"全部科室"（selectedDeptID=0）时返回 nil。
// 会话已锁定时优先用锁定值；未锁定时返回 nil（不限科室，检索全部可访问文章）。
func deptIDPtr(selectedDeptID *int64, conv *entity.Conversation) *int64 {
	// 用户显式选择"全部科室" → 不限科室
	if selectedDeptID != nil && *selectedDeptID == 0 {
		return nil
	}
	if conv.LockedDeptID != nil {
		return conv.LockedDeptID
	}
	return nil
}
