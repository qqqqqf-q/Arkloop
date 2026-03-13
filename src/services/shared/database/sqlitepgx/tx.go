//go:build desktop

package sqlitepgx

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Tx wraps *sql.Tx to satisfy the pgx.Tx interface.
type Tx struct {
	tx *sql.Tx
}

func (t *Tx) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	query = rewriteSQL(query)
	r, err := t.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return pgconn.NewCommandTag(""), translateError(err)
	}
	n, _ := r.RowsAffected()
	return pgconn.NewCommandTag(fmt.Sprintf("OK %d", n)), nil
}

func (t *Tx) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	query = rewriteSQL(query)
	r, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return &shimRows{err: translateError(err)}, nil
	}
	return &shimRows{rows: r}, nil
}

func (t *Tx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	query = rewriteSQL(query)
	return &shimRow{row: t.tx.QueryRowContext(ctx, query, args...)}
}

func (t *Tx) Commit(_ context.Context) error {
	return translateError(t.tx.Commit())
}

func (t *Tx) Rollback(_ context.Context) error {
	return translateError(t.tx.Rollback())
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

// errBatchResults satisfies pgx.BatchResults for unsupported SendBatch calls.
type errBatchResults struct{ err error }

func (e *errBatchResults) Exec() (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag(""), e.err
}
func (e *errBatchResults) Query() (pgx.Rows, error) { return nil, e.err }
func (e *errBatchResults) QueryRow() pgx.Row         { return &shimRow{err: e.err} }
func (e *errBatchResults) Close() error               { return nil }
