// TxManager.WithTx 单元测试（D-MED-07 嵌套语义）。
// 覆盖：成功 Commit / 失败 Rollback / 嵌套复用外层事务。
// 不可达 DB 时自动 Skip（沿用 schema_test 的 skip 模式）。
package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// mustPool 连接本地 PostgreSQL，不可达时跳过测试。
func mustPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("SCHEMA_TEST_DSN")
	if dsn == "" {
		dsn = "postgres://health:health@localhost:5432/health_nexus?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("DB 不可用，跳过 tx 测试: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("DB 不可达，跳过 tx 测试: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// countDeptsByName 统计指定 name 的 departments 行数（用于校验 COMMIT/ROLLBACK 可见性）。
func countDeptsByName(t *testing.T, pool *pgxpool.Pool, name string) int {
	t.Helper()
	var cnt int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM departments WHERE name = $1`, name).Scan(&cnt); err != nil {
		t.Fatalf("count departments: %v", err)
	}
	return cnt
}

// cleanupDept 删除测试写入的临时行，避免污染 DB。
func cleanupDept(t *testing.T, pool *pgxpool.Pool, name string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`DELETE FROM departments WHERE name = $1`, name); err != nil {
		t.Logf("cleanup departments failed: %v", err)
	}
}

// TestWithTx_CommitOnSuccess 验证：非事务内调用 WithTx，fn 返回 nil → 事务 Commit，写入对其它连接可见。
func TestWithTx_CommitOnSuccess(t *testing.T) {
	pool := mustPool(t)
	mgr := NewTxManager(pool)
	ctx := context.Background()
	name := fmt.Sprintf("__test_tx_commit_%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanupDept(t, pool, name) })

	err := mgr.WithTx(ctx, func(ctx context.Context) error {
		// 必须用 ctx 内的 tx 执行，才能纳入事务（pool.Exec 会用独立连接绕过 tx）。
		tx, _ := TxFromCtx(ctx)
		_, execErr := tx.Exec(ctx,
			`INSERT INTO departments (name, description) VALUES ($1, 'commit test')`, name)
		return execErr
	})
	if err != nil {
		t.Fatalf("WithTx returned error: %v", err)
	}
	if cnt := countDeptsByName(t, pool, name); cnt != 1 {
		t.Errorf("after commit: count=%d, want 1 (row should be visible)", cnt)
	}
}

// TestWithTx_RollbackOnError 验证：fn 返回 error → 事务 Rollback，写入不可见。
func TestWithTx_RollbackOnError(t *testing.T) {
	pool := mustPool(t)
	mgr := NewTxManager(pool)
	ctx := context.Background()
	name := fmt.Sprintf("__test_tx_rollback_%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanupDept(t, pool, name) })

	forcedErr := errors.New("force rollback")
	err := mgr.WithTx(ctx, func(ctx context.Context) error {
		tx, _ := TxFromCtx(ctx)
		if _, execErr := tx.Exec(ctx,
			`INSERT INTO departments (name, description) VALUES ($1, 'rollback test')`, name); execErr != nil {
			return execErr
		}
		return forcedErr
	})
	if !errors.Is(err, forcedErr) {
		t.Fatalf("WithTx returned %v, want %v", err, forcedErr)
	}
	if cnt := countDeptsByName(t, pool, name); cnt != 0 {
		t.Errorf("after rollback: count=%d, want 0 (row should be rolled back)", cnt)
	}
}

// TestWithTx_NestedReusesOuterTx 验证：外层 WithTx 包裹内层 WithTx，内层复用外层事务不独立 Commit。
//
// 关键断言：内层 INSERT 后，外层返回 error 触发 Rollback → 内层的写入也应被回滚。
// 若内层独立 Commit（bug 行为），外层 Rollback 只回滚外层自己的写入，内层行会保留——断言失败。
func TestWithTx_NestedReusesOuterTx(t *testing.T) {
	pool := mustPool(t)
	mgr := NewTxManager(pool)
	ctx := context.Background()
	nameInner := fmt.Sprintf("__test_tx_nested_inner_%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanupDept(t, pool, nameInner) })

	forcedErr := errors.New("force outer rollback")
	err := mgr.WithTx(ctx, func(ctx context.Context) error {
		// 内层 WithTx：写入行，返回 nil。如果它独立 Commit，行就会持久化。
		if innerErr := mgr.WithTx(ctx, func(ctx context.Context) error {
			tx, _ := TxFromCtx(ctx)
			_, execErr := tx.Exec(ctx,
				`INSERT INTO departments (name, description) VALUES ($1, 'nested inner')`, nameInner)
			return execErr
		}); innerErr != nil {
			return innerErr
		}
		// 外层返回 error → 触发整个事务 Rollback。
		// 嵌套复用语义下，内层写入在外层事务内，应一并回滚。
		return forcedErr
	})
	if !errors.Is(err, forcedErr) {
		t.Fatalf("WithTx returned %v, want %v", err, forcedErr)
	}
	if cnt := countDeptsByName(t, pool, nameInner); cnt != 0 {
		t.Errorf("after outer rollback: inner count=%d, want 0 (nested reuse should rollback inner writes too)", cnt)
	}
}
