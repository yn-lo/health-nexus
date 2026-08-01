package main

import (
	"fmt"

	"health-nexus/internal/config"
	"health-nexus/internal/platform/crypto"
)

const (
	hashpwArgon2Memory     = 65536
	hashpwArgon2Iterations = 3
	hashpwArgon2Parallel   = 2
	hashpwArgon2SaltLen    = 16
	hashpwArgon2KeyLen     = 32
)

func main() {
	cfg := config.Argon2Config{
		Memory: hashpwArgon2Memory, Iterations: hashpwArgon2Iterations,
		Parallelism: hashpwArgon2Parallel, SaltLength: hashpwArgon2SaltLen, KeyLength: hashpwArgon2KeyLen,
	}
	hash, err := crypto.HashPassword("Pass1234", cfg)
	if err != nil {
		panic(err)
	}
	fmt.Print(hash)
}
