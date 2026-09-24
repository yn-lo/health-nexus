package rag

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"
)

// 生成后语义审核（REQ-CHAT-012~014 增强）。
//
// 输入侧审查无法替代输出侧审查：审查发生在检索与生成之前，看不到最终答案，
// 因此无法判断"检索是否漏掉关键限制""答案是否编造结论""是否把通用知识写成了针对患者的医嘱"。
// 正则审查能挡住特定句式，但不判断医学适用性与证据支持，故高风险答案在展示前需再过一次语义审核。

// OutputReview 语义审核结果。
type OutputReview struct {
	// BoundaryOK 是否守住宣教边界（未输出未经授权的个体化诊疗/用药调整结论）。
	BoundaryOK bool `json:"boundary_ok"`
	// EvidenceSupported 关键结论是否有给定资料支持（含是否遗漏适用条件与重要例外）。
	EvidenceSupported bool `json:"evidence_supported"`
	// Reason 简短判断依据（供医护复核与测试，不要求长篇推理）。
	Reason string `json:"reason"`
	// RevisedAnswer 需修正时的安全替代答案；为空表示无需替换（仅当判定不通过时使用）。
	RevisedAnswer string `json:"revised_answer"`
}

// Pass 判定审核是否通过。
func (r OutputReview) Pass() bool { return r.BoundaryOK && r.EvidenceSupported }

// ErrOutputReviewUnavailable 语义审核不可用（超时/解析失败/未注入）。
var ErrOutputReviewUnavailable = errors.New("rag: output review unavailable")

// outputReviewMaxReasonRune Reason 字段长度上限（防止异常长输出）。
const outputReviewMaxReasonRune = 300

// Validate 校验审核结果字段。
func (r OutputReview) Validate() error {
	if utf8.RuneCountInString(r.Reason) > outputReviewMaxReasonRune {
		return fmt.Errorf("%w: reason too long", ErrOutputReviewUnavailable)
	}
	return nil
}

// OutputReviewer 生成后语义审核能力（消费者定义，由 adapter 适配 platform/llm 实现）。
type OutputReviewer interface {
	// ReviewAnswer 审核答案是否守住宣教边界、关键结论是否有资料支持。
	// 失败必须返回包装 ErrOutputReviewUnavailable 的错误：调用方按"无法确认安全"降级处理。
	ReviewAnswer(ctx context.Context, question, answer string, evidence []string) (OutputReview, error)
}
