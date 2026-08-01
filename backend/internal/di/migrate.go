// Package di 提供依赖注入与启动引导。
package di

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/pressly/goose/v3"

	_ "github.com/jackc/pgx/v5/stdlib" // 注册 pgx database/sql 驱动供 goose 使用
)

const (
	// defaultMigrationsDir 迁移文件目录（相对于工作目录，支持绝对路径覆盖）。
	defaultMigrationsDir = "migrations"
	// migrationAdvisoryLock 避免多实例并发执行迁移的 PG advisory lock（随机 magic number）。
	migrationAdvisoryLock = 0x484E4D47 // "HNMG" = Health Nexus MiGration
	// migrationLockTimeout 获取迁移锁的超时。
	migrationLockTimeout = 30 * time.Second
)

// RunMigrations 在启动时自动应用未执行的数据库迁移。
// goose 的 Up() 是幂等的——已执行的迁移不会重复执行。
// 使用 PG advisory lock 防止多实例并发（server + worker 同时启动时，仅一个执行迁移）。
func RunMigrations(ctx context.Context, dsn string, migrationsDir string) error {
	if migrationsDir == "" {
		migrationsDir = defaultMigrationsDir
	}
	absDir, err := filepath.Abs(migrationsDir)
	if err != nil {
		return fmt.Errorf("resolve migrations dir %q: %w", migrationsDir, err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open db for migration: %w", err)
	}
	defer db.Close()

	// 获取 advisory lock 避免并发迁移
	lockCtx, cancel := context.WithTimeout(ctx, migrationLockTimeout)
	defer cancel()
	if err := acquireAdvisoryLock(lockCtx, db); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer releaseAdvisoryLock(db)

	slog.Info("running database migrations", "dir", absDir)
	if err := goose.Up(db, absDir); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	slog.Info("database migrations complete")
	return nil
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
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// releaseAdvisoryLock 释放 PG advisory lock（幂等，多次调用安全）。
func releaseAdvisoryLock(db *sql.DB) {
	if _, err := db.Exec("SELECT pg_advisory_unlock($1)", migrationAdvisoryLock); err != nil {
		slog.Warn("release migration advisory lock failed (non-fatal)", "err", err)
	}
}
