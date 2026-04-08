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

func TestPoolExecAcquiresWriter(t *testing.T) {
	t.Parallel()
	base := openTestDB(t)
	pool := base.WithWriteExecutor(&spyWriteExecutor{})
	createTestTable(t, pool)

	spy := pool.writeExecutor.(*spyWriteExecutor)
	baseline := spy.Count()
	if _, err := pool.Exec(context.Background(), `INSERT INTO items (name) VALUES ('alpha')`); err != nil {
		t.Fatalf("exec write: %v", err)
	}
	if got := spy.Count() - baseline; got != 1 {
		t.Fatalf("exec write should acquire writer once, got %d", got)
	}
}

func TestPoolQueryRowWriteAcquiresAndReleasesWriter(t *testing.T) {
	t.Parallel()
	pool := openTestDB(t).WithWriteExecutor(NewSerialWriteExecutor())
	createTestTable(t, pool)

	ctx := context.Background()
	var id int
	row := pool.QueryRow(ctx, `INSERT INTO items (name) VALUES ('alpha') RETURNING id`)

	timeoutCtx, cancel := context.WithTimeout(ctx, 40*time.Millisecond)
	defer cancel()
	if _, err := pool.Exec(timeoutCtx, `INSERT INTO items (name) VALUES ('beta')`); err == nil {
		t.Fatal("expected exec to block until QueryRow scan releases writer")
	}

	if err := row.Scan(&id); err != nil {
		t.Fatalf("scan returning row: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO items (name) VALUES ('beta')`); err != nil {
		t.Fatalf("exec after QueryRow scan: %v", err)
	}
}

func TestPoolQueryWithWriteCTEAcquiresWriter(t *testing.T) {
	t.Parallel()
	base := openTestDB(t)
	pool := base.WithWriteExecutor(&spyWriteExecutor{})
	createTestTable(t, pool)

	spy := pool.writeExecutor.(*spyWriteExecutor)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO items (name) VALUES ('alpha')`); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	baseline := spy.Count()
	rows, err := pool.Query(ctx, `
		UPDATE items
		   SET name = 'beta'
		 WHERE name = 'alpha'
		RETURNING id, name`)
	if err != nil {
		t.Fatalf("query returning write: %v", err)
	}
	defer rows.Close()
	if got := spy.Count() - baseline; got != 1 {
		t.Fatalf("returning query should acquire writer once, got %d", got)
	}

	var (
		id   int
		name string
	)
	if !rows.Next() {
		t.Fatal("expected one row from write cte")
	}
	if err := rows.Scan(&id, &name); err != nil {
		t.Fatalf("scan write cte row: %v", err)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
	if id <= 0 || name != "beta" {
		t.Fatalf("unexpected row: id=%d name=%q", id, name)
	}
}

func TestTxQueryRowWriteReusesTxWriter(t *testing.T) {
	t.Parallel()
	base := openTestDB(t)
	pool := base.WithWriteExecutor(&spyWriteExecutor{})
	createTestTable(t, pool)

	spy := pool.writeExecutor.(*spyWriteExecutor)
	baseline := spy.Count()
	tx, err := pool.BeginTx(context.Background(), pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	var id int
	if err := tx.QueryRow(context.Background(), `INSERT INTO items (name) VALUES ('alpha') RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("tx query row write: %v", err)
	}
	if got := spy.Count() - baseline; got != 1 {
		t.Fatalf("write tx QueryRow should reuse existing writer, got %d acquisitions", got)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit tx: %v", err)
	}
}

func TestReadOnlyTxQueryRowRejectsWrite(t *testing.T) {
	t.Parallel()
	base := openTestDB(t)
	pool := base.WithWriteExecutor(&spyWriteExecutor{})
	createTestTable(t, pool)

	tx, err := pool.BeginTx(context.Background(), pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("begin readonly tx: %v", err)
	}
	defer tx.Rollback(context.Background()) //nolint:errcheck

	var id int
	err = tx.QueryRow(context.Background(), `INSERT INTO items (name) VALUES ('alpha') RETURNING id`).Scan(&id)
	if err == nil || err.Error() != "sqlitepgx: write query in read-only transaction" {
		t.Fatalf("expected read-only QueryRow write rejection, got %v", err)
	}
}

func TestReadOnlyTxQueryRejectsWrite(t *testing.T) {
	t.Parallel()
	base := openTestDB(t)
	pool := base.WithWriteExecutor(&spyWriteExecutor{})
	createTestTable(t, pool)

	tx, err := pool.BeginTx(context.Background(), pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("begin readonly tx: %v", err)
	}
	defer tx.Rollback(context.Background()) //nolint:errcheck

	rows, err := tx.Query(context.Background(), `WITH updated AS (
		UPDATE items SET name = 'beta' WHERE 1 = 0 RETURNING id
	) SELECT id FROM updated`)
	if err != nil {
		t.Fatalf("readonly Query should return shim rows, got err %v", err)
	}
	defer rows.Close()
	if err := rows.Err(); err == nil || err.Error() != "sqlitepgx: write query in read-only transaction" {
		t.Fatalf("expected read-only Query write rejection, got %v", err)
	}
}

func TestQueryRequiresWriteGuard_WithWriteCTE(t *testing.T) {
	t.Parallel()
	if !queryRequiresWriteGuard(`
		WITH updated AS (
			UPDATE items SET name = 'beta' RETURNING id
		)
		SELECT id FROM updated`) {
		t.Fatal("expected write CTE to require writer")
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
