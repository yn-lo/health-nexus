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
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // 注册 pgx database/sql 驱动
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

// allSchema 待应用（按顺序）的 SQL 文件。每个文件都是一个独立的第个字段：
// 文件级内容哈希决定是否重跑；文件内语句由 PG 以单个隐式事务整体执行，失败整体回滚。
var allSchema = []struct {
	name string
	sql  string
}{
	{name: "schema.sql", sql: schemaSQL},
	{name: "seed.sql", sql: seedSQL},
}

// ApplySchema 在启动时按"内容哈希"幂等应用 schema.sql 与 seed.sql：
//   - 该文件从未应用 / 内容哈希已变化 → 整文件重跑，随后记录新哈希；
//   - 哈希未变化 → 直接跳过（幂等）。
//
// 两个文件 DDL/种子均已写成幂等形式（IF NOT EXISTS / ON CONFLICT 等），
// 即使内容哈希变化导致重跑，对已存在对象也安全。
// 使用 PG advisory lock 防止 server + worker 并发同时应用。
func ApplySchema(ctx context.Context, dsn string) error {
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

	for _, f := range allSchema {
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
