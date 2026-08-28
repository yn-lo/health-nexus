// Package identity 定义请求身份的共享值对象与从 context 解析的唯一入口。
// 身份事实来源由 middleware 写入 contextkeys（jwt_auth 写 UserID，device_id 写 DeviceID）；
// 本包是唯一把「用户 or 设备」收敛成 Anon()/Subject() 语义的读取入口，
// 供 chat 流式链路（会话/锁 key）与限流 key 共用，避免各处裸拆 context 重复判匿名。
package identity

import (
	"fmt"
	"net/http"

	"health-nexus/internal/shared/contextkeys"
)

// Identity 一次请求的身份载体：认证用户或匿名设备。
type Identity struct {
	UserID   int64  // 认证用户 id；0 表示匿名
	DeviceID string // 匿名设备标识（UserID<=0 时有效）
}

// Anon 是否匿名：无有效 user id 即匿名。
func (i Identity) Anon() bool { return i.UserID <= 0 }

// IsValid 身份是否可接受：认证必携 user，匿名必携 device。
func (i Identity) IsValid() bool { return !i.Anon() || i.DeviceID != "" }

// Subject 身份标识（供锁/限流 key、日志）：匿名前缀 anon，认证前缀 user。
func (i Identity) Subject() string {
	if i.Anon() {
		return "anon:" + i.DeviceID
	}
	return fmt.Sprintf("user:%d", i.UserID)
}

// FromRequestOrZero 从请求 context 解析身份：先 user 后 device，两者皆无时返回零值，
// 不返回错误（供「既无身份又需 IP 兜底」的限流场景）。对身份有硬性要求的调用方自行判 IsValid。
func FromRequestOrZero(r *http.Request) Identity {
	var id Identity
	if uid, ok := r.Context().Value(contextkeys.UserID).(int64); ok && uid > 0 {
		id.UserID = uid
		return id
	}
	// DeviceID 缺失时类型断言得到空串，Id 为匿名且无设备（即 IP 兜底场景）。
	id.DeviceID, _ = r.Context().Value(contextkeys.DeviceID).(string)
	return id
}