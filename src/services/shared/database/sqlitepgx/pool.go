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
	"time"

	"github.com/google/uuid"
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
	db            *sql.DB
	writeExecutor WriteExecutor
}

// New creates a Pool backed by an existing *sql.DB handle.
func New(db *sql.DB) *Pool {
	return &Pool{db: db}
}

// NewWithWriteExecutor creates a Pool with a custom write executor.
// Pass nil to use the current global write executor.
func NewWithWriteExecutor(db *sql.DB, executor WriteExecutor) *Pool {
	return &Pool{db: db, writeExecutor: executor}
}

// WithWriteExecutor returns a lightweight wrapper that shares the same *sql.DB
// but uses the provided write executor.
func (p *Pool) WithWriteExecutor(executor WriteExecutor) *Pool {
	if p == nil {
		return nil
	}
	return &Pool{db: p.db, writeExecutor: executor}
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
	guard, err := p.acquireWriteGuard(ctx)
	if err != nil {
		return pgconn.NewCommandTag(""), err
	}
	defer guard.Release()

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
	case time.Time:
		return val.UTC().Format("2006-01-02 15:04:05.999999999")
	case []string:
		b, _ := json.Marshal(val)
		return string(b)
	case []uuid.UUID:
		out := make([]string, len(val))
		for i := range val {
			out[i] = val[i].String()
		}
		b, _ := json.Marshal(out)
		return string(b)
	case []any:
		b, _ := json.Marshal(val)
		return string(b)
	}
	return v
}

func (p *Pool) Begin(ctx context.Context) (*Tx, error) {
	guard, err := p.acquireWriteGuard(ctx)
	if err != nil {
		return nil, err
	}
	t, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		guard.Release()
		return nil, translateError(err)
	}
	return &Tx{tx: t, writeGuard: guard, writeExecutor: p.resolveWriteExecutor()}, nil
}

// BeginTx satisfies the DesktopDB interface.
// 对写事务（默认）加全局单写串行；读事务直通。
func (p *Pool) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	txOpts := &sql.TxOptions{}
	isReadOnly := opts.AccessMode == pgx.ReadOnly
	if isReadOnly {
		txOpts.ReadOnly = true
	}

	var guard WriteGuard = noopWriteGuard{}
	var err error
	if !isReadOnly {
		guard, err = p.acquireWriteGuard(ctx)
		if err != nil {
			return nil, err
		}
	}

	t, err := p.db.BeginTx(ctx, txOpts)
	if err != nil {
		guard.Release()
		return nil, translateError(err)
	}
	return &Tx{tx: t, readOnly: isReadOnly, writeGuard: guard, writeExecutor: p.resolveWriteExecutor()}, nil
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

func (p *Pool) resolveWriteExecutor() WriteExecutor {
	if p != nil && p.writeExecutor != nil {
		return p.writeExecutor
	}
	return GetGlobalWriteExecutor()
}

func (p *Pool) acquireWriteGuard(ctx context.Context) (WriteGuard, error) {
	executor := p.resolveWriteExecutor()
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
