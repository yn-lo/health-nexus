package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Querier 是 pgx.Tx 与 *pgxpool.Pool 共同满足的最小查询接口。
// 各域 repository 通过此接口实现事务透明的查询。
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Q 从 ctx 取事务，否则回退到 pool。
func Q(ctx context.Context, pool *pgxpool.Pool) Querier {
	if tx, ok := TxFromCtx(ctx); ok {
		return tx
	}
	return pool
}

// Scanner 抽象 pgx.Row 和 pgx.Rows 的 Scan 方法。
type Scanner interface {
	Scan(dest ...any) error
}

// uniqueViolationCode PostgreSQL SQLSTATE 23505（唯一约束冲突）。
const uniqueViolationCode = "23505"

// IsUniqueViolation 判断是否为 PostgreSQL 唯一约束冲突（SQLSTATE 23505）。
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == uniqueViolationCode
	}
	return false
}
