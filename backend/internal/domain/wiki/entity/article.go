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

	// 知识条目元数据（P1）：来源 / 适用人群 / 有效期 / 内容风险等级。
	// ContentRisk 决定逾期策略与检索可见性：high 的资料有效期更短，
	// 且"高风险 + 已逾期"不再参与检索（见 search_service / chunk_repo）。
	Source               string
	ApplicablePopulation string
	ValidUntil           *time.Time
	ContentRisk          string

	// View 字段（JOIN 填充，写操作忽略）
	DepartmentName string
	AuthorName     string
}

// 内容风险等级（articles.content_risk）。
const (
	// ContentRiskNormal 普通宣教内容：逾期 180 天后标记待复审。
	ContentRiskNormal = "normal"
	// ContentRiskHigh 用药/检查准备/高风险护理内容：逾期 90 天即标记，且逾期后退出检索。
	ContentRiskHigh = "high"
)

// IsContentRiskValid 校验内容风险等级取值。
func IsContentRiskValid(v string) bool {
	return v == ContentRiskNormal || v == ContentRiskHigh
}

// 审计动作常量（article_audit_logs.action），避免魔法值。
// article_audit_logs.action 列无 CHECK 约束（见 schema.sql），新增动作无需迁移。
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
