// Package entity 定义 base 域的聚合根与值对象。
package entity

import "time"

// Department 科室聚合根，对应 departments 表。
// 树形层级结构通过 ParentID 实现（REQ-BASE-001）；
// 启用/禁用通过 IsActive 控制，禁用后该科室不可见（REQ-BASE-002）。
type Department struct {
	ID          int64
	Name        string
	ParentID    *int64 // 父科室 ID，根科室为 nil
	IsPublic    bool   // 公共科室：文章对其子科室可见（REQ-WIKI-020）
	IsActive    bool   // 启用/禁用，禁用后不可见（REQ-BASE-002）
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
