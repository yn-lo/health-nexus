package entity

import "time"

// ArticleReference 跨科室引用授权，对应 article_references 表。
// 状态机：pending → approved / rejected / revoked（REQ-WIKI-021/022）。
// View 字段（ArticleTitle/SourceDeptName 等）由 JOIN 查询填充，仅用于读模型。
type ArticleReference struct {
	ID            int64
	ArticleID     int64
	SourceDeptID  int64
	TargetDeptID  int64
	Status        string
	ApplicantID   int64
	ReviewerID    *int64
	ReviewComment string
	ApprovedAt    *time.Time
	RevokedAt     *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time

	// View 字段（JOIN 填充，写操作忽略）
	ArticleTitle        string
	SourceDeptName      string
	TargetDeptName      string
	ApplicantName       string
	SourceArticleStatus string // 源文章当前状态（JOIN articles.status），用于前端变动提示
}
