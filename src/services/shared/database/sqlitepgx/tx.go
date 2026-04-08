//go:build desktop

package sqlitepgx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Tx wraps *sql.Tx to satisfy the pgx.Tx interface.
type Tx struct {
	tx                 *sql.Tx
	readOnly           bool
	afterCommitHooks   []func()
	afterRollbackHooks []func()
	writeGuard         WriteGuard
	writeExecutor      WriteExecutor
}

// AfterCommit registers fn to run after the transaction commits successfully.
// If Commit is never called or fails, fn is never executed.
func (t *Tx) AfterCommit(fn func()) {
	t.afterCommitHooks = append(t.afterCommitHooks, fn)
}

func (t *Tx) AfterRollback(fn func()) {
	t.afterRollbackHooks = append(t.afterRollbackHooks, fn)
}

func (t *Tx) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if t.readOnly {
		return pgconn.NewCommandTag(""), fmt.Errorf("sqlitepgx: write exec in read-only transaction")
	}
	guard, err := t.acquireExecWriteGuard(ctx)
	if err != nil {
		return pgconn.NewCommandTag(""), err
	}
	defer guard.Release()

	query = rewriteSQL(query)
	query, args = expandAnyArgs(query, args)
	args = convertArgs(args)
	r, err := t.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return pgconn.NewCommandTag(""), translateError(err)
	}
	n, _ := r.RowsAffected()
	return pgconn.NewCommandTag(fmt.Sprintf("OK %d", n)), nil
}

func (t *Tx) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	query = rewriteSQL(query)
	query, args = expandAnyArgs(query, args)
	args = convertArgs(args)
	r, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return &shimRows{err: translateError(err)}, nil
	}
	return &shimRows{rows: r}, nil
}

func (t *Tx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	query = rewriteSQL(query)
	query, args = expandAnyArgs(query, args)
	args = convertArgs(args)
	return &shimRow{row: t.tx.QueryRowContext(ctx, query, args...)}
}

func (t *Tx) Commit(_ context.Context) error {
	err := translateError(t.tx.Commit())
	t.releaseWriteGuard()
	if err != nil {
		t.afterCommitHooks = nil
		t.afterRollbackHooks = nil
		return err
	}
	for _, fn := range t.afterCommitHooks {
		fn()
	}
	t.afterCommitHooks = nil
	t.afterRollbackHooks = nil
	return nil
}

func (t *Tx) Rollback(_ context.Context) error {
	err := translateError(t.tx.Rollback())
	t.releaseWriteGuard()
	// 事务已结束（正常回滚或已隐式回滚），都需要执行清理 hooks
	if err == nil || errors.Is(err, sql.ErrTxDone) {
		for _, fn := range t.afterRollbackHooks {
			fn()
		}
	}
	t.afterCommitHooks = nil
	t.afterRollbackHooks = nil
	return err
}

// -- pgx.Tx interface stubs (unused in desktop pipeline) --

func (t *Tx) Begin(_ context.Context) (pgx.Tx, error) {
	return nil, fmt.Errorf("sqlitepgx: nested transactions not supported")
}

func (t *Tx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	return 0, fmt.Errorf("sqlitepgx: CopyFrom not supported")
}

func (t *Tx) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults {
	return &errBatchResults{err: fmt.Errorf("sqlitepgx: SendBatch not supported")}
}

func (t *Tx) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}

func (t *Tx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	return nil, fmt.Errorf("sqlitepgx: Prepare not supported")
}

func (t *Tx) Conn() *pgx.Conn {
	return nil
}

func (t *Tx) releaseWriteGuard() {
	if t.writeGuard == nil {
		return
	}
	t.writeGuard.Release()
	t.writeGuard = nil
}

func (t *Tx) acquireExecWriteGuard(ctx context.Context) (WriteGuard, error) {
	if t.writeGuard != nil {
		return noopWriteGuard{}, nil
	}
	executor := t.writeExecutor
	if executor == nil {
		executor = GetGlobalWriteExecutor()
	}
	if executor == nil {
		return noopWriteGuard{}, nil
	}
	guard, err := executor.AcquireWrite(ctx)
	if err != nil {
		return nil, err
	}
	if guard == nil {
		return noopWriteGuard{}, nil
	}
	return guard, nil
}

// errBatchResults satisfies pgx.BatchResults for unsupported SendBatch calls.
type errBatchResults struct{ err error }

func (e *errBatchResults) Exec() (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag(""), e.err
}
func (e *errBatchResults) Query() (pgx.Rows, error) { return nil, e.err }
func (e *errBatchResults) QueryRow() pgx.Row        { return &shimRow{err: e.err} }
func (e *errBatchResults) Close() error             { return nil }
