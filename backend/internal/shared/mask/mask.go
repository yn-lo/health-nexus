// Package mask 提供 PII 脱敏工具（AC-SEC-04）。
package mask

import "strings"

const (
	apiKeyMinLen = 8
)

// APIKey 密钥脱敏：sk-****xxxx
func APIKey(s string) string {
	if len(s) < apiKeyMinLen {
		return strings.Repeat("*", len(s))
	}
	return s[:3] + "****" + s[len(s)-4:]
}
