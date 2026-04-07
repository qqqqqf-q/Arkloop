package data

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type RunPipelineEventsDB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

type RunPipelineEventRecord struct {
	RunID      uuid.UUID
	AccountID  uuid.UUID
	Middleware string
	EventName  string
	Seq        int
	FieldsJSON map[string]any
}

type RunPipelineEventsWriter interface {
	InsertBatch(ctx context.Context, records []RunPipelineEventRecord) error
	DeleteOlderThan(ctx context.Context, cutoff time.Time) error
}
