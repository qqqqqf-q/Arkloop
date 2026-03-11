package runengine

import (
	"context"
	"testing"
	"time"

	"arkloop/services/worker/internal/data"
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

func TestCreateAndEnqueueChildRun_CreatesQueuedSubAgent(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_child_run_sub_agent")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	orgID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	userID := uuid.New()
	seedThreadAndRun(t, pool, orgID, threadID, &projectID, &userID, runID)

	parentRun := data.Run{
		ID:              runID,
		OrgID:           orgID,
		ThreadID:        threadID,
		ProjectID:       &projectID,
		CreatedByUserID: &userID,
	}
	childRunID := uuid.New()
	jobQueue := &stubJobQueue{}
	if err := createAndEnqueueChildRun(context.Background(), pool, nil, jobQueue, childRunID, parentRun, "trace-1", "researcher@1", "collect facts"); err != nil {
		t.Fatalf("createAndEnqueueChildRun: %v", err)
	}
	if len(jobQueue.enqueuedRunIDs) != 1 || jobQueue.enqueuedRunIDs[0] != childRunID {
		t.Fatalf("unexpected enqueued runs: %#v", jobQueue.enqueuedRunIDs)
	}

	repo := data.SubAgentRepository{}
	agents, err := repo.ListByParentRun(context.Background(), pool, runID)
	if err != nil {
		t.Fatalf("list sub_agents: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 sub_agent, got %d", len(agents))
	}
	agent := agents[0]
	if agent.Status != data.SubAgentStatusQueued {
		t.Fatalf("unexpected status: %s", agent.Status)
	}
	if agent.CurrentRunID == nil || *agent.CurrentRunID != childRunID {
		t.Fatalf("unexpected current_run_id: %#v", agent.CurrentRunID)
	}
	if agent.Depth != 1 {
		t.Fatalf("unexpected depth: %d", agent.Depth)
	}
	if agent.RootRunID != runID || agent.RootThreadID != threadID {
		t.Fatalf("unexpected roots: run=%s thread=%s", agent.RootRunID, agent.RootThreadID)
	}
	if agent.PersonaID == nil || *agent.PersonaID != "researcher@1" {
		t.Fatalf("unexpected persona_id: %#v", agent.PersonaID)
	}
	events, err := (data.SubAgentEventsRepository{}).ListBySubAgent(context.Background(), pool, agent.ID, 0, 10)
	if err != nil {
		t.Fatalf("list sub_agent_events: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 sub_agent_events, got %d", len(events))
	}
	if events[0].Type != data.SubAgentEventTypeSpawnRequested || events[1].Type != data.SubAgentEventTypeSpawned || events[2].Type != data.SubAgentEventTypeRunQueued {
		t.Fatalf("unexpected event order: %#v", []string{events[0].Type, events[1].Type, events[2].Type})
	}
	if got := events[1].DataJSON["run_id"]; got != childRunID.String() {
		t.Fatalf("unexpected spawned run_id: %#v", got)
	}
	if got := events[2].DataJSON["run_id"]; got != childRunID.String() {
		t.Fatalf("unexpected run_queued run_id: %#v", got)
	}
}

func TestMarkSubAgentRunning_TransitionsQueuedAgent(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_sub_agent_running")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	orgID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	userID := uuid.New()
	seedThreadAndRun(t, pool, orgID, threadID, &projectID, &userID, runID)
	parentRun := data.Run{ID: runID, OrgID: orgID, ThreadID: threadID, ProjectID: &projectID, CreatedByUserID: &userID}
	childRunID := uuid.New()
	if err := createAndEnqueueChildRun(context.Background(), pool, nil, &stubJobQueue{}, childRunID, parentRun, "trace-2", "researcher@1", "collect facts"); err != nil {
		t.Fatalf("createAndEnqueueChildRun: %v", err)
	}
	if err := markSubAgentRunning(context.Background(), pool, childRunID); err != nil {
		t.Fatalf("markSubAgentRunning: %v", err)
	}

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(context.Background())

	agent, err := (data.SubAgentRepository{}).GetByCurrentRunID(context.Background(), tx, childRunID)
	if err != nil {
		t.Fatalf("get by current_run_id: %v", err)
	}
	if agent == nil {
		t.Fatal("expected sub_agent")
	}
	if agent.Status != data.SubAgentStatusRunning {
		t.Fatalf("unexpected status: %s", agent.Status)
	}
	if agent.StartedAt == nil {
		t.Fatal("expected started_at")
	}
	events, err := (data.SubAgentEventsRepository{}).ListBySubAgent(context.Background(), pool, agent.ID, 0, 10)
	if err != nil {
		t.Fatalf("list sub_agent_events: %v", err)
	}
	if len(events) == 0 || events[len(events)-1].Type != data.SubAgentEventTypeRunStarted {
		t.Fatalf("expected final event run_started, got %#v", events)
	}
}

func TestMarkChildRunFailed_TransitionsQueuedAgent(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_sub_agent_enqueue_failed")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	orgID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	userID := uuid.New()
	seedThreadAndRun(t, pool, orgID, threadID, &projectID, &userID, runID)
	parentRun := data.Run{ID: runID, OrgID: orgID, ThreadID: threadID, ProjectID: &projectID, CreatedByUserID: &userID}
	childRunID := uuid.New()
	if err := createAndEnqueueChildRun(context.Background(), pool, nil, &stubJobQueue{}, childRunID, parentRun, "trace-3", "researcher@1", "collect facts"); err != nil {
		t.Fatalf("createAndEnqueueChildRun: %v", err)
	}
	markChildRunFailed(context.Background(), pool, nil, childRunID)

	agents, err := (data.SubAgentRepository{}).ListByParentRun(context.Background(), pool, runID)
	if err != nil {
		t.Fatalf("list sub_agents: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 sub_agent, got %d", len(agents))
	}
	if agents[0].Status != data.SubAgentStatusFailed {
		t.Fatalf("unexpected status: %s", agents[0].Status)
	}
	if agents[0].CurrentRunID != nil {
		t.Fatalf("expected current_run_id cleared, got %#v", agents[0].CurrentRunID)
	}
	if agents[0].LastError == nil || *agents[0].LastError != "failed to enqueue child run job" {
		t.Fatalf("unexpected last_error: %#v", agents[0].LastError)
	}
	events, err := (data.SubAgentEventsRepository{}).ListBySubAgent(context.Background(), pool, agents[0].ID, 0, 10)
	if err != nil {
		t.Fatalf("list sub_agent_events: %v", err)
	}
	if len(events) == 0 || events[len(events)-1].Type != data.SubAgentEventTypeFailed {
		t.Fatalf("expected final event failed, got %#v", events)
	}
	if got := events[len(events)-1].DataJSON["message"]; got != "failed to enqueue child run job" {
		t.Fatalf("unexpected failure message: %#v", got)
	}
}
