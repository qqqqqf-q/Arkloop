//go:build desktop

package sqliteadapter

import (
	"context"
	"database/sql"
	"errors"

	"arkloop/services/shared/database"

	_ "modernc.org/sqlite" // SQLite driver registration.
)

// Pool wraps *sql.DB to implement database.DB.
type Pool struct {
	db *sql.DB
}

// New creates a database.DB backed by an existing *sql.DB handle.
func New(db *sql.DB) *Pool {
	return &Pool{db: db}
}

// Open opens a SQLite database with sensible defaults for an embedded single-writer workload.
func Open(dataSourceName string) (*Pool, error) {
	db, err := sql.Open("sqlite", dataSourceName)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, err
		}
	}

	return &Pool{db: db}, nil
}

// Unwrap returns the underlying *sql.DB for code that needs direct access.
func (p *Pool) Unwrap() *sql.DB {
	return p.db
}

func (p *Pool) Exec(ctx context.Context, sql string, args ...any) (database.Result, error) {
	r, err := p.db.ExecContext(ctx, sql, args...)
	if err != nil {
		return nil, translateError(err)
	}
	return result{r: r}, nil
}

func (p *Pool) Query(ctx context.Context, sql string, args ...any) (database.Rows, error) {
	r, err := p.db.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, translateError(err)
	}
	return &rows{rows: r}, nil
}

func (p *Pool) QueryRow(ctx context.Context, sql string, args ...any) database.Row {
	return &row{row: p.db.QueryRowContext(ctx, sql, args...)}
}

func (p *Pool) Begin(ctx context.Context) (database.Tx, error) {
	t, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, translateError(err)
	}
	return &tx{tx: t}, nil
}

func (p *Pool) Close() error {
	return p.db.Close()
}

func (p *Pool) Ping(ctx context.Context) error {
	return p.db.PingContext(ctx)
}

// tx wraps *sql.Tx to implement database.Tx.
type tx struct {
	tx *sql.Tx
}

func (t *tx) Exec(ctx context.Context, sql string, args ...any) (database.Result, error) {
	r, err := t.tx.ExecContext(ctx, sql, args...)
	if err != nil {
		return nil, translateError(err)
	}
	return result{r: r}, nil
}

func (t *tx) Query(ctx context.Context, sql string, args ...any) (database.Rows, error) {
	r, err := t.tx.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, translateError(err)
	}
	return &rows{rows: r}, nil
}

func (t *tx) QueryRow(ctx context.Context, sql string, args ...any) database.Row {
	return &row{row: t.tx.QueryRowContext(ctx, sql, args...)}
}

func (t *tx) Commit(_ context.Context) error {
	return translateError(t.tx.Commit())
}

func (t *tx) Rollback(_ context.Context) error {
	return translateError(t.tx.Rollback())
}

// row wraps *sql.Row to implement database.Row with error translation.
type row struct {
	row *sql.Row
}

func (r *row) Scan(dest ...any) error {
	return translateError(r.row.Scan(dest...))
}

// rows wraps *sql.Rows to implement database.Rows.
type rows struct {
	rows *sql.Rows
}

func (r *rows) Next() bool            { return r.rows.Next() }
func (r *rows) Scan(dest ...any) error { return translateError(r.rows.Scan(dest...)) }
func (r *rows) Close()                { r.rows.Close() }
func (r *rows) Err() error            { return translateError(r.rows.Err()) }

// result wraps sql.Result to implement database.Result.
type result struct {
	r sql.Result
}

func (r result) RowsAffected() int64 {
	n, _ := r.r.RowsAffected()
	return n
}

// translateError converts database/sql errors to database package errors.
func translateError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return database.ErrNoRows
	}
	return err
}
