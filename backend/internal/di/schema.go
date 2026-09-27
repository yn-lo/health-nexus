// Package di 提供依赖注入与启动引导。
package di

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // 注册 pgx database/sql 驱动

	"health-nexus/internal/config"
	"health-nexus/internal/platform/crypto"
)

const (
	// migrationAdvisoryLock 避免多实例并发执行 schema 应用的 PG advisory lock（随机 magic number）。
	migrationAdvisoryLock = 0x484E4D47 // "HNMG" = Health Nexus MiGration
	// migrationLockTimeout 获取 advisory lock 的超时。
	migrationLockTimeout = 30 * time.Second
	// migrationPollInterval 轮询 advisory lock 的间隔。
	migrationPollInterval = 500 * time.Millisecond
)

//go:embed schema.sql
var schemaSQL string

//go:embed seed.sql
var seedSQL string

//go:embed seed.dev.sql
var seedDevSQL string

// adminPasswordPlaceholder seed.sql 中超管密码哈希的占位符，
// 由 ApplySchema 用运行时计算的 argon2id 哈希替换（明文密码经 HEALTH_NEXUS_SEED_ADMIN_PASSWORD 注入，不入库文件）。
const adminPasswordPlaceholder = "{{ADMIN_PASSWORD_HASH}}"

// seedFile 一个待应用的种子 SQL 文件。
type seedFile struct {
	name string
	sql  string
}

// ApplySchema 在启动时按"内容哈希"幂等应用数据库结构与种子数据：
//   - schema.sql：结构（始终应用）；
//   - seed.sql：内置超管（始终应用，密码为空时跳过）；
//   - seed.dev.sql：测试数据（仅 env=development 应用，生产保持数据库干净）。
//
// 每个文件用内容哈希判断是否重跑，全部写成幂等形式（IF NOT EXISTS / ON CONFLICT / WHERE NOT EXISTS），
// 重跑对已有对象安全。使用 PG advisory lock 防止 server + worker 并发同时应用。
//
// env 为运行环境（config.EnvDevelopment 时加载测试数据）；adminPassword 为空则不创建内置超管账号。
func ApplySchema(ctx context.Context, dsn, env, adminPassword string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open db for schema apply: %w", err)
	}
	defer func() { _ = db.Close() }()

	lockCtx, cancel := context.WithTimeout(ctx, migrationLockTimeout)
	defer cancel()
	if err := acquireAdvisoryLock(lockCtx, db); err != nil {
		return fmt.Errorf("acquire schema lock: %w", err)
	}
	defer releaseAdvisoryLock(db)

	if err := ensureAppliedTable(db); err != nil {
		return fmt.Errorf("ensure schema_applied table: %w", err)
	}

	files, err := buildSeedFiles(env, adminPassword)
	if err != nil {
		return err
	}
	for _, f := range files {
		hash := hashSQL(f.sql)
		applied, err := isApplied(db, f.name, hash)
		if err != nil {
			return err
		}
		if applied {
			slog.Info("schema file unchanged, skipping", "file", f.name)
			continue
		}

		slog.Info("applying schema file", "file", f.name, "hash", hash)
		if _, err := db.Exec(f.sql); err != nil {
			return fmt.Errorf("apply %s: %w", f.name, err)
		}
		if err := recordApplied(db, f.name, hash); err != nil {
			return fmt.Errorf("record applied %s: %w", f.name, err)
		}
		slog.Info("schema file applied", "file", f.name)
	}
	return nil
}

// buildSeedFiles 组装按顺序应用的文件列表。
// adminPassword 非空时用 argon2id 现算哈希替换 seed.sql 占位符；
// 为空时改写占位符为 NULL 语义（此处直接替换为空 hash，配合 seed.sql 的 WHERE 守卫跳过插入）。
// 仅 env=development 追加 seed.dev.sql 测试数据。
func buildSeedFiles(env, adminPassword string) ([]seedFile, error) {
	seedContent, err := renderSeed(seedSQL, adminPassword)
	if err != nil {
		return nil, err
	}
	files := []seedFile{
		{name: "schema.sql", sql: schemaSQL},
		{name: "seed.sql", sql: seedContent},
	}
	if env == config.EnvDevelopment {
		files = append(files, seedFile{name: "seed.dev.sql", sql: seedDevSQL})
	}
	return files, nil
}

// renderSeed 将 seed.sql 中的超管密码占位符替换为 argon2id 哈希。
// adminPassword 为空时替换为空串，seed.sql 的 WHERE 守卫据此跳过写入（生产不内置账号）。
func renderSeed(sqlText, adminPassword string) (string, error) {
	if adminPassword == "" {
		return strings.ReplaceAll(sqlText, adminPasswordPlaceholder, ""), nil
	}
	hash, err := crypto.HashPassword(adminPassword, config.DefaultArgon2())
	if err != nil {
		return "", fmt.Errorf("hash seed admin password: %w", err)
	}
	return strings.ReplaceAll(sqlText, adminPasswordPlaceholder, hash), nil
}

// ensureAppliedTable 创建 schema 应用记录表（幂等）。
func ensureAppliedTable(db *sql.DB) error {
	const q = `
CREATE TABLE IF NOT EXISTS schema_applied (
    file        TEXT        PRIMARY KEY,
    hash        CHAR(64)    NOT NULL,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT now()
)`
	_, err := db.Exec(q)
	return err
}

// isApplied 判断文件是否已按给定哈希应用。
func isApplied(db *sql.DB, name, hash string) (bool, error) {
	var stored string
	err := db.QueryRow(`SELECT hash FROM schema_applied WHERE file = $1`, name).Scan(&stored)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query schema_applied(%s): %w", name, err)
	}
	return stored == hash, nil
}

// recordApplied 记录文件某哈希已应用（幂等 upsert）。
func recordApplied(db *sql.DB, name, hash string) error {
	const q = `
INSERT INTO schema_applied (file, hash, applied_at)
VALUES ($1, $2, now())
ON CONFLICT (file) DO UPDATE SET hash = EXCLUDED.hash, applied_at = now()`
	_, err := db.Exec(q, name, hash)
	return err
}

// hashSQL 计算 SQL 文件内容的 SHA-256（用于幂等判断）。
func hashSQL(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// acquireAdvisoryLock 获取 PG advisory lock（非阻塞，超时后返回错误）。
func acquireAdvisoryLock(ctx context.Context, db *sql.DB) error {
	for {
		var locked bool
		if err := db.QueryRowContext(ctx,
			"SELECT pg_try_advisory_lock($1)", migrationAdvisoryLock,
		).Scan(&locked); err != nil {
			return fmt.Errorf("pg_try_advisory_lock: %w", err)
		}
		if locked {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(migrationPollInterval):
		}
	}
}

// releaseAdvisoryLock 释放 PG advisory lock（幂等，多次调用安全）。
func releaseAdvisoryLock(db *sql.DB) {
	if _, err := db.Exec("SELECT pg_advisory_unlock($1)", migrationAdvisoryLock); err != nil {
		slog.Warn("release migration advisory lock failed (non-fatal)", "err", err)
	}
}
