// Package service 实现 chat 域业务编排：RAG 流式问答、会话管理、危机事件处理。
// 事务边界在本层开启（postgres.TxManager.WithTx），Repository 接收 ctx 内的 tx。
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
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
}

// chatPendingLockTTL 会话并发锁 TTL：5 分钟覆盖单次 LLM 流式生成最坏时长。
const chatPendingLockTTL = 5 * time.Minute

// llmStreamTimeout LLM 流式调用硬 deadline：4 分钟，留 1 分钟余量给收尾事务 + SSE flush。
// 与 chatPendingLockTTL 对齐，覆盖 LLM 服务 stall（首字节后中途 hang）场景，避免 goroutine 泄漏。
const llmStreamTimeout = 4 * time.Minute

// ChatSendService RAG 核心服务，编排阶段 1（输入安全 + 用户消息持久化）、
// 阶段 2（检索 + 流式生成）、阶段 3（AI 消息持久化 + SSE 结束事件）。
type ChatSendService struct {
	dept             rag.DepartmentResolver
	safetyIn         rag.InputSafetyFilter
	safetyOut        rag.OutputSafetyFilter
	knowledge        rag.KnowledgeSearcher
	rewriter         llm.Rewriter
	fallbackRewriter llm.Rewriter
	llm              llm.Streamer
	promptProvider   rag.SystemPromptProvider // 可为 nil：降级为 defaultSystemPrompt
	conv             ConversationPort
	msg              MessagePort
	crisis           CrisisPort
	crisisNotifier   CrisisNotifier // 危机事件主动通知（入队 asynq 任务，落库站内通知给 DEPT_ADMIN）
	locker           LockProvider
	tx               TxRunner
	ring             ringStore                         // 匿名会话瞬态上下文环（Redis）；nil 时匿名退化为单轮（无历史）
	oodThreshold     func(ctx context.Context) float64 // 知识库外检测阈值（动态读取 DB 配置，热生效）
}

// CrisisNotifier 危机事件主动通知接口（入队 asynq 任务，由 worker 落库站内通知）。
type CrisisNotifier interface {
	NotifyCrisis(ctx context.Context, eventID int64) error
}

