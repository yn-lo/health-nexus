package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// AES-256 密钥长度（字节）。
const aesKeyLen = 32

// AES-GCM 标准 nonce 长度（字节）。
const aesGCMNonceLen = 12

// 加密相关错误。
var (
	ErrInvalidKey        = errors.New("aes key must be 32 bytes")
	ErrInvalidCiphertext = errors.New("invalid ciphertext")
)

// Encrypt 用 AES-256-GCM 加密明文（REQ-CONFIG-002，API Key 加密存储）。
// 返回 base64(nonce + ciphertext)。key 必须是 32 字节（AES-256）。
func Encrypt(plaintext string, key []byte) (string, error) {
	if len(key) != aesKeyLen {
		return "", ErrInvalidKey
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create aes cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	nonce := make([]byte, aesGCMNonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	// Seal 把 nonce 作为 dst 前缀，结果为 nonce || ciphertext+tag
	combined := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(combined), nil
}

// Decrypt 解密 Encrypt 返回的 base64(nonce + ciphertext) 密文。
// key 必须是 32 字节（AES-256）。
func Decrypt(ciphertext string, key []byte) (string, error) {
	if len(key) != aesKeyLen {
		return "", ErrInvalidKey
	}

	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}

	if len(raw) < aesGCMNonceLen {
		return "", ErrInvalidCiphertext
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create aes cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	nonce := raw[:aesGCMNonceLen]
	ct := raw[aesGCMNonceLen:]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plaintext), nil
}
