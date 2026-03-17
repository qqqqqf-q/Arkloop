package data

import (
	"context"
	"testing"
	"time"

	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/testutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRunEventsRepository_AppendRunEventPreservesOccurredAt(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_run_events_occurred_at")
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	runID := uuid.New()
	accountID := uuid.New()
	threadID := uuid.New()

	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`, runID, accountID, threadID); err != nil {
		t.Fatalf("insert run: %v", err)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	ev := events.NewEmitter("trace-occurred-at").Emit("llm.request", map[string]any{
		"llm_call_id": "call-1",
	}, nil, nil)
	ev.OccurredAt = time.Date(2026, time.March, 17, 8, 16, 17, 987654000, time.UTC)

	repo := RunEventsRepository{}
	if _, err := repo.AppendRunEvent(ctx, tx, runID, ev); err != nil {
		t.Fatalf("append run event: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	var got time.Time
	if err := pool.QueryRow(ctx, `SELECT ts FROM run_events WHERE run_id = $1 AND seq = 1`, runID).Scan(&got); err != nil {
		t.Fatalf("query ts: %v", err)
	}

	if !got.UTC().Equal(ev.OccurredAt) {
		t.Fatalf("unexpected ts: got %s want %s", got.UTC().Format(time.RFC3339Nano), ev.OccurredAt.Format(time.RFC3339Nano))
	}
}
