package pipeline

import (
	"context"

	"arkloop/services/worker/internal/events"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CompactPersistDB 是 compact 持久化与相关查询所需的最小 DB 面。
// 由 *pgxpool.Pool 与 desktop SQLite 的 pgx 适配层实现。
type CompactPersistDB interface {
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// CompactRunEventAppender 与 PG / Desktop 的 run_events 仓储签名一致。
type CompactRunEventAppender interface {
	AppendRunEvent(ctx context.Context, tx pgx.Tx, runID uuid.UUID, ev events.RunEvent) (int64, error)
}
