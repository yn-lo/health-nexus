// Package mask 提供 PII 脱敏工具（AC-SEC-04）。
package mask

import "strings"

const (
	phoneMinLen   = 7
	idCardMinLen  = 10
	idCardMaskLen = 8
	apiKeyMinLen  = 8
)

// Phone 手机号脱敏：138****1234
func Phone(s string) string {
	if len(s) < phoneMinLen {
		return strings.Repeat("*", len(s))
	}
	return s[:3] + "****" + s[len(s)-4:]
}

// IDCard 身份证号脱敏：110101********1234
func IDCard(s string) string {
	if len(s) < idCardMinLen {
		return strings.Repeat("*", len(s))
	}
	return s[:6] + strings.Repeat("*", idCardMaskLen) + s[len(s)-4:]
}

// APIKey 密钥脱敏：sk-****xxxx
func APIKey(s string) string {
	if len(s) < apiKeyMinLen {
		return strings.Repeat("*", len(s))
	}
	return s[:3] + "****" + s[len(s)-4:]
}

// Email 邮箱脱敏：z***@example.com
func Email(s string) string {
	at := strings.IndexByte(s, '@')
	if at < 1 {
		return strings.Repeat("*", len(s))
	}
	name := s[:at]
	domain := s[at:]
	if len(name) <= 1 {
		return name + "***" + domain
	}
	return string(name[0]) + "***" + domain
}
