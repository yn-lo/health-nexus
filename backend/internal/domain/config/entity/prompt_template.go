package entity

import "time"

// PromptTemplate 对应 prompt_templates 表，多版本（is_active 控制生效版本）。
//
// 全院唯一，无科室维度：系统提示词的科室差异由知识库内容承载。
// Content 只承载可配置的「人格/口吻」部分——医疗红线、引用规则、格式规范由
// constants.SystemPromptGuardrails 代码独占并在下发前追加，不存于本表。
type PromptTemplate struct {
	ID          int64
	Type        string
	Version     int
	Content     string
	IsActive    bool
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
