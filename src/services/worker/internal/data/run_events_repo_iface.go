package data

import (
	"context"

	workerevents "arkloop/services/worker/internal/events"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type RunEventStore interface {
	GetLatestEventType(ctx context.Context, tx pgx.Tx, runID uuid.UUID, types []string) (string, error)
	AppendRunEvent(ctx context.Context, tx pgx.Tx, runID uuid.UUID, ev workerevents.RunEvent) (int64, error)
}
