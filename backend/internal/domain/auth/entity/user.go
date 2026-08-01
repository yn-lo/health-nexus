// Package entity 定义 auth 域的实体与值对象。
package entity

import "time"

// User 用户实体，对应 users 表。
// PrimaryDeptID 不在 users 表，由 user_departments.is_primary=true 的行 JOIN 而来；无则 0。
type User struct {
	ID               int64
	Username         string
	Role             string
	PasswordHash     string
	Phone            string
	DateOfBirth      *time.Time
	Gender           string
	EmergencyContact string
	EmergencyPhone   string
	IsActive         bool
	IsDeleted        bool
	PrimaryDeptID    int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// IsLocked 账户是否被禁用：is_active=false 表示管理员手动禁用。
// ponytail: 已删除 login_fail_count/locked_until 死字段（见 migration 00017），简化。
// 自动账户锁定由限流中间件兜底——重复暴力破解在 IP 维度已被限流，
// 应用层再做 N 次失败锁定是重复防御且实现复杂度高于收益。
// 升级路径：若需 per-account 锁定，重新加 locked_until 列 + repo.LockAccount(ctx, id, dur)。
func (u *User) IsLocked() bool {
	return !u.IsActive
}
