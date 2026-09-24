// Package eval 临床评测框架：用科室标注的样本集对"安全关键链路"跑批打分。
//
// 评测对象（均为 LLM 驱动的纯适配器，不依赖 DB/Redis，仅需一个可用的 chat provider）：
//   - 决策层 rag.Assessor（统一理解与审查）：高风险漏判率 / 误拦率 / 动作准确率；
//   - 输出审核层 rag.OutputReviewer（生成后语义审核）：证据支持率。
//
// 样本格式见 samples/clinical.yaml，字段语义见本文件 Sample 类型。
// 样本由科室维护：把临床真实（脱敏）问题与期望处置填进 YAML 即可，无需改代码。
//
// 运行（需真实 LLM 凭据，见 run_test.go 的包内说明）：
//
//	go test -tags eval ./tests/eval/... -v -count=1
//
// 不带 -tags eval 时仅做语料与打分逻辑的自检（不调用外部服务，随常规门禁运行）。
package eval

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"health-nexus/internal/shared/rag"
)

// 样本类别。类别决定期望动作（见 categoryExpectAction），避免样本内自相矛盾。
const (
	CategoryCrisis     = "crisis"     // 自伤风险 → 危机流程
	CategoryEmergency  = "emergency"  // 疑似急症 → 固定急救指引
	CategoryRestricted = "restricted" // 个体化诊疗 / 用药调整 → 固定边界说明
	CategoryReject     = "reject"     // 提示词注入 / 医疗滥用 → 拒答
	CategoryClarify    = "clarify"    // 关键信息不足 → 先澄清
	CategoryEducation  = "education"  // 普通宣教 → 检索 + 生成
	CategoryEvidence   = "evidence"   // 输出审核：答案是否有资料支持（评测 OutputReviewer）
)

// categoryExpectAction 类别 → 期望动作。CategoryEvidence 不评测动作，故不在表中。
var categoryExpectAction = map[string]string{
	CategoryCrisis:     rag.ActionCrisis,
	CategoryEmergency:  rag.ActionEmergency,
	CategoryRestricted: rag.ActionRestricted,
	CategoryReject:     rag.ActionReject,
	CategoryClarify:    rag.ActionClarify,
	CategoryEducation:  rag.ActionRetrieve,
}

// 对话角色（与后端 messages.role 取值一致）。
const (
	roleUser      = "user"
	roleAssistant = "assistant"
)

// Turn 样本的历史轮次（可空）。
type Turn struct {
	Role    string `yaml:"role"`
	Content string `yaml:"content"`
}

// Sample 一条评测样本。
//
// 动作类样本（crisis/emergency/restricted/reject/clarify/education）：只填 message（+可选 history），
// 期望动作由 category 推导。
//
// 证据类样本（evidence）：填 message + evidence（知识库资料）+ draft_answer（待审答案）
// + expect_evidence_supported（临床标注该答案是否有资料支持）。
type Sample struct {
	ID                      string   `yaml:"id"`
	Category                string   `yaml:"category"`
	Message                 string   `yaml:"message"`
	History                 []Turn   `yaml:"history,omitempty"`
	Evidence                []string `yaml:"evidence,omitempty"`
	DraftAnswer             string   `yaml:"draft_answer,omitempty"`
	ExpectEvidenceSupported *bool    `yaml:"expect_evidence_supported,omitempty"`
	Note                    string   `yaml:"note,omitempty"`
}

// ExpectAction 期望的系统动作；证据类样本返回空串（不评测动作）。
func (s Sample) ExpectAction() string { return categoryExpectAction[s.Category] }

// corpus 样本集文件结构。
type corpus struct {
	Version int      `yaml:"version"`
	Samples []Sample `yaml:"samples"`
}

// LoadCorpus 读取并校验样本集。任一样本非法即整体报错（语料缺陷早暴露，不带病跑批）。
func LoadCorpus(path string) ([]Sample, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read corpus: %w", err)
	}
	var c corpus
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse corpus: %w", err)
	}
	if len(c.Samples) == 0 {
		return nil, errors.New("corpus: 样本集为空")
	}
	seen := make(map[string]bool, len(c.Samples))
	for i, s := range c.Samples {
		if verr := s.validate(); verr != nil {
			return nil, fmt.Errorf("corpus: 第 %d 条样本(%s)非法: %w", i+1, s.ID, verr)
		}
		if seen[s.ID] {
			return nil, fmt.Errorf("corpus: 样本 id 重复: %s", s.ID)
		}
		seen[s.ID] = true
	}
	return c.Samples, nil
}

// validate 校验单条样本的必填字段与类别一致性。
func (s Sample) validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return errors.New("id 不能为空")
	}
	if strings.TrimSpace(s.Message) == "" {
		return errors.New("message 不能为空")
	}
	for _, h := range s.History {
		if h.Role != roleUser && h.Role != roleAssistant {
			return fmt.Errorf("history.role 非法: %q（应为 %s/%s）", h.Role, roleUser, roleAssistant)
		}
	}
	if s.Category == CategoryEvidence {
		return s.validateEvidence()
	}
	if _, ok := categoryExpectAction[s.Category]; !ok {
		return fmt.Errorf("category 非法: %q", s.Category)
	}
	if s.ExpectEvidenceSupported != nil {
		return errors.New("expect_evidence_supported 仅用于 evidence 类样本")
	}
	return nil
}

// validateEvidence 校验证据类样本：必须给出资料、待审答案与临床标注。
func (s Sample) validateEvidence() error {
	if len(s.Evidence) == 0 {
		return errors.New("evidence 类样本必须提供 evidence（知识库资料）")
	}
	if strings.TrimSpace(s.DraftAnswer) == "" {
		return errors.New("evidence 类样本必须提供 draft_answer（待审答案）")
	}
	if s.ExpectEvidenceSupported == nil {
		return errors.New("evidence 类样本必须提供 expect_evidence_supported（临床标注）")
	}
	return nil
}

// corpusSamplePath 示例样本集路径（相对包目录）。
const corpusSamplePath = "samples/clinical.yaml"

// TestClinicalCorpusIsValid 校验随仓库提供的示例样本集：可解析、字段合法、覆盖全部类别。
// 不调用外部服务，随常规门禁运行——保证科室替换样本时不会引入结构性错误。
func TestClinicalCorpusIsValid(t *testing.T) {
	samples, err := LoadCorpus(corpusSamplePath)
	if err != nil {
		t.Fatalf("加载示例样本集失败: %v", err)
	}

	counts := make(map[string]int, len(categoryExpectAction)+1)
	for _, s := range samples {
		counts[s.Category]++
	}
	// 覆盖度：每类至少一条，否则该维度的指标没有分母（形同未评测）。
	required := []string{
		CategoryCrisis, CategoryEmergency, CategoryRestricted,
		CategoryReject, CategoryClarify, CategoryEducation, CategoryEvidence,
	}
	for _, c := range required {
		if counts[c] == 0 {
			t.Errorf("示例样本集缺少类别 %q 的样本", c)
		}
	}
	t.Logf("示例样本集：共 %d 条，分布 %v", len(samples), counts)
}
