//go:build desktop

// Package sqlitepgx wraps database/sql with a SQLite backend to satisfy
// the pgx-based data.Querier interface used by API repositories.
// This allows existing repositories to work with SQLite without code changes.
package sqlitepgx

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	_ "modernc.org/sqlite" // SQLite driver registration.
)

// Pool wraps *sql.DB to satisfy the pgx-based data.Querier interface.
type Pool struct {
	db *sql.DB
}

// New creates a Pool backed by an existing *sql.DB handle.
func New(db *sql.DB) *Pool {
	return &Pool{db: db}
}

// Open opens a SQLite database with sensible defaults for an embedded
// single-writer workload (WAL, foreign keys, busy timeout, etc.).
func Open(dsn string) (*Pool, error) {
	db, err := sql.Open("sqlite", dsn)
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

func (p *Pool) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	query = rewriteSQL(query)
	r, err := p.db.ExecContext(ctx, query, args...)
	if err != nil {
		return pgconn.NewCommandTag(""), translateError(err)
	}
	n, _ := r.RowsAffected()
	return pgconn.NewCommandTag(fmt.Sprintf("OK %d", n)), nil
}

func (p *Pool) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	query = rewriteSQL(query)
	r, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return &shimRows{err: translateError(err)}, nil
	}
	return &shimRows{rows: r}, nil
}

func (p *Pool) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	query = rewriteSQL(query)
	return &shimRow{row: p.db.QueryRowContext(ctx, query, args...)}
}

func (p *Pool) Begin(ctx context.Context) (*Tx, error) {
	t, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, translateError(err)
	}
	return &Tx{tx: t}, nil
}

// BeginTx satisfies the DesktopDB interface.
// TxOptions are ignored; SQLite only supports DEFERRED transactions.
func (p *Pool) BeginTx(ctx context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	return p.Begin(ctx)
}

func (p *Pool) Close() error {
	return p.db.Close()
}

func (p *Pool) Ping(ctx context.Context) error {
	return p.db.PingContext(ctx)
}

// Unwrap returns the underlying *sql.DB for code that needs direct access.
func (p *Pool) Unwrap() *sql.DB {
	return p.db
}
