//go:build !desktop

package pgadapter

import (
	"context"
	"errors"

	"arkloop/services/shared/database"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool wraps *pgxpool.Pool to implement database.DB.
type Pool struct {
	pool *pgxpool.Pool
}

// New creates a database.DB backed by a pgx connection pool.
func New(pool *pgxpool.Pool) *Pool {
	return &Pool{pool: pool}
}

// Unwrap returns the underlying pgxpool.Pool for code that still needs
// direct pgx access (e.g. LISTEN/NOTIFY, pgx-specific features).
func (p *Pool) Unwrap() *pgxpool.Pool {
	return p.pool
}

func (p *Pool) Exec(ctx context.Context, sql string, args ...any) (database.Result, error) {
	tag, err := p.pool.Exec(ctx, sql, args...)
	if err != nil {
		return nil, translateError(err)
	}
	return result{tag: tag}, nil
}

func (p *Pool) Query(ctx context.Context, sql string, args ...any) (database.Rows, error) {
	r, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, translateError(err)
	}
	return &rows{rows: r}, nil
}

func (p *Pool) QueryRow(ctx context.Context, sql string, args ...any) database.Row {
	return &row{row: p.pool.QueryRow(ctx, sql, args...)}
}

func (p *Pool) Begin(ctx context.Context) (database.Tx, error) {
	t, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, translateError(err)
	}
	return &tx{tx: t}, nil
}

func (p *Pool) Close() error {
	p.pool.Close()
	return nil
}

func (p *Pool) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

// tx wraps pgx.Tx to implement database.Tx.
type tx struct {
	tx pgx.Tx
}

// UnwrapTx returns the underlying pgx.Tx.
func (t *tx) UnwrapTx() pgx.Tx {
	return t.tx
}

func (t *tx) Exec(ctx context.Context, sql string, args ...any) (database.Result, error) {
	tag, err := t.tx.Exec(ctx, sql, args...)
	if err != nil {
		return nil, translateError(err)
	}
	return result{tag: tag}, nil
}

func (t *tx) Query(ctx context.Context, sql string, args ...any) (database.Rows, error) {
	r, err := t.tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, translateError(err)
	}
	return &rows{rows: r}, nil
}

func (t *tx) QueryRow(ctx context.Context, sql string, args ...any) database.Row {
	return &row{row: t.tx.QueryRow(ctx, sql, args...)}
}

func (t *tx) Commit(ctx context.Context) error {
	return translateError(t.tx.Commit(ctx))
}

func (t *tx) Rollback(ctx context.Context) error {
	return translateError(t.tx.Rollback(ctx))
}

// row wraps pgx.Row to implement database.Row with error translation.
type row struct {
	row pgx.Row
}

func (r *row) Scan(dest ...any) error {
	return translateError(r.row.Scan(dest...))
}

// rows wraps pgx.Rows to implement database.Rows.
type rows struct {
	rows pgx.Rows
}

func (r *rows) Next() bool        { return r.rows.Next() }
func (r *rows) Scan(dest ...any) error { return translateError(r.rows.Scan(dest...)) }
func (r *rows) Close()            { r.rows.Close() }
func (r *rows) Err() error        { return translateError(r.rows.Err()) }

// result wraps pgconn.CommandTag to implement database.Result.
type result struct {
	tag pgconn.CommandTag
}

func (r result) RowsAffected() int64 { return r.tag.RowsAffected() }

// translateError converts pgx-specific errors to database package errors.
func translateError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return database.ErrNoRows
	}
	return err
}
