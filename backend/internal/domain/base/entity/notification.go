package entity

import "time"

// Notification 站内通知聚合根，对应 notifications 表。
// RecipientDeptID 为 nil 表示面向该角色的全部科室广播；RefID 关联触发实体（危机事件/文章 ID）。
type Notification struct {
	ID              int64
	RecipientRole   string
	RecipientDeptID *int64
	Type            string
	Title           string
	Body            string
	RefID           *string
	IsRead          bool
	CreatedAt       time.Time
}
