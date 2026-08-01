package entity

import "time"

// SafetyMessage 对应 safety_messages 表，按 type 区分
// （rejection/emergency/safety_warning/crisis_response）。
// 已废弃：crisis_hotline（合并到 crisis_response）/ medication_disclaimer（合并到 safety_warning）。
type SafetyMessage struct {
	ID        int64
	Type      string
	Content   string
	IsActive  bool
	UpdatedAt time.Time
}

// 安全话术 type 值（与 SQL CHECK 对齐）。
const (
	SafetyMessageTypeRejection      = "rejection"
	SafetyMessageTypeEmergency      = "emergency"
	SafetyMessageTypeSafetyWarning  = "safety_warning"
	SafetyMessageTypeCrisisResponse = "crisis_response"
	SafetyMessageTypeNoKnowledge    = "no_knowledge"
	SafetyMessageTypeSystemError    = "system_error"
)
