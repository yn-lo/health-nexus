// 统一理解与审查协议单元测试：动作优先级（后端固定规则）与字段校验。
package rag

import (
	"errors"
	"testing"
)

// TestAssessmentAction_Priority 同一句可能同时命中多个分类，后端必须按固定优先级决定动作，
// 不能因模型只选了一个主意图而忽略其他风险。
func TestAssessmentAction_Priority(t *testing.T) {
	cases := []struct {
		name string
		in   Assessment
		want string
	}{
		{
			name: "自伤优先于急症与用药",
			in: Assessment{
				Intent: IntentMedicationChange, SelfHarmRisk: RiskSuspected, EmergencyRisk: RiskConfirmed,
				MedicationChange: true, ContextSufficient: true, StandaloneQuery: "q",
			},
			want: ActionCrisis,
		},
		{
			name: "急症优先于用药调整",
			in: Assessment{
				Intent: IntentMedicationChange, EmergencyRisk: RiskSuspected, MedicationChange: true,
				ContextSufficient: true, StandaloneQuery: "q",
			},
			want: ActionEmergency,
		},
		{
			name: "注入优先于个体化诊疗",
			in: Assessment{
				Intent: IntentIndividualized, PromptInjection: true, IndividualizedDx: true,
				ContextSufficient: true, StandaloneQuery: "q",
			},
			want: ActionReject,
		},
		{
			name: "医疗滥用优先于个体化诊疗",
			in: Assessment{
				Intent: IntentIndividualized, MedicalAbuse: true, IndividualizedDx: true,
				ContextSufficient: true, StandaloneQuery: "q",
			},
			want: ActionReject,
		},
		{
			name: "个体化诊疗请求走受限流程",
			in: Assessment{
				Intent: IntentIndividualized, EmergencyRisk: RiskNotDetected, SelfHarmRisk: RiskNotDetected,
				IndividualizedDx: true, ContextSufficient: true, StandaloneQuery: "q",
			},
			want: ActionRestricted,
		},
		{
			name: "用药调整请求走受限流程",
			in: Assessment{
				Intent: IntentPatientEducation, EmergencyRisk: RiskNotDetected, SelfHarmRisk: RiskNotDetected,
				MedicationChange: true, ContextSufficient: true, StandaloneQuery: "q",
			},
			want: ActionRestricted,
		},
		{
			name: "信息不足先澄清",
			in: Assessment{
				Intent: IntentPatientEducation, EmergencyRisk: RiskNotDetected, SelfHarmRisk: RiskNotDetected,
				ContextSufficient: false, MissingCritical: []string{"年龄"}, ClarificationQuestion: "请问年龄？",
			},
			want: ActionClarify,
		},
		{
			name: "普通宣教走检索",
			in: Assessment{
				Intent: IntentPatientEducation, EmergencyRisk: RiskNotDetected, SelfHarmRisk: RiskNotDetected,
				ContextSufficient: true, StandaloneQuery: "高血压日常如何监测血压",
			},
			want: ActionRetrieve,
		},
		{
			name: "风险为 uncertain 时保守处理（按疑似急症）",
			in: Assessment{
				Intent: IntentPatientEducation, EmergencyRisk: RiskUncertain, SelfHarmRisk: RiskNotDetected,
				ContextSufficient: true, StandaloneQuery: "q",
			},
			want: ActionEmergency,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.Action(); got != tc.want {
				t.Errorf("Action() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAssessmentValidate 字段/枚举非法一律视为审查不可用（不得降级为"安全"）。
func TestAssessmentValidate(t *testing.T) {
	valid := Assessment{
		Intent: IntentPatientEducation, EmergencyRisk: RiskNotDetected, SelfHarmRisk: RiskNotDetected,
		ContextSufficient: true, StandaloneQuery: "高血压日常如何监测血压",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("合法结果不应报错: %v", err)
	}

	cases := []struct {
		name string
		in   Assessment
	}{
		{
			name: "意图枚举非法",
			in:   Assessment{Intent: "unknown", EmergencyRisk: RiskNotDetected, SelfHarmRisk: RiskNotDetected, ContextSufficient: true, StandaloneQuery: "q"},
		},
		{
			name: "风险状态枚举非法（缺字段按空值处理）",
			in:   Assessment{Intent: IntentPatientEducation, EmergencyRisk: RiskNotDetected, SelfHarmRisk: "", ContextSufficient: true, StandaloneQuery: "q"},
		},
		{
			name: "检索动作缺少独立问题",
			in:   Assessment{Intent: IntentPatientEducation, EmergencyRisk: RiskNotDetected, SelfHarmRisk: RiskNotDetected, ContextSufficient: true},
		},
		{
			name: "澄清问题过长",
			in: Assessment{
				Intent: IntentPatientEducation, EmergencyRisk: RiskNotDetected, SelfHarmRisk: RiskNotDetected,
				ContextSufficient: false, ClarificationQuestion: string(make([]rune, assessmentMaxQuestionRune+1)),
			},
		},
		{
			name: "风险依据条数超限",
			in: Assessment{
				Intent: IntentPatientEducation, EmergencyRisk: RiskNotDetected, SelfHarmRisk: RiskNotDetected,
				ContextSufficient: true, StandaloneQuery: "q",
				RiskEvidence: make([]string, assessmentMaxEvidence+1),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.in.Validate()
			if err == nil {
				t.Fatal("期望校验失败")
			}
			if !errors.Is(err, ErrAssessmentInvalid) {
				t.Errorf("错误应包装 ErrAssessmentInvalid，实际 %v", err)
			}
		})
	}
}
