package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"health-nexus/internal/domain/chat/entity"
	"health-nexus/internal/platform/llm"
	"health-nexus/internal/shared/constants"
	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/rag"
)

// stageRAG 阶段 1（持久化用户消息 + assistant 占位）+ 阶段 2（检索 + 流式生成）+ 阶段 3（持久化 AI 消息）。
// 认证/匿名统一走此链：持久化全部委托 sess.store（认证=DB 会话，匿名=Redis 瞬态环），链条不感知身份。
// st 为本轮状态（turn_id / 消息 ID / 生成结果），由 Stream 创建并贯穿至权威结果事件。
// 编排各子阶段：prepareRAGContext（历史/改写/检索）→ streamLLMTokens（流式审查推送）→ finalizeRAGOutput（复核 + 落库），
// defer 委托 cleanupRAGStream 处理中断路径的孤儿占位消息清理。
func (s *ChatSendService) stageRAG(
	ctx context.Context, in StreamInput, sess *Session, out SSEWriter, st *ragStreamState,
	assessment rag.Assessment, snapshot []*entity.Message,
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

	// 阶段 2.1~2.4：历史裁剪 + 检索（检索失败/空结果在内部降级为拒答）。
	// 复用 Stream 一次性读取的历史快照（先于本轮用户消息落库，天然不含当前轮，无需再排除）。
	// 检索用统一审查给出的独立问题（assessment.StandaloneQuery）；生成用患者原话（originalQuery）。
	// 系统提示词先于检索确定：生成侧 token 预算需要扣除提示词占用（prompt 体积不可控）。
	systemPrompt, promptSel := s.buildSystemPrompt(ctx)
	s.recordPromptSource(ctx, sess, st, promptSel)

	s.writeStage(ctx, out, StageRetrieving)
	originalQuery, history, chunks, err := s.prepareRAGContext(
		ctx, in, sess, out, st, assessment, snapshot, systemPrompt)
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

	// 下发阶段进度：组织回答（检索完成，进入流式生成）。
	s.writeStage(ctx, out, StageComposing)

	// 阶段 2.7：流式生成。st 承载可变状态，defer 委托 cleanupRAGStream 处理中断路径。
	// streamCompleted 区分两种中断：
	//   - 流未完成（LLM 不可用 / chunk.Err / out.Write 失败）：清理为拒答，避免空 content 孤儿消息。
	//   - 流已完成但后续步骤因 ctx 取消失败：保留真实答案（经输出安全审查），不覆盖为拒答。
	// finalized 阻止 defer 重复清理——正常路径 finalize 成功后置 true。

	// 高风险内容（涉及用药/个体化）不做流式先发：st.deferred=true 时完整生成后在 finalizeRAGOutput
	// 统一做语义审核，通过才展示——流式内容一旦下发，事后修正无法消除先前影响。
	// 判定依据完整对话语义 + 还原问题 + 知识风险等级（不只看当前这一句，P1）。
	st.deferred = requiresFullReview(assessment, in.Message, history, chunks)

	slog.InfoContext(ctx, "chat: LLM stream started",
		"history_turns", len(history)/2, "chunks", len(chunks), "deferred", st.deferred)
	startTime := time.Now()
	if err := s.streamLLMTokens(ctx, systemPrompt, originalQuery, history, chunks, out, st); err != nil {
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

	// 引用合规观测：答案实际引用的文章数 / 候选数（前端"参考来源"只展示被引用的文章）。
	// 零引用（但存在候选）时告警——提示模型未遵守规则 8，也便于统计引用合规率。
	if len(chunks) > 0 {
		cited := citedArticleCount(st.content.String(), chunks)
		slog.InfoContext(ctx, "chat: citation coverage", "cited", cited, "candidates", len(chunks))
		if cited == 0 {
			slog.WarnContext(ctx, "chat: answer cites no reference despite retrieved candidates")
		}
	}

	// 阶段 2.8 + 阶段 3：输出侧安全审查 + 持久化 finalize AI 消息
	if err := s.finalizeRAGOutput(ctx, sess, st, out, in.Message); err != nil {
		return err
	}
	slog.InfoContext(ctx, "chat: request completed", "result_code", constants.ResultAnswered)
	return nil
}

// requiresFullReview 判断本轮答案是否必须完整生成、经生成后语义审核通过再展示。
//
// P1：不能只匹配"当前这一句"的关键词——多轮追问常省略主语（上文问剂量，下一轮只问"那晚上呢？"）。
// 判定依据三类信号，任一命中即需审核：
//  1. 本轮统一审查的语义结论（用药调整 / 个体化诊疗）；
//  2. 完整对话语义：历史消息 + 本轮统一审查产出的独立问题（StandaloneQuery，已还原指代）
//     出现用药/剂量意图——覆盖"上文讨论剂量、本轮省略主语的追问"；
//  3. 知识风险等级：命中高风险（high）资料时答案涉及用药/检查准备等，必须审核。
func requiresFullReview(a rag.Assessment, message string, history []*entity.Message, chunks []rag.Chunk) bool {
	if a.MedicationChange || a.IndividualizedDx {
		return true
	}
	if hasHighRiskChunk(chunks) {
		return true
	}
	// 对话语义：当前原话 + 还原后的独立问题。
	if medicationIntentRe.MatchString(message) || medicationIntentRe.MatchString(a.StandaloneQuery) {
		return true
	}
	// 历史：任一轮（用户或助手）讨论过用药即认为本轮处于用药语境。
	for _, m := range history {
		if medicationIntentRe.MatchString(m.Content) {
			return true
		}
	}
	return false
}

// hasHighRiskChunk 命中的资料中是否存在高风险（用药/检查准备/高风险护理）文章。
// 与 wiki 域 entity.ContentRiskHigh 取值一致；此处用字面量避免 chat 域反向依赖 wiki 域。
func hasHighRiskChunk(chunks []rag.Chunk) bool {
	for _, c := range chunks {
		if c.ContentRisk == contentRiskHigh {
			return true
		}
	}
	return false
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

// prepareRAGContext 阶段 2.1~2.4：裁剪历史快照与检索切片、按统一审查给出的独立问题检索。
// snapshot 为 Stream 一次性读取的历史快照（先于本轮用户消息落库，天然不含当前轮），
// 此处直接复用而非二次读取——避免"审查用历史 A、生成用历史 B"的时序漂移，也减少一次 IO。
// 检索问题来自 assessment.StandaloneQuery（审查阶段已保留否定/时间/人群等限定），
// 生成侧仍用患者原话（in.Message）——改写偏差不污染答案。
// systemPrompt 参与生成侧 token 预算（提示词 + 切片 + 历史 共享同一预算）。
// 检索失败或无结果时降级为拒答（finalizeRejection），返回其错误供 stageRAG 直接透传。
func (s *ChatSendService) prepareRAGContext(
	ctx context.Context, in StreamInput, sess *Session,
	out SSEWriter, st *ragStreamState, assessment rag.Assessment, snapshot []*entity.Message,
	systemPrompt string,
) (originalQuery string, history []*entity.Message, chunks []rag.Chunk, err error) {
	// 阶段 2.1：历史消息（最近 N 轮，复用本轮快照）
	history = snapshot

	// 阶段 2.1b：生成阶段 Token 预算兜底（REQ-CHAT-006-A）。
	// 检索改写已移至统一审查（其内部分别裁剪历史），故此处按生成预算对历史做一次 FIFO 裁剪。
	// ponytail: 近似 token 估算见 estimateTokens；上限——中文重场景估算偏低，极端情况下突破实际 LLM 上下文窗口，
	// 由 LLM 服务端报 context_length_exceeded 错误兜底（已在 streamErr 路径处理为 CHAT_LLM_UNAVAILABLE）。
	beforeTrim := len(history)
	history = trimHistoryForTokens(history, in.Message, generationBudget(systemPrompt, in.Message))
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
	// 改写漂移兜底：统一审查产出的改写可能与患者原话偏离导致 0 命中
	// （实测同一问题不同轮次改写不同，有的命中、有的不命中）。
	// 此时用患者原话再检索一次——只增加召回，相似度阈值与生成侧相关性过滤不变。
	if err == nil && len(chunks) == 0 && retrievalQuery != in.Message {
		slog.InfoContext(ctx, "chat: rewritten query yielded no hits, retry with raw message",
			"rewrite_len", len(retrievalQuery), "raw_len", len(in.Message))
		chunks, err = s.knowledge.SearchSimilarChunks(ctx, rag.SearchQuery{
			Query: in.Message, DeptID: sess.DeptID, TopK: 0,
		})
	}
	if err != nil {
		// 检索失败降级为拒答：阶段 1 用户消息已持久化，
		// 若直接返回 503 会留下无 assistant 回复的孤儿 user 消息，污染会话历史。
		// 用系统异常话术而非"知识库没有答案"——检索故障不等于没有资料（避免误导患者与医护）。
		slog.ErrorContext(ctx, "knowledge search failed, degrading to rejection", "err", err)
		return "", nil, nil, s.finalizeRejection(ctx, sess, st, out, s.safetyIn.SystemErrorMessage())
	}

	// 引用编号以"文章"为单位：归并同篇文章的命中切片。
	// 生成侧 [资料N]、审核侧证据、落库 references、SSE references 事件均基于该归并结果，
	// 与前端"参考来源"列表（按 article_id 去重）语义一致，避免"列表 1 条、正文却引用 [资料3]"。
	chunks = groupChunksByArticle(chunks)

	// 阶段 2.3b：切片纳入同一 token 预算（历史已裁剪完，剩余预算给切片）。
	// 之前切片不受预算约束：TopK×chunk_size 调大后，5 条 2000 字切片的 prompt 必然超窗，
	// 只能靠上游 context_length_exceeded 报错兜底（患者侧表现为"AI 服务暂不可用"）。
	// 切片按相似度降序，超预算时丢弃尾部（相关度最低）——只损失补充信息，不丢主线答案。
	chunkBudget := generationBudget(systemPrompt, in.Message) - estimateHistoryTokens(history, in.Message)
	before := len(chunks)
	chunks = trimChunksForTokens(chunks, chunkBudget)
	if len(chunks) != before {
		slog.InfoContext(ctx, "chat: chunks trimmed for generation",
			"before", before, "after", len(chunks), "budget", chunkBudget)
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
		ContextChunks: chunkContentsForLLM(chunks),
	})
	if streamErr != nil {
		return apperrors.ServiceUnavailable("CHAT_LLM_UNAVAILABLE", "AI 服务暂不可用，请稍后重试")
	}
	// doneSeen 记录是否收到模型的**完成证据**（Done 片段）。channel 关闭但未见 Done 即上游提前断流。
	doneSeen := false
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
			// 长度截断（max_tokens / 上下文窗口）：内容不完整，落库须为 PARTIAL，
			// 不得因"流正常结束"当成本轮生成完成。
			if chunk.Truncated {
				st.partial = true
			}
			doneSeen = true
			break
		}
		st.pending.WriteString(chunk.Token)
		if err := s.emitSafeSentences(ctx, out, st); err != nil {
			// 客户端断开：已推送内容视为不完整答案。
			st.partial = true
			return err
		}
	}
	// 审查并推送尾部不足一句的内容。
	if err := s.flushPendingTail(ctx, out, st); err != nil {
		st.partial = true
		return err
	}
	// 上游提前断流（channel 关闭但未收到完成证据）：已有内容按中断处理（PARTIAL + 截断提示），
	// 不得标记为完整回答——否则患者看到残缺答案且无任何提示（P1）。
	if !doneSeen {
		if st.content.Len() > 0 {
			st.partial = true
			slog.WarnContext(ctx, "chat: upstream stream closed before completion, marking partial",
				"tokens", st.content.Len())
			if err := out.Write(EventNotice, noticePayload{
				Kind: NoticeTimeout,
				Text: "（连接中断，以上为部分内容，完整回答请稍后重试）",
			}); err != nil {
				return err
			}
		} else {
			// 无任何内容：走拒答路径（defer 用 streamCompleted=false 清理为固定兜底话术）。
			st.streamCompleted = false
			return apperrors.ServiceUnavailable("CHAT_LLM_UNAVAILABLE", "AI 服务暂不可用，请稍后重试")
		}
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
		// 下发阶段进度：核对内容（完整生成后语义审核，可能持续数秒）。
		s.writeStage(ctx, out, StageReviewing)
		review, rerr := s.reviewAnswer(ctx, question, content, st.chunks)
		if rerr != nil {
			// 无法确认安全：不展示未经审核的答案（审查不可用 ≠ 安全）。
			slog.ErrorContext(ctx, "chat: deferred answer review unavailable, degrading", "err", rerr)
			return swallowRejection(s.finalizeRejection(ctx, sess, st, out, rag.AssessmentFailedMessage()))
		}
		if !review.Pass() {
			// 修正答案由模型生成：落库/下发前必须经输出审查，绝不可直接信任——
			// 否则模型可借"修正答案"出口输出越权内容（停药/诊断等），绕过输出审核（P1）。
			// 审查不通过或无修正文本时，退回代码内固定安全话术。
			alt := s.sanitizeModelText(ctx, review.RevisedAnswer, rag.RestrictedCareMessage())
			slog.WarnContext(ctx, "chat: deferred answer rejected by semantic review",
				"boundary_ok", review.BoundaryOK, "evidence_supported", review.EvidenceSupported,
				"reason", review.Reason)
			return swallowRejection(s.finalizeRejection(ctx, sess, st, out, alt))
		}
		// 审核通过：完整答案一次性下发（此前刻意未流式发送）。
		st.reviewed = true
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

// cleanupRAGStream defer 清理：正常路径（finalized=true）直接返回；
// 否则用 context.Background() + 超时执行清理——请求 ctx 可能在客户端断开时已取消，
// 此时用原 ctx 清理会因 context.Canceled 而失败，留下孤儿消息。
// 落库内容取 st.content（已经输出审查、且已推送给客户端的内容），保证 DB 与客户端所见一致。
//   - deferred 内容（高风险）未通过生成后语义审核：一律不落库未审核正文，落安全兜底话术 +
//     REJECTED——否则中断即绕过语义审核，患者重开会话即可看到未经审核的答案（P1）。
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
	// P1：deferred（高风险）内容要求"生成后语义审核通过才展示/落库"。中断路径未完成语义审核，
	// 直接落库等于让未审核正文绕过审核进入历史。此处放弃该片段，落安全兜底话术。
	if st.deferred && !st.reviewed {
		slog.WarnContext(ctx, "chat: deferred answer interrupted before semantic review, discard unreviewed content")
		if ferr := sess.store.FinalizeAssistant(
			cleanupCtx, st.aiMsgID, st.turnID, rag.AssessmentFailedMessage(), constants.ResultRejected, nil,
		); ferr != nil {
			slog.ErrorContext(ctx, "cleanup unreviewed deferred answer failed", "err", ferr)
		}
		return
	}
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

// recordPromptSource 将本轮使用的提示词模板版本写入 assistant 消息（远程均可复盘）。
// 写入失败只告警：溯源信息缺失不得阻断回答（主流程优先）。
func (s *ChatSendService) recordPromptSource(
	ctx context.Context, sess *Session, st *ragStreamState, sel rag.SystemPromptSelection,
) {
	if err := sess.store.SetAssistantPromptMeta(ctx, st.aiMsgID, sel.TemplateID, sel.Version); err != nil {
		slog.WarnContext(ctx, "chat: record prompt version failed",
			"template_id", sel.TemplateID, "version", sel.Version, "err", err)
	}
}

// buildSystemPrompt 构造完整的 system prompt：可配置人格（DB）+ 代码独占护栏。
// 返回的 SystemPromptSelection 携带模板 ID / 版本，由 recordPromptSource 落库。
//
// 分层是硬约束：护栏（医疗红线 / [资料N] 引用 / 手机端格式）永远由
// constants.SystemPromptGuardrails 追加，管理员只能在人格段内改口吻。
// config 域故障时降级为默认人格 + 护栏（答不完全不如服务不可用更安全），但失败必留日志
// ——不得静默当成「无配置」，否则提示词配错会长期无人发现。
func (s *ChatSendService) buildSystemPrompt(ctx context.Context) (string, rag.SystemPromptSelection) {
	if s.promptProvider != nil {
		sel, err := s.promptProvider.SystemPrompt(ctx)
		if err != nil {
			slog.WarnContext(ctx, "chat: load system prompt failed, falling back to default persona", "err", err)
			return constants.ComposeSystemPrompt(""), rag.SystemPromptSelection{}
		}
		slog.InfoContext(ctx, "chat: system prompt loaded",
			"source", promptSource(sel), "template_id", sel.TemplateID, "version", sel.Version)
		return constants.ComposeSystemPrompt(sel.Persona), sel
	}
	slog.InfoContext(ctx, "chat: system prompt loaded", "source", "default")
	return constants.ComposeSystemPrompt(""), rag.SystemPromptSelection{}
}

// promptSource 提示词来源标识（日志/复盘用）。
func promptSource(sel rag.SystemPromptSelection) string {
	if sel.TemplateID > 0 {
		return "database"
	}
	return "default"
}
