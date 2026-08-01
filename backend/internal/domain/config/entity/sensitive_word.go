package entity

import "time"

// SensitiveWord 对应 sensitive_words 表，按类别维护（suicide/emergency/injection）。
type SensitiveWord struct {
	ID        int64
	Word      string
	Category  string
	IsActive  bool
	CreatedAt time.Time
}
