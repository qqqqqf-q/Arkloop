//go:build desktop

// Package sqlitepgx wraps database/sql with a SQLite backend to satisfy
// the pgx-based data.Querier interface used by API repositories.
// This allows existing repositories to work with SQLite without code changes.
package sqlitepgx

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	sqlite "modernc.org/sqlite"
)

// pragmaConnector 实现 driver.Connector，在每条新连接上执行 PRAGMA 初始化。
type pragmaConnector struct {
	dsn string
	drv driver.Driver
}

func (c *pragmaConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.drv.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	// busy_timeout: desktop shares one SQLite file across worker + API + poll;
	// 5s was too low when run.execute concurrency>1 and Telegram poll hold writes.
	for _, p := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=20000",
		"PRAGMA synchronous=NORMAL",
	} {
		if err := sqliteExecPragma(ctx, conn, p); err != nil {
			conn.Close()
			return nil, fmt.Errorf("sqlitepgx: pragma %q: %w", p, err)
		}
	}
	return conn, nil
}

func (c *pragmaConnector) Driver() driver.Driver { return c.drv }

func sqliteExecPragma(ctx context.Context, conn driver.Conn, pragma string) error {
	if ec, ok := conn.(driver.ExecerContext); ok {
		_, err := ec.ExecContext(ctx, pragma, nil)
		return err
	}
	stmt, err := conn.Prepare(pragma)
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(nil)
	return err
}

func openSQLiteDB(dsn string) (*sql.DB, error) {
	return sql.OpenDB(&pragmaConnector{dsn: dsn, drv: &sqlite.Driver{}}), nil
}

// Pool wraps *sql.DB to satisfy the pgx-based data.Querier interface.
type Pool struct {
	db *sql.DB
}

// New creates a Pool backed by an existing *sql.DB handle.
func New(db *sql.DB) *Pool {
	return &Pool{db: db}
}

// DesktopMaxOpenConns and DesktopMaxIdleConns match the limits used by Open
// so the desktop API path can share one *sql.DB with the Worker without a
// second pool fighting the same file.
const (
	DesktopMaxOpenConns = 5
	DesktopMaxIdleConns = 2
)

// ConfigureDesktopSQLPool applies [DesktopMaxOpenConns] and [DesktopMaxIdleConns]
// to an existing *sql.DB (e.g. after sqliteadapter.AutoMigrate). Pragma setup
// must already have been applied on this DB.
func ConfigureDesktopSQLPool(db *sql.DB) {
	if db == nil {
		return
	}
	db.SetMaxOpenConns(DesktopMaxOpenConns)
	db.SetMaxIdleConns(DesktopMaxIdleConns)
}

// Open opens a SQLite database with sensible defaults for an embedded
// single-writer workload (WAL, foreign keys, busy timeout, etc.).
func Open(dsn string) (*Pool, error) {
	db, err := openSQLiteDB(dsn)
	if err != nil {
		return nil, err
	}
	ConfigureDesktopSQLPool(db)
	return &Pool{db: db}, nil
}

func (p *Pool) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	query = rewriteSQL(query)
	query, args = expandAnyArgs(query, args)
	args = convertArgs(args)
	r, err := p.db.ExecContext(ctx, query, args...)
	if err != nil {
		return pgconn.NewCommandTag(""), translateError(err)
	}
	n, _ := r.RowsAffected()
	return pgconn.NewCommandTag(fmt.Sprintf("OK %d", n)), nil
}

func (p *Pool) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	query = rewriteSQL(query)
	query, args = expandAnyArgs(query, args)
	args = convertArgs(args)
	r, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return &shimRows{err: translateError(err)}, nil
	}
	return &shimRows{rows: r}, nil
}

func (p *Pool) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	query = rewriteSQL(query)
	query, args = expandAnyArgs(query, args)
	args = convertArgs(args)
	return &shimRow{row: p.db.QueryRowContext(ctx, query, args...)}
}

// convertArgs converts Go types that database/sql / SQLite do not natively
// support into compatible types:
//   - []string  → JSON array string  (e.g. `["a","b"]`)
//   - []byte    → passed as-is (already handled by database/sql)
//   - map/slice → JSON string fallback
func convertArgs(args []any) []any {
	result := make([]any, len(args))
	for i, a := range args {
		result[i] = convertArg(a)
	}
	return result
}

func convertArg(v any) any {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case []string:
		b, _ := json.Marshal(val)
		return string(b)
	case []any:
		b, _ := json.Marshal(val)
		return string(b)
	}
	return v
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
