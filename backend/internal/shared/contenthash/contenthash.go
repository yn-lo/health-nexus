// Package contenthash 提供内容 SHA256 哈希计算（文章变更检测，REQ-WIKI-015）。
package contenthash

import (
	"crypto/sha256"
	"encoding/hex"
)

// SHA256 返回 content 的 SHA256 十六进制摘要。
func SHA256(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}
