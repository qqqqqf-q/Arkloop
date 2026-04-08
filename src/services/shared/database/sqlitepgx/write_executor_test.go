//go:build desktop

package sqlitepgx

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type spyWriteExecutor struct {
	calls atomic.Int64
}

func (s *spyWriteExecutor) AcquireWrite(context.Context) (WriteGuard, error) {
	s.calls.Add(1)
	return noopWriteGuard{}, nil
}

func (s *spyWriteExecutor) Count() int64 {
	return s.calls.Load()
}

func TestSerialWriteExecutor_ContextCancel(t *testing.T) {
	t.Parallel()
	executor := NewSerialWriteExecutor()
	first, err := executor.AcquireWrite(context.Background())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer first.Release()

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err = executor.AcquireWrite(ctx)
	if err == nil {
		t.Fatal("expected acquire timeout error")
	}
}

func TestPoolBeginTx_WriteAcquiresOnce(t *testing.T) {
	t.Parallel()
	base := openTestDB(t)
	pool := base.WithWriteExecutor(&spyWriteExecutor{})
	createTestTable(t, pool)

	spy := pool.writeExecutor.(*spyWriteExecutor)
	baseline := spy.Count()
	ctx := context.Background()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if got := spy.Count() - baseline; got != 1 {
		t.Fatalf("begin tx acquire count = %d, want 1", got)
	}

	if _, err := tx.Exec(ctx, `INSERT INTO items (name) VALUES ('alpha')`); err != nil {
		t.Fatalf("tx exec: %v", err)
	}
	if got := spy.Count() - baseline; got != 1 {
		t.Fatalf("write tx exec should not reacquire, got %d", got)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func TestPoolBeginTx_ReadOnlyBypassesWriter(t *testing.T) {
	t.Parallel()
	base := openTestDB(t)
	pool := base.WithWriteExecutor(&spyWriteExecutor{})
	createTestTable(t, pool)

	spy := pool.writeExecutor.(*spyWriteExecutor)
	baseline := spy.Count()
	ctx := context.Background()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("begin readonly tx: %v", err)
	}
	if got := spy.Count() - baseline; got != 0 {
		t.Fatalf("readonly begin should not acquire writer, got %d", got)
	}

	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM items`).Scan(&count); err != nil {
		t.Fatalf("readonly query: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback readonly tx: %v", err)
	}
}

func TestPoolReadQueryBypassesWriter(t *testing.T) {
	t.Parallel()
	base := openTestDB(t)
	pool := base.WithWriteExecutor(&spyWriteExecutor{})
	createTestTable(t, pool)

	spy := pool.writeExecutor.(*spyWriteExecutor)
	baseline := spy.Count()
	ctx := context.Background()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM items`).Scan(&count); err != nil {
		t.Fatalf("query row: %v", err)
	}
	if got := spy.Count() - baseline; got != 0 {
		t.Fatalf("query row should not acquire writer, got %d", got)
	}
}

func TestTxCommitFailureKeepsRollbackHooks(t *testing.T) {
	t.Parallel()
	pool := openTestDB(t)
	createTestTable(t, pool)

	ctx := context.Background()
	txIface, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	tx, ok := txIface.(*Tx)
	if !ok {
		t.Fatalf("expected *Tx, got %T", txIface)
	}

	var rollbackHookCalls atomic.Int64
	tx.AfterRollback(func() {
		rollbackHookCalls.Add(1)
	})

	if err := tx.tx.Rollback(); err != nil {
		t.Fatalf("force rollback: %v", err)
	}
	if err := tx.Commit(ctx); err == nil {
		t.Fatal("expected commit to fail after forced rollback")
	}
	if err := tx.Rollback(ctx); err == nil {
		t.Fatal("expected rollback to report tx done")
	}
	if got := rollbackHookCalls.Load(); got != 1 {
		t.Fatalf("rollback hook calls = %d, want 1", got)
	}
}
