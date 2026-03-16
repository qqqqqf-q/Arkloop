package data

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// TxStarter provides transaction support. Both *pgxpool.Pool (PostgreSQL)
// and *sqlitepgx.Pool (desktop SQLite) satisfy this interface.
type TxStarter interface {
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

// DB combines Querier with transaction support.
type DB interface {
	Querier
	TxStarter
}
