package entity

import (
	"time"

	"github.com/google/uuid"
)

// CrisisEvent 危机事件实体，对应 crisis_events 表。
// 由规则层安全审查命中危机关键词时同步创建（REQ-CHAT-008/015）。
type CrisisEvent struct {
	ID               int64
	PatientID        int64
	ConversationID   uuid.UUID
	MessageID        *uuid.UUID // 可空：用户消息持久化失败时为 nil
	TriggeredContent string
	MatchedKeywords  []string
	Level            string // constants.CrisisLevel*
	IsHandled        bool
	HandlerID        *int64
	HandledAt        *time.Time
	HandleNote       string
	CreatedAt        time.Time

	// LockedDeptID 所属科室（派生字段，非表字段）：JOIN conversations.locked_dept_id 取得。
	// 未锁定科室的会话为 0。供 service 层做危机事件处理的科室归属校验。
	LockedDeptID int64
}

// PatientName 仅用于列表/详情响应，非表字段（JOIN users 取得）。
// 放在此处方便 DTO 转换与 repository 一并扫描。
type PatientName struct {
	ID   int64
	Name string
}