// NewChatSendService 构造 RAG 服务。
// 跨域依赖（dept/knowledge/safetyIn/safetyOut/promptProvider）通过 interface 注入；阶段 2 由对应域实现。
// promptProvider 可为 nil：降级为 defaultSystemPrompt（保持修复前行为，便于测试与渐进接入）。
func NewChatSendService(
	dept rag.DepartmentResolver,
	safetyIn rag.InputSafetyFilter,
	safetyOut rag.OutputSafetyFilter,
	knowledge rag.KnowledgeSearcher,
	rewriter llm.Rewriter,
	fallbackRewriter llm.Rewriter,
	llmStreamer llm.Streamer,
	conv ConversationPort,
	msg MessagePort,
	crisis CrisisPort,
	crisisNotifier CrisisNotifier,
	locker LockProvider,
	tx TxRunner,
	ring ringStore,
	promptProvider rag.SystemPromptProvider,
	oodThreshold func(ctx context.Context) float64,
) *ChatSendService {
	return &ChatSendService{
		dept: dept, safetyIn: safetyIn, safetyOut: safetyOut, knowledge: knowledge,
		rewriter: rewriter, fallbackRewriter: fallbackRewriter, llm: llmStreamer,
		conv: conv, msg: msg, crisis: crisis, crisisNotifier: crisisNotifier,
		locker: locker, tx: tx, ring: ring,
		promptProvider: promptProvider,
		oodThreshold:   oodThreshold,
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

	// (1)~(2) 会话准备（REQ-CHAT-019）：统一构建 Session（认证=DB 会话+科室锁定；匿名=Redis 瞬态会话，不限科室）。
	sess, err := s.buildSession(ctx, in)
	if err != nil {
		return err
	}

	// (3) 防并发锁（REQ-NFR-012）——key 随身份（认证=user+conv；匿名=device）。
	// 须在 conversation 事件之前获取：锁失败（并发生成）时 wroteAny=false，handler 回退 HTTP 409
	// （符合 Conflict 语义，客户端可据此重试）；若先发 conversation 事件，错误会降级为 SSE error 事件（HTTP 200）。
	lockKey := buildLockKey(in)
	unlock, err := s.locker.Lock(ctx, lockKey, chatPendingLockTTL)
	if err != nil {
		if errors.Is(err, redis.ErrLockNotAcquired) {
			return apperrors.Conflict("CHAT_CONCURRENT_STREAM", "会话正在生成中，请稍后重试")
		}
		return fmt.Errorf("acquire lock: %w", err)
	}
	// E2E EDGE-CONC-001 修复：unlock 失败不再静默吞错——锁泄漏会导致同用户后续请求
	// 持续 409 至 TTL（5min）过期，必须有日志可观测（根因定位依赖此告警）。
	defer func() {
		if uerr := unlock(); uerr != nil {
			slog.WarnContext(ctx, "chat: release pending lock failed, will expire by TTL",
				"lock_key", lockKey, "ttl", chatPendingLockTTL.String(), "err", uerr)
		}
	}()

	// (2.5) 回传会话 ID（升级/已有均下发，锁获取成功后首个 SSE 事件）：认证为会话 UUID，匿名为 device 派生 id。
	// 前端据此更新 URL 与后续请求的 conversation_id，匿名用户据此维持多轮上下文标识。
	if err := out.Write("conversation", map[string]string{"conversation_id": sess.ID()}); err != nil {
		return err
	}

	// (4) 紧急症状预提醒（REQ-CHAT-010，spec §3.1 safety_warning 事件）
	emergencyWarned, err := s.writeEmergencyWarning(ctx, in, out)
	if err != nil {
		return err
	}

	// (5) 规则层安全审查（零延迟，REQ-NFR-005/007）
	decision, crisis := s.safetyIn.CheckRules(ctx, in.Message)
	if decision == rag.DecisionBlock {
		if crisis != nil {
			return s.handleCrisis(ctx, in, sess, crisis, out)
		}
		return s.handleInjection(ctx, in, sess, out, constants.ResultRejected, emergencyWarned)
	}

	// (6) LLM 就绪性预检
	if !s.llm.IsReady() {
		return apperrors.ServiceUnavailable("CHAT_LLM_UNAVAILABLE", "AI 服务暂不可用，请稍后重试")
	}

	// (7) LLM 层深度审查（疑似复核，REQ-CHAT-007）
	if !s.safetyIn.LLMCheck(ctx, in.Message) {
		slog.InfoContext(ctx, "chat: input blocked by LLM safety check")
		return s.handleInjection(ctx, in, sess, out, constants.ResultIntercepted, emergencyWarned)
	}

	slog.InfoContext(ctx, "chat: input safety passed")
	return s.stageRAG(ctx, in, sess, out, emergencyWarned)
}

// buildSession 统一构建会话：认证用户承载 DB 会话（含科室锁定与持久化）；
// 匿名用户承载 Redis 瞬态会话（多轮上下文，TTL 自动过期，不限科室，危机不记录）。
// 链条自此只与 Session 交互，不区分身份。
func (s *ChatSendService) buildSession(ctx context.Context, in StreamInput) (*Session, error) {
	if in.Identity.Anon() {
		// 匿名：会话 id 由 device 稳定派生（多轮续传），科室不限。
		sid := deriveAnonSessionID(in.Identity.DeviceID)
		return &Session{SID: sid, DeptID: nil, store: newMemSessionStore(s.ring, sid)}, nil
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

// deriveAnonSessionID 由 device_id 稳定派生匿名会话 id（同设备多轮沿用同一标识）。
func deriveAnonSessionID(deviceID string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("health-nexus:anon:"+deviceID)).String()
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

// writeEmergencyWarning 紧急症状预提醒：命中时推送 safety_warning，返回是否已下发。
func (s *ChatSendService) writeEmergencyWarning(
	ctx context.Context, in StreamInput, out SSEWriter,
) (bool, error) {
	if hits := s.safetyIn.EmergencyCheck(ctx, in.Message); len(hits) > 0 {
		if err := out.Write("safety_warning", s.safetyIn.EmergencyMessage()); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

// buildLockKey 构造防并发锁 key。已认证用 user_id，匿名用 device_id。
func buildLockKey(in StreamInput) string {
	if !in.Identity.Anon() {
		cid := "new"
		if in.ConversationID != nil {
			cid = in.ConversationID.String()
		}
		return fmt.Sprintf("chat_pending:%d:%s", in.Identity.UserID, cid)
	}
	return fmt.Sprintf("chat_pending:anon:%s", in.Identity.DeviceID)
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
	conv, err := s.conv.GetByIDForPatient(ctx, *in.ConversationID, in.Identity.UserID)
	if err != nil {
		return nil, fmt.Errorf("load conversation: %w", err)
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

// handleCrisis 命中危机关键词：持久化危机（认证=落库危机事件并通知医护；匿名=空操作——不汇报不记录）
// 并下发危机热线。持久化失败不阻断 SSE——无论成败都推送 crisis 热线，确保患者收到救命信息（REQ-CHAT-008 / R7-1）。
// 紧急就医提醒已在 Stream 主流程提前下发，此处不再重复推送 safety_warning。
func (s *ChatSendService) handleCrisis(
	ctx context.Context, in StreamInput, sess *Session, c *rag.Crisis, out SSEWriter,
) error {
	// DB store 内部含一次性重试并记录告警；匿名 store 为空操作。不阻断热线下发。
	if err := sess.store.PersistCrisis(
		ctx, in.Identity.UserID, in.Message, c, s.safetyIn.CrisisResponse(),
	); err != nil {
		slog.ErrorContext(ctx, "chat crisis persist failed, still pushing hotline", "err", err)
	}

	// 无论事务是否成功，都推送 crisis 热线（心理援助话术，已含热线号码）。
	if err := out.Write("crisis", map[string]any{
		"answer": s.safetyIn.CrisisResponse(),
	}); err != nil {
		return err
	}

	// SSE 协议（spec §3.1）：crisis 事件已推送，done=[DONE] 终止流。
	if err := out.Write("done", "[DONE]"); err != nil {
		return err
	}
	slog.InfoContext(ctx, "chat: request completed", "result_code", constants.ResultCrisis)
	return nil
}

// handleInjection 命中 Prompt 注入（规则层）或 LLM 审查拒绝（LLM 层）：
// 保存用户消息 + assistant 拒答消息（认证=落库；匿名=Redis 环/退化为不持久化），
// 推送 safety_warning + done SSE。
// resultCode 区分：规则层用 ResultRejected，LLM 层用 ResultIntercepted（深度拦截）。
// emergencyWarned=true 时跳过 safety_warning SSE（紧急就医提醒已下发，避免双 warning）。
func (s *ChatSendService) handleInjection(
	ctx context.Context, in StreamInput, sess *Session, out SSEWriter, resultCode string, emergencyWarned bool,
) error {
	if _, err := sess.store.SaveUser(ctx, in.Message, sess.DeptID); err != nil {
		return err
	}
	if err := sess.store.SaveAssistant(ctx, s.safetyIn.RejectionMessage(), resultCode, nil); err != nil {
		return err
	}
	// 紧急提醒已下发时不再发拒答 warning--避免一次流中两条 safety_warning 造成前端困惑。
	if !emergencyWarned {
		if err := out.Write("safety_warning", s.safetyIn.RejectionMessage()); err != nil {
			return err
		}
	}
	if err := out.Write("done", "[DONE]"); err != nil {
		return err
	}
	slog.InfoContext(ctx, "chat: request completed", "result_code", resultCode)
	return nil
}

// ragCleanupTimeout defer 清理用独立超时--请求 ctx 可能已取消，故用 context.Background() + 5s。
const ragCleanupTimeout = 5 * time.Second

// ragStreamState 阶段 2.7 流式生成的可变状态，供 streamLLMTokens 累积、
// cleanupRAGStream 在 defer 中据 streamCompleted/finalized 决定清理路径。
type ragStreamState struct {
	aiMsgID         uuid.UUID
	chunks          []rag.Chunk
	full            strings.Builder
	streamCompleted bool
	finalized       bool
	partial         bool // LLM 超时导致答案不完整
}

// stageRAG 阶段 1（持久化用户消息）+ 阶段 2（检索 + 流式生成）+ 阶段 3（持久化 AI 消息）。
// 认证/匿名统一走此链：持久化全部委托 sess.store（认证=DB 会话，匿名=Redis 瞬态环），链条不感知身份。
// emergencyWarned 表示是否已下发紧急 safety_warning——为 true 时 finalizeRejection 跳过拒答 warning。
// 编排各子阶段：prepareRAGContext（历史/改写/检索）→ streamLLMTokens（流式累积）→ finalizeRAGOutput（输出审查 + 落库），
// defer 委托 cleanupRAGStream 处理中断路径的孤儿占位消息清理。
func (s *ChatSendService) stageRAG(
	ctx context.Context, in StreamInput, sess *Session,
	out SSEWriter, emergencyWarned bool,
) error {
	// 阶段 1：持久化用户消息 + 标题（DB 实现含科室锁定；Redis 实现入环），返回消息用于历史排除。
	userMsg, err := sess.store.SaveUser(ctx, in.Message, sess.DeptID)
	if err != nil {
		return err
	}

	// 阶段 2.1~2.4：历史加载/裁剪 + 查询改写 + 检索（检索失败/空结果在内部降级为拒答）
	// 传入当前用户消息 ID：历史加载须排除它（已单独作为 UserMessage 传入 LLM，避免重复提问）。
	// 改写结果仅用于检索，生成用用户原话（originalQuery）。
	originalQuery, history, chunks, err := s.prepareRAGContext(ctx, in, sess, out, emergencyWarned, userMsg.ID)
	if err != nil {
		if errors.Is(err, errRejectionHandled) {
			return nil // finalizeRejection 已推送 safety_warning + done，无需继续
		}
		return err
	}

	// 阶段 2.5：推送引用切片到前端（spec §3.1：data 为裸数组）
	if err := out.Write("references", chunks); err != nil {
		return err
	}

	// 阶段 2.6：保存 AI 占位消息
	aiMsg, err := sess.store.SaveAssistantPlaceholder(ctx)
	if err != nil {
		return fmt.Errorf("save ai placeholder: %w", err)
	}

	// 阶段 2.7：流式生成。st 承载可变状态，defer 委托 cleanupRAGStream 处理中断路径。
	// streamCompleted 区分两种中断：
	//   - 流未完成（LLM 不可用 / chunk.Err / out.Write 失败）：清理为拒答，避免空 content 孤儿消息。
	//   - 流已完成但后续步骤因 ctx 取消失败：保留真实答案（经输出安全审查），不覆盖为拒答。
	// finalized 阻止 defer 重复清理——正常路径 finalize 成功后置 true。
	st := &ragStreamState{aiMsgID: aiMsg.ID, chunks: chunks}
	defer func() { s.cleanupRAGStream(ctx, sess, st) }()

	// 生成 Token 预算：改写阶段已按 TokenBudgetRewrite(4000) 裁过历史，TokenBudgetGenerate(16000)
	// 更宽松，此后无需二次裁剪（原 trimHistoryForGeneration 恒为 no-op，已移除）。

	slog.InfoContext(ctx, "chat: LLM stream started",
		"history_turns", len(history)/2, "chunks", len(chunks))
	startTime := time.Now()
	if err := s.streamLLMTokens(ctx, s.buildSystemPrompt(ctx), originalQuery, history, chunks, out, st); err != nil {
		return err
	}

	// High 2: LLM 流正常结束但未产生任何 token（如 LLM 服务返回空 stream）。
	// 若不显式拦截，placeholder 会被 finalize 为空 content + ResultAnswered（违反 REQ-CHAT-003）。
	// 显式 finalize placeholder 为 RejectionMessage + REJECTED，并设置 finalized=true 阻止 defer 重复清理。
	if st.full.Len() == 0 {
		return s.handleEmptyStream(ctx, sess, st, out, emergencyWarned)
	}

	slog.InfoContext(ctx, "chat: LLM stream completed",
		"tokens", st.full.Len(), "duration_ms", time.Since(startTime).Milliseconds())

	// 阶段 2.8 + 阶段 3：输出侧安全审查 + 持久化 finalize AI 消息
	if err := s.finalizeRAGOutput(ctx, sess, st, out); err != nil {
		return err
	}
	slog.InfoContext(ctx, "chat: request completed", "result_code", constants.ResultAnswered)
	return nil
}

// handleEmptyStream LLM 流正常结束但未产生任何 token：显式 finalize placeholder 为拒答。
// finalized=true 阻止 defer 重复清理（否则 cleanupRAGStream 会再次清理并产生误导日志）。
func (s *ChatSendService) handleEmptyStream(
	ctx context.Context, sess *Session, st *ragStreamState, out SSEWriter, emergencyWarned bool,
) error {
	slog.WarnContext(ctx, "llm stream returned empty content, degrading to rejection")
	st.streamCompleted = false
	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), ragCleanupTimeout)
	if ferr := sess.store.FinalizeAssistant(
		cleanupCtx, st.aiMsgID, s.safetyIn.SystemErrorMessage(), constants.ResultRejected, nil,
	); ferr != nil {
		slog.ErrorContext(ctx, "finalize empty stream placeholder failed", "err", ferr)
	}
	cancelCleanup()
	st.finalized = true
	if !emergencyWarned {
		if err := out.Write("safety_warning", s.safetyIn.SystemErrorMessage()); err != nil {
			return err
		}
	}
	return out.Write("done", "[DONE]")
}

// rewriteQuery 查询改写（三级降级：专用改写 → 主 LLM 兜底 → 原始查询）。
// 即使无历史也执行——短问/口语问需要扩写以提升检索质量（如"怎么控制"→"高血压日常护理方法"）。
func (s *ChatSendService) rewriteQuery(ctx context.Context, msg string, history []llm.Message) string {
	query := msg
	if rewritten, rerr := s.rewriter.ToStandaloneQuestion(ctx, msg, history); rerr != nil {
		slog.WarnContext(ctx, "primary rewrite failed, trying LLM fallback", "err", rerr)
		if s.fallbackRewriter != nil {
			if rewritten2, rerr2 := s.fallbackRewriter.ToStandaloneQuestion(ctx, msg, history); rerr2 != nil {
				slog.WarnContext(ctx, "LLM rewrite fallback failed, using original query", "err", rerr2)
			} else {
				query = rewritten2
			}
		}
	} else {
		query = rewritten
	}
	if query != msg {
		slog.InfoContext(ctx, "chat: query rewritten", "orig_len", len(msg), "rewritten_len", len(query))
	}
	return query
}

// prepareRAGContext 阶段 2.1~2.4：加载并裁剪历史、查询改写（失败降级原始查询）、知识库检索。
// currentUserMsgID 为当前轮用户消息 ID：阶段 1 已将其持久化，历史加载须排除，
// 否则 LLM 上下文出现两条连续 user 消息（原始问题 + 改写问题）。
// 改写仅用于检索（函数内部），返回 originalQuery（用户原话，用于生成）、
// 裁剪后的历史（不含当前用户消息）、检索到的 chunks。
// 检索失败或无结果时降级为拒答（finalizeRejection），返回其错误供 stageRAG 直接透传。
func (s *ChatSendService) prepareRAGContext(
	ctx context.Context, in StreamInput, sess *Session,
	out SSEWriter, emergencyWarned bool, currentUserMsgID uuid.UUID,
) (originalQuery string, history []*entity.Message, chunks []rag.Chunk, err error) {
	// 阶段 2.1：历史消息（最近 N 轮，排除当前轮用户消息）
	history, err = sess.store.History(ctx, constants.HistoryTurns, &currentUserMsgID)
	if err != nil {
		return "", nil, nil, fmt.Errorf("load history: %w", err)
	}

	// 阶段 2.1b：改写阶段 Token 预算兜底（REQ-CHAT-006-A）
	// 改写器输入 = 历史消息 + 当前查询；超 TokenBudgetRewrite 时 FIFO 丢弃最早历史轮次。
	// ponytail: 近似 token 估算见 estimateTokens；上限——中文重场景估算偏低，极端情况下突破实际 LLM 上下文窗口，
	// 由 LLM 服务端报 context_length_exceeded 错误兜底（已在 streamErr 路径处理为 CHAT_LLM_UNAVAILABLE）。
	beforeTrim := len(history)
	history = trimHistoryForTokens(history, in.Message, constants.TokenBudgetRewrite)
	if beforeTrim != len(history) {
		slog.InfoContext(ctx, "chat: history trimmed for rewrite",
			"turns_before", beforeTrim/2, "turns_after", len(history)/2)
	}

	// 阶段 2.2：查询改写（三级降级：专用改写 → 主 LLM 兜底 → 原始查询，REQ-NFR-017）。
	// **无论首问/多轮都改写**：短问/口语问需扩写提升检索召回
	// （如"怎么控制"→"高血压的日常护理方法"）。改写结果仅用于检索；生成侧仍用用户原话。
	llmHistory := toLLMMessages(history)
	rewrittenQuery := s.rewriteQuery(ctx, in.Message, llmHistory)

	// 阶段 2.3：检索（跨域 wiki 域，阶段 2 实现；阶段 1 此处可能返回 ErrNotImplemented）。
	// TopK=0 让 RAGConfig.TopK 接管——修死 硬编码 DefaultTopK 使管理员配置 top_k 对 chat 失效。
	chunks, err = s.knowledge.SearchSimilarChunks(ctx, rag.SearchQuery{
		Query: rewrittenQuery, DeptID: sess.DeptID, TopK: 0,
	})
	if err != nil {
		// 检索失败降级为拒答：阶段 1 用户消息已在阶段 1 持久化，
		// 若直接返回 503 会留下无 assistant 回复的孤儿 user 消息，污染会话历史。
		// 降级路径写入 assistant 拒答消息保证会话完整性，并记录原始错误供排查。
		slog.ErrorContext(ctx, "knowledge search failed, degrading to rejection", "err", err)
		return "", nil, nil, s.finalizeRejection(ctx, sess, out, emergencyWarned, s.safetyIn.NoKnowledgeMessage())
	}

	// 阶段 2.4：无检索结果拒答（REQ-CHAT-003）
	if len(chunks) == 0 {
		slog.WarnContext(ctx, "knowledge search returned 0 chunks, degrading to rejection",
			"query_len", len(rewrittenQuery), "dept_id", sess.DeptID)
		return "", nil, nil, s.finalizeRejection(ctx, sess, out, emergencyWarned, s.safetyIn.NoKnowledgeMessage())
	}

	// 阶段 2.4b：OOD 检测 - 所有切片向量相似度都很低时拒答（医疗场景严肃化）
	// oodThreshold 动态读取 DB rag_configs.ood_threshold，热生效。
	// 当 similarity_threshold=0（管理员要求不过滤）时，VecScore=0.445 等相关切片仍可通过 OOD。
	threshold := s.oodThreshold(ctx)
	if isOutOfDomain(chunks, threshold) {
		slog.WarnContext(ctx, "chat: OOD detected, all chunks below threshold",
			"chunk_count", len(chunks), "ood_threshold", threshold)
		return "", nil, nil, s.finalizeRejection(ctx, sess, out, emergencyWarned, s.safetyIn.NoKnowledgeMessage())
	}

	slog.InfoContext(ctx, "chat: RAG search completed",
		"chunks", len(chunks), "query_len", len(rewrittenQuery))
	// 生成侧返回用户原话（in.Message），改写仅用于检索；
	// 如此 LLM 忠实于患者原始表述，改写器扩写偏差不再污染答案，且可被精确否决（references 仍指向改写检索结果）。
	return in.Message, history, chunks, nil
}

// streamLLMTokens 阶段 2.7：LLM 流式调用 + token 累积 + 中断/超时检测。
// 累积结果写入 st.full；流是否正常完成写入 st.streamCompleted（供 defer 清理路径判断）。
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
			// 已流式输出部分 token：用户已经看到答案片段，标记为已完成避免 DB 覆盖为拒答。
			if st.full.Len() > 0 {
				st.streamCompleted = true
			}
			return apperrors.ServiceUnavailable("CHAT_LLM_UNAVAILABLE", "AI 服务暂不可用，请稍后重试")
		}
		if chunk.Done {
			break
		}
		// spec §3.1：token 事件 data 为裸字符串
		// 先 out.Write 成功再累积到 full——确保 full 仅含客户端已收到的 token，
		// 这样中断路径的 defer 用 full.String() finalize 与客户端实际所见一致。
		if err := out.Write("token", chunk.Token); err != nil {
			// 客户端断开：已发送的 token 视为已完成，避免 defer 覆盖为拒答造成 DB/客户端不一致。
			if st.full.Len() > 0 {
				st.streamCompleted = true
			}
			return err
		}
		st.full.WriteString(chunk.Token)
	}
	// LLM 流正常结束（Done break 或 channel 关闭）。后续终止原因检测委托 checkStreamTermination。
	st.streamCompleted = true
	return s.checkStreamTermination(ctx, streamCtx, out, st)
}

// checkStreamTermination 流退出后的终止原因检测：客户端断开（ctx 取消）与 LLM stall（streamCtx 超时）。
// 客户端断开时 ctx 被取消，LLM goroutine 关闭 channel 但不投递错误，for-range 正常退出，
// 此处提前返回避免对不完整内容做无效的输出审查和事务。
// R8-1: streamCtx 超时（LLM stall）检测——父 ctx 仍存活，故 ctx.Err() 无法捕获 streamCtx 超时；
// 空 content 标记 streamCompleted=false 让 defer 写拒答并返回 503，部分 content 推送截断提示后按 Answered finalize。
func (s *ChatSendService) checkStreamTermination(
	ctx, streamCtx context.Context, out SSEWriter, st *ragStreamState,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("stream cancelled: %w", err)
	}
	if err := streamCtx.Err(); err != nil {
		if st.full.Len() == 0 {
			st.streamCompleted = false
			return apperrors.ServiceUnavailable("CHAT_LLM_TIMEOUT", "AI 服务响应超时，请稍后重试")
		}
		slog.WarnContext(ctx, "llm stream timed out with partial content",
			"tokens", st.full.Len(), "err", err)
		st.partial = true
		// 推送截断提示让患者知晓答案不完整（避免误以为已收尾）。
		if err := out.Write("safety_warning", "（响应超时，以上为部分内容，完整回答请稍后重试）"); err != nil {
			return err
		}
	}
	return nil
}

// finalizeRAGOutput 阶段 2.8（输出侧安全审查，REQ-CHAT-012~014）+ 阶段 3（持久化 finalize AI 消息）。
// 输出审查触发时推送 safety_warning（spec §3.1），data 为 JSON 对象 {"mode","text"}：
//   - mode=replace：越权内容已替换为安全话术，text 为完整安全话术，前端据此覆盖已累积的 token 缓冲。
//   - mode=append：追加免责声明，text 为追加部分，前端追加到累积答案末尾。
//
// 这样 UI 最终内容与 DB 持久化内容保持一致（修复前 UI 保留被拦截的原始内容）。
func (s *ChatSendService) finalizeRAGOutput(
	ctx context.Context, sess *Session, st *ragStreamState, out SSEWriter,
) error {
	out2 := s.safetyOut.Validate(ctx, st.full.String())
	final := out2.Final
	if out2.Changed {
		slog.InfoContext(ctx, "chat: output safety triggered",
			"blocked", out2.Blocked, "orig_len", st.full.Len(), "final_len", len(final))
		mode := "replace"
		warning := final
		if !out2.Blocked {
			// 仅追加声明场景：截取追加部分
			mode = "append"
			if strings.HasPrefix(final, st.full.String()) {
				warning = final[len(st.full.String()):]
			} else {
				// 防御：前缀不匹配（不应发生），降级为 replace 避免前端追加错误内容
				mode = "replace"
				warning = final
			}
		}
		if err := out.Write("safety_warning", map[string]string{"mode": mode, "text": warning}); err != nil {
			return err
		}
	} else {
		slog.InfoContext(ctx, "chat: output safety passed")
	}

	// 阶段 3：持久化 finalize AI 消息（DB=更新占位行；Redis=入环）
	resultCode := constants.ResultAnswered
	if out2.Blocked {
		resultCode = constants.ResultIntercepted
	} else if st.partial {
		resultCode = constants.ResultPartial
	}
	if err := sess.store.FinalizeAssistant(ctx, st.aiMsgID, final, resultCode, toEntityRefs(st.chunks)); err != nil {
		return fmt.Errorf("finalize ai message: %w", err)
	}
	st.finalized = true // 阻止 defer 清理——已成功 finalize

	// spec §3.1：done 事件 data 为字面量 [DONE]，标记流结束
	return out.Write("done", "[DONE]")
}

// cleanupRAGStream defer 清理：正常路径（finalized=true）直接返回；
// 否则用 context.Background() + 超时执行清理——请求 ctx 可能在客户端断开时已取消，
// 此时用原 ctx 清理会因 context.Canceled 而失败，留下孤儿消息。
//   - streamCompleted=true：LLM 流已完成，对完整内容做输出审查后用真实答案 finalize，避免覆盖已生成内容。
//   - streamCompleted=false：LLM 流未完成，清理为拒答，避免遗留空 content 的孤儿消息。
//
// 持久化经 sess.store 收敛身份（DB=更新占位行；Redis=入环，最终一致多轮上下文）。
func (s *ChatSendService) cleanupRAGStream(ctx context.Context, sess *Session, st *ragStreamState) {
	if st.finalized {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), ragCleanupTimeout)
	defer cancel()
	if st.streamCompleted {
		out2 := s.safetyOut.Validate(cleanupCtx, st.full.String())
		resultCode := constants.ResultAnswered
		if out2.Blocked {
			resultCode = constants.ResultIntercepted
		} else if st.partial {
			resultCode = constants.ResultPartial
		}
		if ferr := sess.store.FinalizeAssistant(
			cleanupCtx, st.aiMsgID, out2.Final, resultCode, toEntityRefs(st.chunks),
		); ferr != nil {
			slog.ErrorContext(ctx, "cleanup finalized stream failed", "err", ferr)
		}
		return
	}
	if st.full.Len() > 0 {
		out2 := s.safetyOut.Validate(cleanupCtx, st.full.String())
		if out2.Blocked {
			if ferr := sess.store.FinalizeAssistant(
				cleanupCtx, st.aiMsgID, s.safetyIn.SystemErrorMessage(), constants.ResultRejected, nil,
			); ferr != nil {
				slog.ErrorContext(ctx, "cleanup unsafe partial failed", "err", ferr)
			}
			return
		}
		if ferr := sess.store.FinalizeAssistant(
			cleanupCtx, st.aiMsgID, out2.Final, constants.ResultPartial, toEntityRefs(st.chunks),
		); ferr != nil {
			slog.ErrorContext(ctx, "cleanup partial content failed", "err", ferr)
		}
		return
	}
	if ferr := sess.store.FinalizeAssistant(
		cleanupCtx, st.aiMsgID, s.safetyIn.SystemErrorMessage(), constants.ResultRejected, nil,
	); ferr != nil {
		slog.ErrorContext(ctx, "cleanup orphan placeholder failed", "err", ferr)
	}
}

