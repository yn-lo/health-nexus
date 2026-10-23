package identity

import (
	"net/http"

	"health-nexus/internal/shared/contextkeys"
	apperrors "health-nexus/internal/shared/errors"
)

// Actor 已认证操作者上下文（handler 解析后传给 service）。
// 各域直接复用此类型，不再各自定义同形结构。
type Actor struct {
	UserID int64
	Role   string
	DeptID int64
}

// ActorFromRequest 从 JWTAuth 写入的 context 提取操作者身份。
// 受保护端点必经 JWTAuth，缺失 user_id / role 视为未认证（401）。
func ActorFromRequest(r *http.Request) (Actor, error) {
	ctx := r.Context()
	uid, _ := ctx.Value(contextkeys.UserID).(int64)
	if uid <= 0 {
		return Actor{}, apperrors.Unauthorized("UNAUTHORIZED", "missing user identity")
	}
	role, _ := ctx.Value(contextkeys.UserRole).(string)
	if role == "" {
		return Actor{}, apperrors.Unauthorized("UNAUTHORIZED", "missing user role")
	}
	deptID, _ := ctx.Value(contextkeys.DeptID).(int64) // omitempty，PATIENT/部分账号可能为 0
	return Actor{UserID: uid, Role: role, DeptID: deptID}, nil
}
