// Package entity 定义 wiki 域的聚合根与值对象。
package entity

import "time"

// Article 文章聚合根，对应 articles 表。
// 状态机：draft → pending → published → archived → deleted（软删除 is_deleted=true，REQ-WIKI-001）。
// View 字段（DepartmentName/AuthorName）由 JOIN 查询填充，仅用于读模型，不写入表。
type Article struct {
	ID              int64
	Title           string
	Content         string
	Summary         string
	CoverImageURL   string
	Status          string
	Version         int
	ContentHash     string
	AuthorID        int64
	DepartmentID    *int64
	ReviewerID      *int64
	ReviewComment   string
	ViewCount       int64
	FeaturedRank    int
	IsDeleted       bool
	AllowReference  bool
	ReviewOverdue   bool
	ReviewOverdueAt *time.Time
	PublishedAt     *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time

	// View 字段（JOIN 填充，写操作忽略）
	DepartmentName string
	AuthorName     string
}

// 审计动作常量（article_audit_logs.action），避免魔法值。
// article_audit_logs.action 列无 CHECK 约束（见 00001_init.sql），新增动作无需迁移。
const (
	AuditActionCreate    = "create"
	AuditActionUpdate    = "update"
	AuditActionSubmit    = "submit"
	AuditActionPublish   = "publish"
	AuditActionReject    = "reject"
	AuditActionDelete    = "delete"
	AuditActionArchive   = "archive"
	AuditActionUnarchive = "unarchive"
	AuditActionFeature   = "feature"
	// 引用授权操作（REQ-WIKI-002，D-HIGH-05）。
	AuditActionReferenceApply   = "reference_apply"
	AuditActionReferenceApprove = "reference_approve"
	AuditActionReferenceReject  = "reference_reject"
	AuditActionReferenceRevoke  = "reference_revoke"
)
