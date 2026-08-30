// Package entity 定义 auth 域的实体与值对象。
package entity

import (
	"errors"
	"time"
)

// ErrInviteInvalid 邀请码不可用（不存在/已过期/已被使用/角色不符）。
// 对外统一消息由 service 层映射，避免区分细节被探测有效码。定义于 entity 包，
// 使 repository（返回方）与 service（判定方）皆可引用而不产生 import 环。
var ErrInviteInvalid = errors.New("invite code invalid, expired, or used")

// InviteCode 邀请码实体，对应 invite_codes 表。
// UsedBy/UsedAt 同为空或同非空（由 CHECK 约束保证），二者非空表示已消费（一次性）。
type InviteCode struct {
	ID        int64
	Code      string
	Role      string
	CreatedBy int64
	UsedBy    *int64
	UsedAt    *time.Time
	ExpiresAt time.Time
	CreatedAt time.Time
}
