package entity

import "time"

// safety_rules 表 action 取值。
const (
	SafetyActionReplace = "replace"
	SafetyActionBlock   = "block"
)

// SafetyRule 危险输出模式实体。IsActive 替代 IsEnabled（与 spec §6.3 对齐）。
type SafetyRule struct {
	ID          int64
	Name        string
	Category    string // diagnosis|prescription|stop_medication|delay_medical|other
	Pattern     string
	Action      string // replace|block
	Replacement string // action=replace 时的替换话术
	IsActive    bool
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
