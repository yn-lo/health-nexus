// Package crypto 提供密码哈希（argon2id）与对称加密（AES-256-GCM）。
package crypto

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"

	"health-nexus/internal/config"
)

// argon2Version 是 argon2id 算法版本号（0x13 = 19）。
const argon2Version = 0x13

// argon2Format 是哈希字符串的编码格式。
// 形如：argon2id$v=19$m=65536,t=3,p=2$<base64(salt)>$<base64(hash)>
const argon2Format = "argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s"

// argon2PartCount 是哈希字符串按 "$" 切分后的段数（argon2id$v$params$salt$hash）。
const argon2PartCount = 5

// maxArgon2KeyLength 哈希输出的合理上界（字节）：校验损坏记录，并保证长度安全转换为 uint32。
const maxArgon2KeyLength = 1024

// 哈希相关错误。
var (
	ErrInvalidHashFormat  = errors.New("invalid argon2id hash format")
	ErrPasswordMismatch   = errors.New("password does not match")
	ErrUnsupportedVersion = errors.New("unsupported argon2 version")
)

// HashPassword 用 argon2id 哈希密码，返回标准编码字符串。
// 参数遵循 OWASP 2023 推荐（memory=64MB, iterations=3, parallelism=2）。
// 编码格式：argon2id$v=19$m=...,t=...,p=...$base64(salt)$base64(hash)
func HashPassword(password string, cfg config.Argon2Config) (string, error) {
	salt := make([]byte, cfg.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, cfg.Iterations, cfg.Memory, cfg.Parallelism, cfg.KeyLength)

	b64Salt := base64.StdEncoding.EncodeToString(salt)
	b64Hash := base64.StdEncoding.EncodeToString(hash)
	return fmt.Sprintf(argon2Format, argon2Version, cfg.Memory, cfg.Iterations, cfg.Parallelism, b64Salt, b64Hash), nil
}

// ComparePasswordAndPassword 比较哈希与明文密码。
// 解析哈希字符串得到 salt 与参数，重新计算 hash，使用 subtle.ConstantTimeCompare 防止时序攻击。
func ComparePasswordAndPassword(hashedPassword, password string) error {
	parts := strings.Split(hashedPassword, "$")
	if len(parts) != argon2PartCount {
		return ErrInvalidHashFormat
	}
	if parts[0] != "argon2id" {
		return ErrInvalidHashFormat
	}

	var version int
	if _, err := fmt.Sscanf(parts[1], "v=%d", &version); err != nil {
		return fmt.Errorf("parse argon2 version: %w", err)
	}
	if version != argon2Version {
		return fmt.Errorf("%w: %d", ErrUnsupportedVersion, version)
	}

	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[2], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return fmt.Errorf("parse argon2 params: %w", err)
	}

	salt, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return fmt.Errorf("decode salt: %w", err)
	}

	expectedHash, err := base64.StdEncoding.DecodeString(parts[4])
	if err != nil {
		return fmt.Errorf("decode hash: %w", err)
	}
	// R8-Auth-1: 防御空 hash 匹配任意密码——subtle.ConstantTimeCompare([]byte{}, []byte{}) 返回 1，
	// 若 DB 中存有 hash 部分为空的损坏记录（如迁移残留/手工篡改），任意密码都会通过校验。
	// 正常路径下 HashPassword 始终生成非空 hash（argon2.KeyLength≥16），此分支仅作防御。
	// 上界校验同时保证 keyLen 可安全转换为 uint32（防整数溢出）。
	keyLen := len(expectedHash)
	if keyLen == 0 || keyLen > maxArgon2KeyLength {
		return ErrInvalidHashFormat
	}

	computedHash := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(keyLen))

	if subtle.ConstantTimeCompare(expectedHash, computedHash) != 1 {
		return ErrPasswordMismatch
	}
	return nil
}
