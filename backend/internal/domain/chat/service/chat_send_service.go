// Package service 实现 chat 域业务编排：RAG 流式问答、会话管理、危机事件处理。
// 事务边界在本层开启（postgres.TxManager.WithTx），Repository 接收 ctx 内的 tx。
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"health-nexus/internal/domain/chat/entity"
	"health-nexus/internal/platform/llm"
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
	// EventPing SSE 心跳：长时间无业务数据（高风险答案先生成、后审核）时保持连接可观测，
	// 前端据此判定"连接存活"，不进入答案正文。
	EventPing = "ping"
	// EventStage 阶段进度：告知前端当前处于哪个处理阶段（理解/检索/组织/核对），
	// 仅用于展示状态文案，不影响正文；未知阶段由前端忽略。
	EventStage = "stage"
)

// stage 事件的阶段取值（粗粒度、中性文案，前端映射为展示文字）。
const (
	StageUnderstanding = "understanding" // 理解问题（统一理解与审查）
	StageRetrieving    = "retrieving"    // 检索知识
	StageComposing     = "composing"     // 组织回答（流式生成前）
	StageReviewing     = "reviewing"     // 核对内容（生成后语义审核）
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

// stagePayload 阶段进度载荷（EventStage）。
type stagePayload struct {
	Stage string `json:"stage"`
}

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
	promptProvider rag.SystemPromptProvider // 可为 nil：降级为代码默认人格
	conv           ConversationPort
	msg            MessagePort
	crisis         CrisisPort
	crisisNotifier CrisisNotifier // 危机事件主动通知（入队 asynq 任务，落库站内通知给 DEPT_ADMIN）
	locker         LockProvider
	tx             TxRunner
	ring           ringStore    // 匿名会话瞬态上下文环（Redis）；nil 时匿名退化为单轮（无历史）
	turns          TurnRegistry // 请求幂等登记；nil 时不启用幂等（重复提交会重复生成）
}

// writeStage 尽力下发阶段进度事件；写失败（多为客户端已断开）不中止主流程——
// 阶段仅用于展示，不应因进度推送失败影响问答本身。
func (s *ChatSendService) writeStage(ctx context.Context, out SSEWriter, stage string) {
	if err := out.Write(EventStage, stagePayload{Stage: stage}); err != nil {
		slog.WarnContext(ctx, "chat: write stage event failed", "stage", stage, "err", err)
	}
}

// turnResultPayload 本轮权威结果载荷。
type turnResultPayload struct {
	TurnID             string             `json:"turn_id"`
	UserMessageID      string             `json:"user_message_id"`
	AssistantMessageID string             `json:"assistant_message_id"`
	ResultCode         string             `json:"result_code"`
	References         []entity.Reference `json:"references"`
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

// CrisisNotifier 危机事件主动通知接口（入队 asynq 任务，由 worker 落库站内通知）。
type CrisisNotifier interface {
	NotifyCrisis(ctx context.Context, eventID int64) error
}

// NewChatSendService 构造 RAG 服务。
// 跨域依赖（dept/knowledge/safetyIn/safetyOut/assessor/outputReviewer/promptProvider）通过 interface 注入。
// assessor 可为 nil：视为审查不可用，每轮按"无法判断"降级（受限兜底），不会放行为安全。
// outputReviewer 可为 nil：高风险答案跳过语义审核并记录告警（不阻断普通宣教）。
// promptProvider 可为 nil：降级为代码默认人格（便于测试；生产必有 provider）。
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

	// (0.5) 原子占用 request_id（SET NX）：防止同 request_id 的并发提交各自创建会话并生成（P2）。
	// 占位成功后才继续；释放回调在本请求未产生终态时清除占位，避免异常中断后无法重试。
	release, err := s.reserveTurn(ctx, in)
	if err != nil {
		return err
	}
	defer release()

	// (1)~(2) 会话准备（REQ-CHAT-019）：统一构建 Session（认证=DB 会话+科室锁定；匿名=Redis 瞬态会话）。
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
	// 先一次性读取历史快照（供审查 / 检索 / 生成三阶段复用），再据此审查。
	// 下发阶段进度：理解问题（通常是首 token 前最长的一段等待）。
	s.writeStage(ctx, out, StageUnderstanding)
	history, assessment, aerr := s.loadSnapshotAndAssess(ctx, in, sess)
	if aerr != nil {
		// 审查不可用（历史读取失败 / 超时 / 解析失败 / 字段非法）：按"无法判断"降级，
		// 绝不回退为"安全放行"，也不用空历史做风险判断（P1）。
		slog.WarnContext(ctx, "chat: assessment unavailable, degrading to restricted reply", "err", aerr)
		if err := s.registerTurn(ctx, in, sess, st.turnID); err != nil {
			return err
		}
		return s.handleFixedReply(ctx, in, sess, out, rag.AssessmentFailedMessage(), constants.ResultRejected, st)
	}

	// 动作分流：所有分支都先登记本轮（结果可被重复提交回放）。
	if handled, derr := s.dispatchByAssessment(ctx, in, sess, out, assessment, history, st); handled {
		return derr
	}

	// 普通宣教：登记本轮后进入检索 + 生成链路。复用同一历史快照，避免二次读取的时序漂移。
	if err := s.registerTurn(ctx, in, sess, st.turnID); err != nil {
		return err
	}
	return s.stageRAG(ctx, in, sess, out, st, assessment, history)
}

