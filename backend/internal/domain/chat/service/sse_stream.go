package service

import (
	"context"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"health-nexus/internal/domain/chat/entity"
	"health-nexus/internal/shared/rag"
)

// SSEWriter SSE 事件写入接口，由 handler 层实现（消费者定义在 service 端）。
// 每个 token / 引用 / 危机事件通过 Write 推送给客户端并立即 flush。
type SSEWriter interface {
	Write(event string, data any) error
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
	partial         bool // LLM 超时/中断/长度截断导致答案不完整
	safetyChanged   bool // 流式过程中输出审查替换过内容
	// deferred 高风险内容不流式先发：生成结果先累积，经生成后语义审核通过后一次性下发。
	deferred bool
	// reviewed 生成后语义审核已通过。deferred 内容只有审核通过才可落库/展示——
	// 中断路径（defer cleanup）据此判断内容是否经过审核，避免未审核正文绕过审核写入历史。
	reviewed bool
}

// len 已产生（含未凑满一句的尾部）的答案长度，用于空流判断。
func (st *ragStreamState) len() int { return st.content.Len() + st.pending.Len() }

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
