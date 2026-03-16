//go:build !desktop

package pipeline

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLoadRunInputsIncludesRoleFromFirstEvent(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "pipeline_input_loader_role")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	if _, err := pool.Exec(context.Background(), `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, threadID, accountID, projectID); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO runs (id, account_id, thread_id, status) VALUES ($1, $2, $3, 'running')`, runID, accountID, threadID); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, 1, 'run.started', '{"persona_id":"p1","role":"worker"}'::jsonb)`, runID); err != nil {
		t.Fatalf("insert run event: %v", err)
	}

	inputJSON, messages, err := loadRunInputs(context.Background(), pool, data.Run{ID: runID, AccountID: accountID, ThreadID: threadID}, data.RunEventsRepository{}, data.MessagesRepository{}, 20)
	if err != nil {
		t.Fatalf("loadRunInputs failed: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("expected no messages, got %d", len(messages))
	}
	if got := inputJSON["role"]; got != "worker" {
		t.Fatalf("unexpected role: %#v", got)
	}
	if got := inputJSON["persona_id"]; got != "p1" {
		t.Fatalf("unexpected persona_id: %#v", got)
	}
}
