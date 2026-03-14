//go:build !desktop

package pipeline

import (
	"context"
	"testing"
	"time"

	creditpolicy "arkloop/services/shared/creditpolicy"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/queue"
	"arkloop/services/worker/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type stubJobQueue struct {
	enqueuedRunIDs []uuid.UUID
}

func (s *stubJobQueue) EnqueueRun(ctx context.Context, orgID uuid.UUID, runID uuid.UUID, traceID string, queueJobType string, payload map[string]any, availableAt *time.Time) (uuid.UUID, error) {
	s.enqueuedRunIDs = append(s.enqueuedRunIDs, runID)
	return uuid.New(), nil
}

func (s *stubJobQueue) Lease(context.Context, int, []string) (*queue.JobLease, error) {
	return nil, nil
}
func (s *stubJobQueue) Heartbeat(context.Context, queue.JobLease, int) error { return nil }
func (s *stubJobQueue) Ack(context.Context, queue.JobLease) error            { return nil }
func (s *stubJobQueue) Nack(context.Context, queue.JobLease, *int) error     { return nil }

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

	writer := newEventWriter(pool, data.Run{ID: runID, OrgID: orgID, ThreadID: threadID}, "trace-1", nil, nil, "", "", data.UsageRecordsRepository{}, data.CreditsRepository{}, 1000, 1, nil, nil, nil, nil, creditpolicy.DefaultPolicy)
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

	writer := newEventWriter(pool, data.Run{ID: runID, OrgID: orgID, ThreadID: threadID}, "trace-2", nil, nil, "", "", data.UsageRecordsRepository{}, data.CreditsRepository{}, 1000, 1, nil, nil, nil, nil, creditpolicy.DefaultPolicy)
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

func TestEventWriterAppend_AutoQueuesNextRunFromPendingInputs(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_sub_agent_pending_autorun")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	orgID := uuid.New()
	projectID := uuid.New()
	parentThreadID := uuid.New()
	childThreadID := uuid.New()
	parentRunID := uuid.New()
	childRunID := uuid.New()
	subAgentID := uuid.New()
	seedPipelineThread(t, pool, orgID, parentThreadID, projectID)
	seedPipelineThread(t, pool, orgID, childThreadID, projectID)
	seedPipelineRun(t, pool, orgID, parentThreadID, parentRunID, nil)
	seedPipelineRun(t, pool, orgID, childThreadID, childRunID, &parentRunID)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO sub_agents (
			id, org_id, parent_run_id, parent_thread_id, root_run_id, root_thread_id,
			depth, source_type, context_mode, status, current_run_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, subAgentID, orgID, parentRunID, parentThreadID, parentRunID, parentThreadID, 1, data.SubAgentSourceTypeThreadSpawn, data.SubAgentContextModeIsolated, data.SubAgentStatusRunning, childRunID)
	if err != nil {
		t.Fatalf("insert sub_agent: %v", err)
	}
	_, err = pool.Exec(context.Background(), `INSERT INTO sub_agent_pending_inputs (sub_agent_id, input, priority) VALUES ($1, $2, $3), ($1, $4, $5)`, subAgentID, "phase two", false, "urgent", true)
	if err != nil {
		t.Fatalf("insert pending inputs: %v", err)
	}

	jobQueue := &stubJobQueue{}
	writer := newEventWriter(pool, data.Run{ID: childRunID, OrgID: orgID, ThreadID: childThreadID, ParentRunID: &parentRunID}, "trace-3", nil, jobQueue, "", "", data.UsageRecordsRepository{}, data.CreditsRepository{}, 1000, 1, nil, nil, nil, nil, creditpolicy.DefaultPolicy)
	ev := events.NewEmitter("trace-3").Emit("run.completed", map[string]any{}, nil, nil)
	if err := writer.Append(context.Background(), data.RunsRepository{}, data.RunEventsRepository{}, childRunID, ev); err != nil {
		t.Fatalf("append terminal event: %v", err)
	}
	if err := writer.Flush(context.Background()); err != nil {
		t.Fatalf("flush events: %v", err)
	}
	if len(jobQueue.enqueuedRunIDs) != 1 {
		t.Fatalf("expected 1 auto-enqueued run, got %d", len(jobQueue.enqueuedRunIDs))
	}

	agent, err := (data.SubAgentRepository{}).ListByParentRun(context.Background(), pool, parentRunID)
	if err != nil {
		t.Fatalf("list sub_agents: %v", err)
	}
	if len(agent) != 1 || agent[0].CurrentRunID == nil || *agent[0].CurrentRunID != jobQueue.enqueuedRunIDs[0] {
		t.Fatalf("unexpected projected sub_agent: %#v", agent)
	}
	var merged string
	if err := pool.QueryRow(context.Background(), `
		SELECT content FROM messages
		 WHERE thread_id = $1 AND role = 'user'
		 ORDER BY created_at DESC, id DESC
		 LIMIT 1`, childThreadID).Scan(&merged); err != nil {
		t.Fatalf("load merged input: %v", err)
	}
	if merged != "urgent\n\nphase two" {
		t.Fatalf("unexpected merged input: %q", merged)
	}
}

func seedPipelineThread(t *testing.T, pool *pgxpool.Pool, orgID, threadID, projectID uuid.UUID) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `INSERT INTO threads (id, account_id, project_id) VALUES ($1, $2, $3)`, threadID, orgID, projectID)
	if err != nil {
		t.Fatalf("insert thread: %v", err)
	}
}

func seedPipelineRun(t *testing.T, pool *pgxpool.Pool, orgID, threadID, runID uuid.UUID, parentRunID *uuid.UUID) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `INSERT INTO runs (id, account_id, thread_id, parent_run_id, status) VALUES ($1, $2, $3, $4, 'running')`, runID, orgID, threadID, parentRunID)
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
