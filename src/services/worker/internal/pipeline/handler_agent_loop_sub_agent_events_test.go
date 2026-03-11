package pipeline

import (
	"context"
	"testing"

	creditpolicy "arkloop/services/shared/creditpolicy"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEventWriterAppend_AppendsSubAgentCompletedEvent(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_sub_agent_completed_event")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	orgID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	subAgentID := uuid.New()
	seedPipelineThread(t, pool, orgID, threadID, projectID)
	seedPipelineRun(t, pool, orgID, threadID, runID, nil)
	seedPipelineSubAgent(t, pool, subAgentID, orgID, threadID, runID)

	writer := newEventWriter(pool, data.Run{ID: runID, OrgID: orgID, ThreadID: threadID}, "trace-1", nil, "", "", data.UsageRecordsRepository{}, data.CreditsRepository{}, 1000, 1, nil, nil, nil, nil, creditpolicy.DefaultPolicy)
	ev := events.NewEmitter("trace-1").Emit("run.completed", map[string]any{}, nil, nil)
	if err := writer.Append(context.Background(), data.RunsRepository{}, data.RunEventsRepository{}, runID, ev); err != nil {
		t.Fatalf("append terminal event: %v", err)
	}
	if err := writer.Flush(context.Background()); err != nil {
		t.Fatalf("flush events: %v", err)
	}

	eventsList, err := (data.SubAgentEventsRepository{}).ListBySubAgent(context.Background(), pool, subAgentID, 0, 10)
	if err != nil {
		t.Fatalf("list sub_agent_events: %v", err)
	}
	if len(eventsList) != 1 || eventsList[0].Type != data.SubAgentEventTypeCompleted {
		t.Fatalf("unexpected sub_agent_events: %#v", eventsList)
	}
}

func TestEventWriterAppend_AppendsSubAgentCancelledEventOnCancelRequest(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_sub_agent_cancelled_event")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	orgID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	subAgentID := uuid.New()
	seedPipelineThread(t, pool, orgID, threadID, projectID)
	seedPipelineRun(t, pool, orgID, threadID, runID, nil)
	seedPipelineSubAgent(t, pool, subAgentID, orgID, threadID, runID)

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := (data.RunEventsRepository{}).AppendEvent(context.Background(), tx, runID, "run.cancel_requested", map[string]any{}, nil, nil); err != nil {
		t.Fatalf("append cancel_requested: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit cancel_requested: %v", err)
	}

	writer := newEventWriter(pool, data.Run{ID: runID, OrgID: orgID, ThreadID: threadID}, "trace-2", nil, "", "", data.UsageRecordsRepository{}, data.CreditsRepository{}, 1000, 1, nil, nil, nil, nil, creditpolicy.DefaultPolicy)
	ev := events.NewEmitter("trace-2").Emit("message.delta", map[string]any{"content_delta": "ignored"}, nil, nil)
	if err := writer.Append(context.Background(), data.RunsRepository{}, data.RunEventsRepository{}, runID, ev); err != nil {
		t.Fatalf("append after cancel request: %v", err)
	}

	eventsList, err := (data.SubAgentEventsRepository{}).ListBySubAgent(context.Background(), pool, subAgentID, 0, 10)
	if err != nil {
		t.Fatalf("list sub_agent_events: %v", err)
	}
	if len(eventsList) != 1 || eventsList[0].Type != data.SubAgentEventTypeCancelled {
		t.Fatalf("unexpected sub_agent_events: %#v", eventsList)
	}
}

func seedPipelineThread(t *testing.T, pool *pgxpool.Pool, orgID, threadID, projectID uuid.UUID) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `INSERT INTO threads (id, org_id, project_id) VALUES ($1, $2, $3)`, threadID, orgID, projectID)
	if err != nil {
		t.Fatalf("insert thread: %v", err)
	}
}

func seedPipelineRun(t *testing.T, pool *pgxpool.Pool, orgID, threadID, runID uuid.UUID, parentRunID *uuid.UUID) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `INSERT INTO runs (id, org_id, thread_id, parent_run_id, status) VALUES ($1, $2, $3, $4, 'running')`, runID, orgID, threadID, parentRunID)
	if err != nil {
		t.Fatalf("insert run: %v", err)
	}
}

func seedPipelineSubAgent(t *testing.T, pool *pgxpool.Pool, subAgentID, orgID, threadID, runID uuid.UUID) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO sub_agents (
			id, org_id, parent_run_id, parent_thread_id, root_run_id, root_thread_id,
			depth, source_type, context_mode, status, current_run_id
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11
		)
	`, subAgentID, orgID, runID, threadID, runID, threadID, 1, data.SubAgentSourceTypeThreadSpawn, data.SubAgentContextModeIsolated, data.SubAgentStatusRunning, runID)
	if err != nil {
		t.Fatalf("insert sub_agent: %v", err)
	}
}