// loadSnapshotAndAssess 读取本轮历史快照并执行统一理解与审查。
// 历史在本轮用户消息落库之前一次性读取（无需排除当前轮），供审查 / 检索 / 生成三阶段复用。
// 读取失败**必须**作为错误返回（调用方按"审查不可用"降级），不能静默降级为空历史——
// 追问常依赖上文语境（上一轮已触发危机/急症，本轮只说"那现在呢"），
// 用空历史做安全判断会丢失语境、误判为普通宣教放行（P1）。
func (s *ChatSendService) loadSnapshotAndAssess(
	ctx context.Context, in StreamInput, sess *Session,
) ([]*entity.Message, rag.Assessment, error) {
	history, err := sess.store.History(ctx, constants.HistoryTurns, nil)
	if err != nil {
		return nil, rag.Assessment{}, fmt.Errorf("%w: load history: %w", rag.ErrAssessmentInvalid, err)
	}
	assessment, err := s.assess(ctx, in, sess.DeptName, history)
	if err != nil {
		return nil, rag.Assessment{}, err
	}
	return history, assessment, nil
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
		if errors.Is(err, apperrors.ErrLockNotAcquired) {
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
// history 为本轮历史快照，供澄清守卫判断上一轮是否已追问（不参与其他分流）。
func (s *ChatSendService) dispatchByAssessment(
	ctx context.Context, in StreamInput, sess *Session, out SSEWriter,
	assessment rag.Assessment, history []*entity.Message, st *ragStreamState,
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
		// 责任科室已明确 + 患者教育意图：澄清判定降级为检索。
		// 依据审查规则 7（科室已限定时，术后/饮食/出院等常规宣教问题视为信息充分），
		// 但 LLM 判定存在非确定性（实测同一「胸外科 + 术后康复需要注意什么」会交替
		// clarify/retrieve），代码兜底保证稳定：科室已限定即直接检索，0 命中再由 2.4 的
		// "暂无相关内容"兜底。不绕过任何安全动作（crisis/emergency/restricted/reject
		// 在 Assessment.Action() 中优先级更高）。
		if sess.DeptID != nil && assessment.Intent == rag.IntentPatientEducation {
			slog.InfoContext(ctx, "chat: clarify downgraded to retrieve (dept-scoped patient education)")
			return false, nil
		}
		// 一次性澄清守卫：上一轮助手已是澄清追问、患者本轮多半就是在回答它，若模型仍判信息不足，
		// 直接跳过澄清、转入检索（命中则作答，0 命中走"暂无相关内容"兜底），避免"追问→回答→再追问"死循环。
		if lastAssistantWasClarification(history) {
			slog.InfoContext(ctx, "chat: skip repeated clarification, fall through to retrieve")
			return false, nil
		}
		// 关键信息不足：先澄清，不检索不生成（避免用不适用的一般性资料作答）。
		if err := register(); err != nil {
			return true, err
		}
		// 澄清问题由模型生成，落库/下发前必须经输出审查；越权则退回代码内固定澄清话术（P1）。
		reply := s.sanitizeModelText(ctx,
			clarificationText(assessment.ClarificationQuestion), rag.ClarificationFallback())
		return true, s.handleFixedReply(ctx, in, sess, out, reply, constants.ResultRejected, st)
	}
	return false, nil
}

// lastAssistantWasClarification 判断历史中最近一条助手回复是否为澄清追问。
// 一次性澄清守卫据此阻断重复追问；历史为空或无助手回复时返回 false（不误拦首次澄清）。
func lastAssistantWasClarification(history []*entity.Message) bool {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != constants.MessageRoleAssistant {
			continue
		}
		return rag.IsClarificationReply(history[i].Content)
	}
	return false
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

// sanitizeModelText 对模型给出的动态文本（澄清问题 / 修正答案）做输出安全审查后再使用。
//
// 这两个字段由模型生成、会被落库并向患者展示，绝不能直接信任：模型可能借"澄清"或
// "修正答案"的出口输出越权内容（停药 / 诊断 / 延误就医等），绕过正常输出审查。
// 语义：
//   - 内容被替换/拦截（Blocked）：说明含越权表述，放弃模型文本，改用调用方提供的固定安全话术；
//   - 审查输出为空：异常，同样回退安全话术；
//   - 通过：返回审查后的文本（可能与原文相同，或已追加免责声明）。
//
// fallback 必须为代码内固定安全话术（不得再来自模型）。
func (s *ChatSendService) sanitizeModelText(ctx context.Context, text, fallback string) string {
	if strings.TrimSpace(text) == "" {
		return fallback
	}
	res := s.safetyOut.Validate(ctx, text)
	if res.Blocked {
		slog.WarnContext(ctx, "chat: model-provided text blocked by output safety, using safe fallback")
		return fallback
	}
	if strings.TrimSpace(res.Final) == "" {
		return fallback
	}
	return res.Final
}

// assess 执行统一理解与审查。
// history 为本轮统一读取的历史快照（由 Stream 一次性加载并复用），此处不再二次读取。
// deptName 为当前会话锁定科室名（可为空=不限科室），作为审查的语境提示，
// 使科室已限定的常规宣教问题不被误判为信息不足。
func (s *ChatSendService) assess(
	ctx context.Context, in StreamInput, deptName string, history []*entity.Message,
) (rag.Assessment, error) {
	if s.assessor == nil {
		return rag.Assessment{}, fmt.Errorf("%w: assessor not injected", rag.ErrAssessmentInvalid)
	}
	return s.assessor.AssessAndRewrite(ctx, in.Message, deptName, toAssessTurns(history))
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

// turnReservedMarker 幂等登记的"进行中"占位值。原子占位（SET NX）成功时先写此标记，
// 生成完成后由 registerTurn 覆盖为权威 turnRef。第二个并发请求查到该标记即判 409（生成中）。
const turnReservedMarker = "__reserved__"

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

// sentenceBoundaries 流式输出审查的分句边界（句末标点与换行）。
// 不含半角句点：安全规则模式不含这些字符，整句入审不会漏检；反之在句中切分可能切断规则匹配。
const sentenceBoundaries = "。！？!?；;\n"

// medicationIntentRe 对话中出现用药相关意图的信号（剂量/加量/停药等）。
// 用于判断本轮是否必须"完整生成 + 审核后展示"：这类答案一旦越界（给出个体化剂量方案），
// 事后修正无法消除已展示内容的影响。
var medicationIntentRe = regexp.MustCompile(
	`(剂量|用量|加量|减量|停药|换药|加倍|翻倍|加服|少吃|多吃|mg|毫克|几片|吃几)`)

// contentRiskHigh 高风险内容等级取值（对齐 wiki 域 articles.content_risk='high'）。
const contentRiskHigh = "high"

// errRejectionHandled 拒答已处理哨兵错误。finalizeRejection 推送 result + done 后返回此值，
// 调用方（prepareRAGContext -> stageRAG）据此停止后续 RAG 流程，避免重复推送事件。
var errRejectionHandled = errors.New("rejection already handled")

// minGenerationBudget 生成侧可压缩到的预算下限（token）。
// 提示词本身已占满预算时，仍需给切片/历史留余地，否则会退化成一问一答无资料的"裸生成"。
const minGenerationBudget = 2000

// citeMarkerRe 匹配答案中的内联引用标记 [资料N]（N 为文章级编号）。
var citeMarkerRe = regexp.MustCompile(`\[资料\s*(\d+)\]`)
