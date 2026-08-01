package entity

import "time"

// PromptTemplate 对应 prompt_templates 表，多类型 + 多版本（is_active 控制生效版本）。
// DepartmentID 为 nil 表示全局默认模板；非 nil 表示科室级定制（契约 §6.6）。
type PromptTemplate struct {
	ID           int64
	Type         string
	Version      int
	Content      string
	IsActive     bool
	Description  string
	DepartmentID *int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
