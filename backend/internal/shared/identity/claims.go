package identity

import "github.com/golang-jwt/jwt/v5"

// Claims JWT 自定义声明，扩展标准 RegisteredClaims。
// 签发（auth 域）与校验（middleware）共用，避免任何一侧依赖另一层的类型。
type Claims struct {
	jwt.RegisteredClaims
	UserID    int64  `json:"user_id"`
	Role      string `json:"role"`
	DeptID    int64  `json:"dept_id,omitempty"`
	TokenType string `json:"token_type,omitempty"` // "access" 或 "refresh"；空表示历史 token
	// TokenVersion 会话版本（签发时的 users.token_version）。与当前值不一致即视为失效：
	// 密码变更后旧令牌不可再用（P1）。
	TokenVersion int64 `json:"ver,omitempty"`
}
