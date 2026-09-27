package main

import (
	"fmt"
	"os"

	"health-nexus/internal/config"
	"health-nexus/internal/platform/crypto"
)

const (
	hashpwArgon2Memory     = 65536
	hashpwArgon2Iterations = 3
	hashpwArgon2Parallel   = 2
	hashpwArgon2SaltLen    = 16
	hashpwArgon2KeyLen     = 32

	// defaultPassword 未提供命令行参数时使用的默认密码（与历史行为保持一致）。
	defaultPassword = "Pass1234"
)

// main 生成 argon2id 密码哈希。用法：go run ./cmd/hashpw [password]
// 未传参数时使用 defaultPassword，便于兼容既有流程。
func main() {
	password := defaultPassword
	if len(os.Args) > 1 {
		password = os.Args[1]
	}
	cfg := config.Argon2Config{
		Memory: hashpwArgon2Memory, Iterations: hashpwArgon2Iterations,
		Parallelism: hashpwArgon2Parallel, SaltLength: hashpwArgon2SaltLen, KeyLength: hashpwArgon2KeyLen,
	}
	hash, err := crypto.HashPassword(password, cfg)
	if err != nil {
		panic(err)
	}
	fmt.Print(hash)
}
