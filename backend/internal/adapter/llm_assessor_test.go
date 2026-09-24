// 统一理解与审查 / 生成后语义审核的适配器测试：输出解析与失败降级。
package adapter

import (
	"context"
	"errors"
	"testing"
	"time"

	"health-nexus/internal/shared/rag"
)

// fakeJSONCompleter 固定返回预设响应或错误的 JSONCompleter。
type fakeJSONCompleter struct {
	raw string
	err error
}

func (f *fakeJSONCompleter) CompleteJSON(
	_ context.Context, _, _ string, _ time.Duration,
) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.raw, nil
}

func TestExtractJSONObject(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		wantOK bool
	}{
		{name: "纯JSON", raw: `{"intent":"patient_education"}`, wantOK: true},
		{name: "带代码围栏", raw: "```json\n{\"intent\":\"other\"}\n```", wantOK: true},
		{name: "前后带说明", raw: "好的，结果如下：{\"intent\":\"other\"} 完毕", wantOK: true},
		{name: "无对象", raw: "抱歉我不能回答", wantOK: false},
		{name: "空字符串", raw: "", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractJSONObject(tc.raw)
			if tc.wantOK && err != nil {
				t.Fatalf("期望解析成功，实际 %v", err)
			}
			if !tc.wantOK && err == nil {
				t.Fatalf("期望解析失败，实际得到 %s", got)
			}
		})
	}
}

// TestLLMAssessor_ParsesStructuredOutput 合法 JSON → 结构化审查结果。
func TestLLMAssessor_ParsesStructuredOutput(t *testing.T) {
	raw := `{
	  "intent": "medication_change",
	  "emergency_risk": "not_detected",
	  "self_harm_risk": "not_detected",
	  "medical_abuse": false,
	  "prompt_injection": false,
	  "individualized_diagnosis": false,
	  "medication_change_request": true,
	  "context_sufficient": true,
	  "missing_critical_information": [],
	  "standalone_query": "高血压患者能否自行加倍降压药剂量",
	  "must_preserve_constraints": ["自行加倍"],
	  "clarification_question": null,
	  "risk_evidence": [],
	  "recommended_action": "restricted"
	}`
	a := NewLLMAssessor(&fakeJSONCompleter{raw: raw})
	got, err := a.AssessAndRewrite(context.Background(), "我能把降压药加倍吃吗", nil)
	if err != nil {
		t.Fatalf("期望解析成功，实际 %v", err)
	}
	if got.Action() != rag.ActionRestricted {
		t.Errorf("Action() = %q, want %q", got.Action(), rag.ActionRestricted)
	}
	if got.StandaloneQuery != "高血压患者能否自行加倍降压药剂量" {
		t.Errorf("StandaloneQuery = %q", got.StandaloneQuery)
	}
}

// TestLLMAssessor_FailuresDegradeToInvalid 调用失败 / 非 JSON / 字段非法
// 都必须返回 ErrAssessmentInvalid（上层据此按"无法判断"降级，绝不视为安全）。
func TestLLMAssessor_FailuresDegradeToInvalid(t *testing.T) {
	cases := []struct {
		name    string
		clients []*fakeJSONCompleter
	}{
		{name: "调用失败", clients: []*fakeJSONCompleter{{err: errors.New("timeout")}}},
		{name: "输出非JSON", clients: []*fakeJSONCompleter{{raw: "抱歉，我无法判断"}}},
		{name: "字段缺失导致枚举非法", clients: []*fakeJSONCompleter{{raw: `{"intent":"patient_education"}`}}},
		{name: "JSON语法错误", clients: []*fakeJSONCompleter{{raw: `{"intent":`}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := NewLLMAssessor(tc.clients[0])
			_, err := a.AssessAndRewrite(context.Background(), "任意输入", nil)
			if err == nil {
				t.Fatal("期望返回错误")
			}
			if !errors.Is(err, rag.ErrAssessmentInvalid) {
				t.Errorf("错误应包装 ErrAssessmentInvalid，实际 %v", err)
			}
		})
	}
}

// TestLLMOutputReviewer_PassAndFail 语义审核通过/不通过判定。
func TestLLMOutputReviewer_PassAndFail(t *testing.T) {
	pass := NewLLMOutputReviewer(&fakeJSONCompleter{
		raw: `{"boundary_ok":true,"evidence_supported":true,"reason":"均有资料支持","revised_answer":""}`,
	})
	got, err := pass.ReviewAnswer(context.Background(), "问题", "答案", []string{"资料"})
	if err != nil {
		t.Fatalf("期望成功，实际 %v", err)
	}
	if !got.Pass() {
		t.Error("两项均为 true 时应通过")
	}

	fail := NewLLMOutputReviewer(&fakeJSONCompleter{
		raw: `{"boundary_ok":false,"evidence_supported":true,"reason":"给出个体化剂量","revised_answer":"请遵医嘱"}`,
	})
	got, err = fail.ReviewAnswer(context.Background(), "问题", "把药量翻倍", []string{"资料"})
	if err != nil {
		t.Fatalf("期望成功，实际 %v", err)
	}
	if got.Pass() {
		t.Error("boundary_ok=false 时不应通过")
	}
	if got.RevisedAnswer != "请遵医嘱" {
		t.Errorf("RevisedAnswer = %q", got.RevisedAnswer)
	}
}

// TestLLMOutputReviewer_Unavailable 审核不可用时返回 ErrOutputReviewUnavailable，
// 调用方据此不展示未经审核的答案。
func TestLLMOutputReviewer_Unavailable(t *testing.T) {
	r := NewLLMOutputReviewer(&fakeJSONCompleter{err: errors.New("timeout")})
	_, err := r.ReviewAnswer(context.Background(), "问题", "答案", nil)
	if !errors.Is(err, rag.ErrOutputReviewUnavailable) {
		t.Errorf("错误应包装 ErrOutputReviewUnavailable，实际 %v", err)
	}
}
