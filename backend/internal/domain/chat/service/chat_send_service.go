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
	"health-nexus/internal/shared/rag"
)

// SSEWriter SSE 事件写入接口，由 handler 层实现（消费者定义在 service 端）。
// 每个 token / 引用 / 危机事件通过 Write 推送给客户端并立即 flush。
type SSEWriter interface {
	Write(event string, data any) error
}

// StreamInput 流式问答输入。
type StreamInput struct {
	UserID         int64
	DeviceID       string     // 匿名用户设备标识（UserID==0 时有效）
	ConversationID *uuid.UUID // nil = 新建会话
	SelectedDeptID *int64     // nil = 不限定；会话已锁定时必须与锁定值一致
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
	promptProvider rag.SystemPromptProvider,
	oodThreshold func(ctx context.Context) float64,
) *ChatSendService {
	return &ChatSendService{
		dept: dept, safetyIn: safetyIn, safetyOut: safetyOut, knowledge: knowledge,
		rewriter: rewriter, fallbackRewriter: fallbackRewriter, llm: llmStreamer,
		conv: conv, msg: msg, crisis: crisis, crisisNotifier: crisisNotifier,
		locker: locker, tx: tx,
		promptProvider: promptProvider,
		oodThreshold:   oodThreshold,
	}
}

// Stream 执行 RAG 三阶段流式问答。
// 匿名用户（UserID==0）跳过会话持久化，仅做安全审查 + RAG 流式生成。
// 错误统一返回 AppError 由 handler 写 SSE error 事件或 HTTP 错误响应。
func (s *ChatSendService) Stream(ctx context.Context, in StreamInput, out SSEWriter) error {
	// 输入校验
	if err := validateStreamInput(in); err != nil {
		return err
	}

	isAnonymous := in.UserID == 0

	// (1)~(2) 科室范围校验 + 会话准备（REQ-CHAT-019）——匿名用户跳过（无科室偏好，检索时不限科室）
	var dept rag.Department
	var conv *entity.Conversation
	if !isAnonymous {
		var err error
		if dept, conv, err = s.resolveDeptAndConversation(ctx, in); err != nil {
			return err
		}
	}

	// (3) 防并发锁（REQ-NFR-012）——匿名用户用 device_id 标识。
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

	// (2.5) 回传会话 ID（新建/已有均下发，锁获取成功后首个 SSE 事件）：前端据此更新 URL 与后续请求的 conversation_id。
	// 缺失此事件时，新会话每条消息都会隐式创建独立会话，多轮上下文丢失、会话列表被单条消息会话污染。
	if !isAnonymous {
		if err := out.Write("conversation", map[string]string{"conversation_id": conv.ID.String()}); err != nil {
			return err
		}
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
			return s.handleCrisis(ctx, in, conv, dept, crisis, out)
		}
		return s.handleInjection(ctx, in, conv, dept, out, constants.ResultRejected, emergencyWarned)
	}

	// (6) LLM 就绪性预检
	if !s.llm.IsReady() {
		return apperrors.ServiceUnavailable("CHAT_LLM_UNAVAILABLE", "AI 服务暂不可用，请稍后重试")
	}

	// (7) LLM 层深度审查（疑似复核，REQ-CHAT-007）
	if !s.safetyIn.LLMCheck(ctx, in.Message) {
		slog.InfoContext(ctx, "chat: input blocked by LLM safety check")
		return s.handleInjection(ctx, in, conv, dept, out, constants.ResultIntercepted, emergencyWarned)
	}

	slog.InfoContext(ctx, "chat: input safety passed")
	if isAnonymous {
		return s.stageRAGAnonymous(ctx, in, out, emergencyWarned)
	}
	return s.stageRAG(ctx, in, conv, dept, out, emergencyWarned)
}

