package entity

import "time"

// ArticleAuditLog 文章操作审计日志，对应 article_audit_logs 表。
// 不可变：Repository 层仅提供 Create，无 Update/Delete（AC-SEC-06，REQ-WIKI-002）。
type ArticleAuditLog struct {
	ID         int64
	ArticleID  int64
	OperatorID int64
	Action     string
	FromStatus string
	ToStatus   string
	Summary    string
	Reason     string
	CreatedAt  time.Time
}
