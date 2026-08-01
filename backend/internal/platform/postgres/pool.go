// Package postgres 封装 pgx 连接池与事务管理，供 Repository 实现使用。
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"health-nexus/internal/config"
)

// pingTimeout 是启动时 ping 数据库的超时时间。
const pingTimeout = 5 * time.Second

// NewPool 创建 pgx 连接池并 ping 验证连接。
// 连接池参数 MaxConns/MinConns/MaxConnLifetime 来自 cfg。
func NewPool(ctx context.Context, cfg config.PostgresConfig) (*pgxpool.Pool, error) {
	pcfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}
	if cfg.MaxConns > 0 {
		pcfg.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		pcfg.MinConns = cfg.MinConns
	}
	if cfg.MaxConnLifetime > 0 {
		pcfg.MaxConnLifetime = cfg.MaxConnLifetime
	}

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}
