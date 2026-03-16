package database

import "context"

// DB abstracts a database connection pool.
// Both PostgreSQL (pgxpool.Pool) and SQLite can implement this interface.
//
// TODO(m4): Consider adding BeginTx(ctx context.Context, opts TxOptions) (Tx, error)
// for callers that need isolation-level or read-only transaction options.
// Currently all Begin callers use default options.
type DB interface {
	Querier
	Begin(ctx context.Context) (Tx, error)
	Close() error
	Ping(ctx context.Context) error
}

// Tx abstracts a database transaction.
type Tx interface {
	Querier
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// Querier is the shared query interface satisfied by both DB and Tx.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (Result, error)
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) Row
}

// Row abstracts a single-row query result.
type Row interface {
	Scan(dest ...any) error
}

// Rows abstracts a multi-row query result set.
//
// TODO(m2): Close() currently returns no error. Changing to Close() error
// would be a breaking change across 80+ call sites that use `defer rows.Close()`.
// Evaluate in a future pass if error propagation from Close is needed.
type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Close()
	Err() error
}

// Result abstracts the outcome of an Exec operation.
//
// TODO(m3): LastInsertId() is intentionally omitted. PostgreSQL does not
// support it (uses RETURNING instead) and no existing code needs it.
// Add LastInsertId() (int64, error) if a use case arises.
type Result interface {
	RowsAffected() int64
}
