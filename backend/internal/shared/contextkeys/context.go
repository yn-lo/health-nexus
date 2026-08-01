// Package contextkeys 定义 context.Context 的键类型，避免循环依赖。
// middleware 写入，response/shared 读取。
package contextkeys

import "context"

// Key 类型化 context key，避免字符串键冲突。
type Key string

const (
	RequestID Key = "request_id"
	UserID    Key = "user_id"
	UserRole  Key = "user_role"
	DeptID    Key = "dept_id"
	// DeviceID 匿名用户设备标识（X-Device-Id 头），未登录时用于限流 key 与会话隔离。
	DeviceID Key = "device_id"
	// DataScopeKey DataIsolation 中间件写入的 DataScope 指针键。
	// service 层通过 middleware.ActorFromDataScope 读取。
	DataScopeKey Key = "data_scope"
)

// FromCtx 从 context 读取字符串值。
func FromCtx(ctx context.Context, k Key) string {
	if v, ok := ctx.Value(k).(string); ok {
		return v
	}
	return ""
}