// resolveDeptAndConversation 非匿名用户的科室范围校验 + 会话加载/创建。
func (s *ChatSendService) resolveDeptAndConversation(
	ctx context.Context, in StreamInput,
) (rag.Department, *entity.Conversation, error) {
	dept, err := s.dept.ResolveForPatient(ctx, in.UserID, in.SelectedDeptID)
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
	if in.UserID > 0 {
		cid := "new"
		if in.ConversationID != nil {
			cid = in.ConversationID.String()
		}
		return fmt.Sprintf("chat_pending:%d:%s", in.UserID, cid)
	}
	return fmt.Sprintf("chat_pending:anon:%s", in.DeviceID)
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
// ponytail: 代价——若后续 ensureConversationAndUserMessage 失败会留下空会话，折中；
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
			c, err := s.conv.Create(ctx, in.UserID, lockedDeptID)
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
	conv, err := s.conv.GetByIDForPatient(ctx, *in.ConversationID, in.UserID)
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

// handleCrisis 命中危机关键词：先事务内持久化 user msg + crisis event + assistant msg（非匿名），
// tx 失败时不阻断 SSE 流——仍推送 crisis 事件（心理援助热线）+ done，确保患者收到救命信息（REQ-CHAT-008 / R7-1）。
// 紧急就医提醒已在 Stream 主流程提前下发，此处不再重复推送 safety_warning。
// 匿名用户（conv==nil）跳过持久化，仅下发 crisis 热线。
func (s *ChatSendService) handleCrisis(
	ctx context.Context, in StreamInput, conv *entity.Conversation,
	dept rag.Department, c *rag.Crisis, out SSEWriter,
) error {

	if conv != nil {
		var crisisEventID int64
		err := s.tx.WithTx(ctx, func(ctx context.Context) error {
			_, _, ceID, err := s.persistUserMessageAndCrisis(ctx, in, conv, dept, c)
			if err != nil {
				return err
			}
			crisisEventID = ceID
			if _, err := s.msg.SaveAssistant(
				ctx, conv.ID, s.safetyIn.CrisisResponse(), constants.ResultCrisis, nil,
			); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			slog.ErrorContext(ctx, "chat crisis persistence failed, still pushing hotline",
				"patient_id", in.UserID, "conversation_id", conv.ID,
				"keywords", c.Keywords, "err", err)
		} else {
			slog.InfoContext(ctx, "chat crisis event created",
				"event_id", crisisEventID, "patient_id", in.UserID, "conversation_id", conv.ID, "keywords", c.Keywords)
			// 主动通知：入队 asynq 任务，worker 落库站内通知给 DEPT_ADMIN（fire-and-forget，不阻断 SSE 流）
			if err := s.crisisNotifier.NotifyCrisis(ctx, crisisEventID); err != nil {
				slog.ErrorContext(ctx, "chat crisis notify enqueue failed", "event_id", crisisEventID, "err", err)
			}
		}
	}

	// 无论 tx 是否成功，都推送 crisis 热线（心理援助话术，已含热线号码）。
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
// 非匿名时事务内创建会话 + 用户消息 + assistant 拒答消息，推送 safety_warning + done SSE。
// 匿名用户跳过持久化，直接推送 safety_warning + done。
// resultCode 区分：规则层用 ResultRejected，LLM 层用 ResultIntercepted（深度拦截）。
// emergencyWarned=true 时跳过 safety_warning SSE（紧急就医提醒已下发，避免双 warning）。
func (s *ChatSendService) handleInjection(
	ctx context.Context, in StreamInput, conv *entity.Conversation,
	dept rag.Department, out SSEWriter, resultCode string, emergencyWarned bool,
) error {
	if conv != nil {
		err := s.tx.WithTx(ctx, func(ctx context.Context) error {
			_, _, err := s.ensureConversationAndUserMessage(ctx, in, conv, dept)
			if err != nil {
				return err
			}
			if _, err = s.msg.SaveAssistant(ctx, conv.ID, s.safetyIn.RejectionMessage(), resultCode, nil); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			return err
		}
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
// emergencyWarned 表示是否已下发紧急 safety_warning——为 true 时 finalizeRejection 跳过拒答 warning。
// 编排各子阶段：prepareRAGContext（历史/改写/检索）→ streamLLMTokens（流式累积）→ finalizeRAGOutput（输出审查 + 落库），
// defer 委托 cleanupRAGStream 处理中断路径的孤儿占位消息清理。
func (s *ChatSendService) stageRAG(
	ctx context.Context, in StreamInput, conv *entity.Conversation,
	dept rag.Department, out SSEWriter, emergencyWarned bool,
) error {
	// 阶段 1：事务内确保会话 + 用户消息 + 标题
	var userMsg *entity.Message
	err := s.tx.WithTx(ctx, func(ctx context.Context) error {
		var txErr error
		_, userMsg, txErr = s.ensureConversationAndUserMessage(ctx, in, conv, dept)
		return txErr
	})
	if err != nil {
		return err
	}

	// 阶段 2.1~2.4：历史加载/裁剪 + 查询改写 + 检索（检索失败/空结果在内部降级为拒答）
	// 传入当前用户消息 ID：历史加载须排除它（已单独作为 UserMessage 传入 LLM，避免重复提问）。
	// 改写结果仅用于检索，生成用用户原话（originalQuery）。
	_, originalQuery, history, chunks, err := s.prepareRAGContext(ctx, in, conv, out, emergencyWarned, userMsg.ID)
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
	aiMsg, err := s.msg.SaveAssistantPlaceholder(ctx, conv.ID)
	if err != nil {
		return fmt.Errorf("save ai placeholder: %w", err)
	}

	// 阶段 2.7：流式生成。st 承载可变状态，defer 委托 cleanupRAGStream 处理中断路径。
	// streamCompleted 区分两种中断：
	//   - 流未完成（LLM 不可用 / chunk.Err / out.Write 失败）：清理为拒答，避免空 content 孤儿消息。
	//   - 流已完成但后续步骤因 ctx 取消失败：保留真实答案（经输出安全审查），不覆盖为拒答。
	// finalized 阻止 defer 重复清理——正常路径 finalize 成功后置 true。
	st := &ragStreamState{aiMsgID: aiMsg.ID, chunks: chunks}
	defer func() { s.cleanupRAGStream(ctx, st) }()

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
		return s.handleEmptyStream(ctx, st, out, emergencyWarned)
	}

	slog.InfoContext(ctx, "chat: LLM stream completed",
		"tokens", st.full.Len(), "duration_ms", time.Since(startTime).Milliseconds())

	// 阶段 2.8 + 阶段 3：输出侧安全审查 + 事务内 finalize AI 消息
	if err := s.finalizeRAGOutput(ctx, st, out); err != nil {
		return err
	}
	slog.InfoContext(ctx, "chat: request completed", "result_code", constants.ResultAnswered)
	return nil
}

// handleEmptyStream LLM 流正常结束但未产生任何 token：显式 finalize placeholder 为拒答。
// finalized=true 阻止 defer 重复清理（否则 cleanupRAGStream 会再次清理并产生误导日志）。
func (s *ChatSendService) handleEmptyStream(
	ctx context.Context, st *ragStreamState, out SSEWriter, emergencyWarned bool,
) error {
	slog.WarnContext(ctx, "llm stream returned empty content, degrading to rejection")
	st.streamCompleted = false
	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), ragCleanupTimeout)
	if ferr := s.msg.FinalizeAssistant(
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

// stageRAGAnonymous 匿名用户 RAG 流式生成：跳过会话持久化与历史上下文，仅检索 + 流式输出。
// 无消息落库。
func (s *ChatSendService) stageRAGAnonymous(
	ctx context.Context, in StreamInput, out SSEWriter, emergencyWarned bool,
) error {
	// 查询改写（三级降级：专用改写 → 主 LLM 兜底 → 原始查询）
	query := s.rewriteQuery(ctx, in.Message, nil)

	// 检索（不限科室，匿名用户无科室偏好）；TopK=0 让 RAGConfig.TopK 接管（与认证路径一致，
	// 修死 硬编码 DefaultTopK 导致管理员配置 top_k 对 chat 永远失效）。
	chunks, err := s.knowledge.SearchSimilarChunks(ctx, rag.SearchQuery{
		Query: query, DeptID: nil, TopK: 0,
	})
	if err != nil {
		slog.ErrorContext(ctx, "knowledge search failed for anonymous, degrading to rejection", "err", err)
		return s.writeAnonymousRejection(out, emergencyWarned)
	}
	if len(chunks) == 0 {
		slog.WarnContext(ctx, "knowledge search returned 0 chunks for anonymous, degrading to rejection",
			"query_len", len(query))
		return s.writeAnonymousRejection(out, emergencyWarned)
	}
	// 与认证路径一致的 OOD 检出（REQ-CHAT-003）：所有切片向量相似度都低时拒答，
	// 避免匿名用户绕过知识库外检测（医疗场景严肃化一致性）。
	if isOutOfDomain(chunks, s.oodThreshold(ctx)) {
		slog.WarnContext(ctx, "chat: anonymous OOD detected, all chunks below threshold",
			"chunk_count", len(chunks))
		return s.writeAnonymousRejection(out, emergencyWarned)
	}

	// 推送引用
	if err := out.Write("references", chunks); err != nil {
		return err
	}

	slog.InfoContext(ctx, "chat: anonymous RAG search completed", "chunks", len(chunks))

	// 流式 LLM 生成（无历史）。改写后的 query 仅用于上方检索；
	// UserMessage 用用户原话，忠实原始表述（与认证路径一致）。
	streamCtx, cancelStream := context.WithTimeout(ctx, llmStreamTimeout)
	defer cancelStream()
	streamCh, streamErr := s.llm.StreamChat(streamCtx, llm.ChatRequest{
		SystemPrompt:  s.buildSystemPrompt(ctx),
		History:       nil,
		UserMessage:   in.Message,
		ContextChunks: chunkContents(chunks),
	})
	if streamErr != nil {
		return apperrors.ServiceUnavailable("CHAT_LLM_UNAVAILABLE", "AI 服务暂不可用，请稍后重试")
	}
	var full strings.Builder
	for chunk := range streamCh {
		if chunk.Err != nil {
			slog.ErrorContext(ctx, "llm stream error", "err", chunk.Err)
			return apperrors.ServiceUnavailable("CHAT_LLM_UNAVAILABLE", "AI 服务暂不可用，请稍后重试")
		}
		if chunk.Done {
			break
		}
		if err := out.Write("token", chunk.Token); err != nil {
			return err
		}
		full.WriteString(chunk.Token)
	}

	if full.Len() == 0 {
		slog.WarnContext(ctx, "chat: anonymous LLM stream produced no tokens, rejecting")
		_ = out.Write("safety_warning", s.safetyIn.SystemErrorMessage())
		return out.Write("done", "[DONE]")
	}

	// 输出安全审查（与 finalizeRAGOutput 一致的 mode 载荷，前端据此覆盖/追加 UI 内容）
	if err := s.writeOutputSafetyWarning(ctx, out, full.String()); err != nil {
		return err
	}

	slog.InfoContext(ctx, "chat: anonymous request completed", "tokens", full.Len())
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

// writeAnonymousRejection 匿名用户检索失败/空结果降级：推送拒答 warning + done。
// emergencyWarned=true 时跳过 warning（紧急就医提醒已下发，避免双 warning）。
func (s *ChatSendService) writeAnonymousRejection(out SSEWriter, emergencyWarned bool) error {
	if !emergencyWarned {
		if werr := out.Write("safety_warning", s.safetyIn.NoKnowledgeMessage()); werr != nil {
			return werr
		}
	}
	return out.Write("done", "[DONE]")
}

// writeOutputSafetyWarning 输出安全审查：命中时推送 safety_warning（{"mode","text"}）。
//   - mode=replace：越权内容已替换为安全话术，text 为完整安全话术。
//   - mode=append：追加免责声明，text 为追加部分。
func (s *ChatSendService) writeOutputSafetyWarning(ctx context.Context, out SSEWriter, content string) error {
	out2 := s.safetyOut.Validate(ctx, content)
	if !out2.Changed {
		return nil
	}
	mode := "replace"
	warning := out2.Final
	if !out2.Blocked {
		mode = "append"
		if strings.HasPrefix(out2.Final, content) {
			warning = out2.Final[len(content):]
		} else {
			// 防御：前缀不匹配（不应发生），降级为 replace 避免前端追加错误内容
			mode = "replace"
			warning = out2.Final
		}
	}
	return out.Write("safety_warning", map[string]string{"mode": mode, "text": warning})
}

// prepareRAGContext 阶段 2.1~2.4：加载并裁剪历史、查询改写（失败降级原始查询）、知识库检索。
// currentUserMsgID 为当前轮用户消息 ID：阶段 1 已将其持久化，历史加载须排除，
// 否则 LLM 上下文出现两条连续 user 消息（原始问题 + 改写问题）。
// 返回 rewrittenQuery（改写后，用于检索）、originalQuery（用户原话，用于生成）、
// 裁剪后的历史（不含当前用户消息）、检索到的 chunks。
// 检索失败或无结果时降级为拒答（finalizeRejection），返回其错误供 stageRAG 直接透传。
func (s *ChatSendService) prepareRAGContext(
	ctx context.Context, in StreamInput, conv *entity.Conversation,
	out SSEWriter, emergencyWarned bool, currentUserMsgID uuid.UUID,
) (rewrittenQuery, originalQuery string, history []*entity.Message, chunks []rag.Chunk, err error) {
	// 阶段 2.1：历史消息（最近 N 轮，排除当前轮用户消息）
	history, err = s.msg.GetRecentHistory(ctx, conv.ID, constants.HistoryTurns, &currentUserMsgID)
	if err != nil {
		return "", "", nil, nil, fmt.Errorf("load history: %w", err)
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
	// 与匿名路径共用 rewriteQuery。**无论首问/多轮都改写**：短问/口语问需扩写提升检索召回
	// （如"怎么控制"→"高血压的日常护理方法"）。改写结果仅用于检索；生成侧仍用用户原话。
	llmHistory := toLLMMessages(history)
	rewrittenQuery = s.rewriteQuery(ctx, in.Message, llmHistory)

	// 阶段 2.3：检索（跨域 wiki 域，阶段 2 实现；阶段 1 此处可能返回 ErrNotImplemented）。
	// TopK=0 让 RAGConfig.TopK 接管——修死 硬编码 DefaultTopK 使管理员配置 top_k 对 chat 失效。
	chunks, err = s.knowledge.SearchSimilarChunks(ctx, rag.SearchQuery{
		Query: rewrittenQuery, DeptID: deptIDPtr(in.SelectedDeptID, conv), TopK: 0,
	})
	if err != nil {
		// 检索失败降级为拒答：阶段 1 用户消息已在阶段 1 事务内提交，
		// 若直接返回 503 会留下无 assistant 回复的孤儿 user 消息，污染会话历史。
		// 降级路径写入 assistant 拒答消息保证会话完整性，并记录原始错误供排查。
		slog.ErrorContext(ctx, "knowledge search failed, degrading to rejection", "err", err)
		return "", "", nil, nil, s.finalizeRejection(ctx, conv, out, emergencyWarned, s.safetyIn.NoKnowledgeMessage())
	}

	// 阶段 2.4：无检索结果拒答（REQ-CHAT-003）
	if len(chunks) == 0 {
		slog.WarnContext(ctx, "knowledge search returned 0 chunks, degrading to rejection",
			"query_len", len(rewrittenQuery), "dept_id", deptIDPtr(in.SelectedDeptID, conv))
		return "", "", nil, nil, s.finalizeRejection(ctx, conv, out, emergencyWarned, s.safetyIn.NoKnowledgeMessage())
	}

	// 阶段 2.4b：OOD 检测 - 所有切片向量相似度都很低时拒答（医疗场景严肃化）
	// oodThreshold 动态读取 DB rag_configs.ood_threshold，热生效。
	// 当 similarity_threshold=0（管理员要求不过滤）时，VecScore=0.445 等相关切片仍可通过 OOD。
	threshold := s.oodThreshold(ctx)
	if isOutOfDomain(chunks, threshold) {
		slog.WarnContext(ctx, "chat: OOD detected, all chunks below threshold",
			"chunk_count", len(chunks), "ood_threshold", threshold)
		return "", "", nil, nil, s.finalizeRejection(ctx, conv, out, emergencyWarned, s.safetyIn.NoKnowledgeMessage())
	}

	slog.InfoContext(ctx, "chat: RAG search completed",
		"chunks", len(chunks), "query_len", len(rewrittenQuery))
	// 生成侧返回用户原话（in.Message），改写仅用于检索；
	// 如此 LLM 忠实于患者原始表述，改写器扩写偏差不再污染答案，且可被精确否决（references 仍指向改写检索结果）。
	return rewrittenQuery, in.Message, history, chunks, nil
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

// finalizeRAGOutput 阶段 2.8（输出侧安全审查，REQ-CHAT-012~014）+ 阶段 3（事务内 finalize AI 消息）。
// 输出审查触发时推送 safety_warning（spec §3.1），data 为 JSON 对象 {"mode","text"}：
//   - mode=replace：越权内容已替换为安全话术，text 为完整安全话术，前端据此覆盖已累积的 token 缓冲。
//   - mode=append：追加免责声明，text 为追加部分，前端追加到累积答案末尾。
//
// 这样 UI 最终内容与 DB 持久化内容保持一致（修复前 UI 保留被拦截的原始内容）。
func (s *ChatSendService) finalizeRAGOutput(ctx context.Context, st *ragStreamState, out SSEWriter) error {
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

	// 阶段 3：事务内 finalize AI 消息
	err := s.tx.WithTx(ctx, func(ctx context.Context) error {
		resultCode := constants.ResultAnswered
		if out2.Blocked {
			resultCode = constants.ResultIntercepted
		} else if st.partial {
			resultCode = constants.ResultPartial
		}
		return s.msg.FinalizeAssistant(ctx, st.aiMsgID, final, resultCode, toEntityRefs(st.chunks))
	})
	if err != nil {
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
func (s *ChatSendService) cleanupRAGStream(ctx context.Context, st *ragStreamState) {
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
		if ferr := s.msg.FinalizeAssistant(
			cleanupCtx, st.aiMsgID, out2.Final, resultCode, toEntityRefs(st.chunks),
		); ferr != nil {
			slog.ErrorContext(ctx, "cleanup finalized stream failed", "err", ferr)
		}
		return
	}
	if st.full.Len() > 0 {
		out2 := s.safetyOut.Validate(cleanupCtx, st.full.String())
		if out2.Blocked {
			if ferr := s.msg.FinalizeAssistant(
				cleanupCtx, st.aiMsgID, s.safetyIn.SystemErrorMessage(), constants.ResultRejected, nil,
			); ferr != nil {
				slog.ErrorContext(ctx, "cleanup unsafe partial failed", "err", ferr)
			}
			return
		}
		if ferr := s.msg.FinalizeAssistant(
			cleanupCtx, st.aiMsgID, out2.Final, constants.ResultPartial, toEntityRefs(st.chunks),
		); ferr != nil {
			slog.ErrorContext(ctx, "cleanup partial content failed", "err", ferr)
		}
		return
	}
	if ferr := s.msg.FinalizeAssistant(
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
	ctx context.Context, conv *entity.Conversation, out SSEWriter, emergencyWarned bool, msg string,
) error {
	err := s.tx.WithTx(ctx, func(ctx context.Context) error {
		_, err := s.msg.SaveAssistant(ctx, conv.ID, msg, constants.ResultRejected, nil)
		return err
	})
	if err != nil {
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

// persistUserMessageAndCrisis 事务内子操作：确保会话 + 用户消息 + 危机事件。
// ponytail: 调用方 handleCrisis 已包裹 s.tx.WithTx，message 与 crisis_event 原子提交，简化——
// 故先 SaveUserMessage 拿到 messageID，再一次性 INSERT crisis_event(message_id)，
// 省去原先"INSERT(NULL) + UPDATE(message_id)"中的 UPDATE。
func (s *ChatSendService) persistUserMessageAndCrisis(
	ctx context.Context, in StreamInput, conv *entity.Conversation,
	dept rag.Department, c *rag.Crisis,
) (*entity.Conversation, *entity.Message, int64, error) {
	c2, userMsg, err := s.ensureConversationAndUserMessage(ctx, in, conv, dept)
	if err != nil {
		return nil, nil, 0, err
	}
	e := &entity.CrisisEvent{
		PatientID:        in.UserID,
		ConversationID:   c2.ID,
		TriggeredContent: in.Message,
		MatchedKeywords:  c.Keywords,
		Level:            c.Level,
	}
	// 用户消息已在同事务内保存，直接关联 message_id——避免后续 UPDATE 回填。
	if userMsg != nil {
		msgID := userMsg.ID
		e.MessageID = &msgID
	}
	id, err := s.crisis.Create(ctx, e)
	if err != nil {
		return nil, nil, 0, err
	}
	return c2, userMsg, id, nil
}

// ensureConversationAndUserMessage 保存用户消息 + 设置标题 + 更新 last_message_at。
// 会话由 loadOrPrepareConversation 提前创建（新会话）或加载（已有会话），此处不再创建。
// 已有会话未锁定科室时用当前 dept 锁定（兼容历史数据）；新会话已锁定，跳过。
func (s *ChatSendService) ensureConversationAndUserMessage(
	ctx context.Context, in StreamInput, conv *entity.Conversation, dept rag.Department,
) (*entity.Conversation, *entity.Message, error) {
	// 会话未锁定时，仅当用户明确选择具体科室（>0）才锁定到该科室；
	// nil/0 一律视为"全部科室"，保持未锁定（检索不限科室），修复旧逻辑把未传科室误锁到解析科室的问题。
	if conv.LockedDeptID == nil && in.SelectedDeptID != nil && *in.SelectedDeptID > 0 {
		if err := s.conv.LockDept(ctx, conv.ID, dept.ID); err != nil {
			return nil, nil, err
		}
		deptID := dept.ID
		conv.LockedDeptID = &deptID
	}
	msg, err := s.msg.SaveUserMessage(ctx, conv.ID, in.Message)
	if err != nil {
		return nil, nil, err
	}
	// 设置标题（首条消息前 20 字截断，REQ-CHAT-018）
	if err := s.conv.UpdateTitleIfEmpty(ctx, conv.ID, truncateTitle(in.Message)); err != nil {
		return nil, nil, err
	}
	if err := s.conv.TouchLastMessageAt(ctx, conv.ID); err != nil {
		return nil, nil, err
	}
	return conv, msg, nil
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
