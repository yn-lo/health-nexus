// Package entity 定义 chat 域的聚合根与值对象，与 DB 表一一对应。
package entity

import (
	"time"

	"github.com/google/uuid"
)

// Conversation 会话聚合根，对应 conversations 表。
// 一旦选定科室（LockedDeptID 非 nil）后不可更改（REQ-CHAT-019）。
type Conversation struct {
	ID            uuid.UUID
	PatientID     int64
	LockedDeptID  *int64 // nil 表示未锁定
	Title         string
	IsArchived    bool
	LastMessageAt time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// TitleMaxLen 会话标题最大长度（REQ-CHAT-018：首条消息前 20 字截断）。
const TitleMaxLen = 20

// HasLockedDept 是否已锁定科室。
func (c *Conversation) HasLockedDept() bool { return c.LockedDeptID != nil }