// errRejectionHandled 拒答已处理哨兵错误。finalizeRejection 推送 safety_warning + done 后返回此值，
// 调用方（prepareRAGContext -> stageRAG）据此停止后续 RAG 流程，避免重复推送事件。
var errRejectionHandled = errors.New("rejection already handled")

// finalizeRejection 持久化 assistant 拒答消息并推送 safety_warning + done。
// msg 为具体拒答话术（无知识 / 系统异常等），emergencyWarned=true 时跳过 SSE warning。
func (s *ChatSendService) finalizeRejection(
	ctx context.Context, sess *Session, out SSEWriter, emergencyWarned bool, msg string,
) error {
	if err := sess.store.SaveAssistant(ctx, msg, constants.ResultRejected, nil); err != nil {
		return err
	}
	// 紧急提醒已下发时不再发拒答 warning——避免一次流中两条 safety_warning 造成前端困惑。
	if !emergencyWarned {
		if err := out.Write("safety_warning", msg); err != nil {
			return err
		}
	}
	if err := out.Write("done", "[DONE]"); err != nil {
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

// isOutOfDomain 判断所有检索切片的向量相似度是否都低于 OOD 阈值。
// 医疗场景：语义不相关时拒答，而非让 AI 硬凑。
func isOutOfDomain(chunks []rag.Chunk, threshold float64) bool {
	if len(chunks) == 0 {
		return true
	}
	maxVecScore := 0.0
	for _, c := range chunks {
		if c.VecScore > maxVecScore {
			maxVecScore = c.VecScore
		}
	}
	return maxVecScore < threshold
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
