package postgres

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// txKey 是事务在 context 中的私有键类型。
type txKey struct{}

// TxManager 管理数据库事务，支持嵌套复用：context 中已有事务则复用，否则开启新事务。
type TxManager struct {
	pool *pgxpool.Pool
}

// NewTxManager 创建事务管理器。
func NewTxManager(pool *pgxpool.Pool) *TxManager {
	return &TxManager{pool: pool}
}

// TxFromCtx 从 context 中提取事务。Repository 实现可用此函数获取当前事务。
// 若 context 中无事务，返回 (nil, false)。
func TxFromCtx(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	return tx, ok
}

// WithTx 在事务中执行 fn。
// 若 ctx 中已有事务则直接复用（嵌套调用不负责 Commit/Rollback）；
// 否则开启新事务，fn 返回 error 时 Rollback，成功时 Commit，panic 时也 Rollback 并重新抛出。
//
// ponytail: 嵌套复用——内层 WithTx 不开 savepoint，直接 fn(ctx) 透传到外层事务。
// 上限：内层 fn 返回 error 时不会仅回滚内层写入（无 savepoint），error 透传给外层；
// 若外层 fn 捕获了该 error 并继续返回 nil，内层的部分写入仍会随外层 Commit 落库。
// 当前调用方均在外层 fn 中遇错即 return（不吞错），上限不暴露。
// 升级路径：需要部分回滚时引入 pgx.Tx.SavePoint，或拆分事务编排。
func (m *TxManager) WithTx(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	if _, ok := TxFromCtx(ctx); ok {
		return fn(ctx)
	}

	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	txCtx := context.WithValue(ctx, txKey{}, tx)
	defer func() {
		if p := recover(); p != nil {
			// 用 context.Background()：原 ctx 可能已 Done，但 Rollback 必须执行。
			if rbErr := tx.Rollback(context.Background()); rbErr != nil {
				slog.ErrorContext(ctx, "tx rollback after panic", "err", rbErr)
			}
			//nolint:forbidigo // 平台层重新抛出 recover 到的 panic，区别于业务代码中新增 panic
			panic(p)
		}
		if err != nil {
			if rbErr := tx.Rollback(context.Background()); rbErr != nil {
				slog.ErrorContext(ctx, "tx rollback", "err", rbErr, "original_err", err)
			}
			return
		}
		if cErr := tx.Commit(context.Background()); cErr != nil {
			err = fmt.Errorf("commit tx: %w", cErr)
		}
	}()

	return fn(txCtx)
}
