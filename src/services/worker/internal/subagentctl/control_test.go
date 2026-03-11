package subagentctl

import (
	"context"
	"testing"
	"time"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/queue"
	"arkloop/services/worker/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

func TestServiceSpawnAndWaitCompleted(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_subagentctl_spawn_wait")
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
	jobQueue := &stubJobQueue{}
	service := NewService(pool, nil, jobQueue, parentRun, "trace-1")

	snapshot, err := service.Spawn(context.Background(), SpawnRequest{PersonaID: "researcher@1", Input: "collect facts"})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if snapshot.Status != data.SubAgentStatusQueued {
		t.Fatalf("unexpected status: %s", snapshot.Status)
	}
	if snapshot.CurrentRunID == nil {
		t.Fatal("expected current_run_id")
	}
	completeSubAgentRun(t, pool, snapshot.SubAgentID, *snapshot.CurrentRunID, "done")

	resolved, err := service.Wait(context.Background(), WaitRequest{SubAgentID: snapshot.SubAgentID, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if resolved.Status != data.SubAgentStatusCompleted {
		t.Fatalf("unexpected resolved status: %s", resolved.Status)
	}
	if resolved.LastOutput == nil || *resolved.LastOutput != "done" {
		t.Fatalf("unexpected output: %#v", resolved.LastOutput)
	}
	if resolved.LastOutputRef == nil || *resolved.LastOutputRef == "" {
		t.Fatalf("expected output ref, got %#v", resolved.LastOutputRef)
	}
}

func TestServiceSendInputAndResume(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_subagentctl_resume")
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
	jobQueue := &stubJobQueue{}
	service := NewService(pool, nil, jobQueue, parentRun, "trace-2")

	snapshot, err := service.Spawn(context.Background(), SpawnRequest{PersonaID: "researcher@1", Input: "phase one"})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	completeSubAgentRun(t, pool, snapshot.SubAgentID, *snapshot.CurrentRunID, "phase one done")

	inputSnapshot, err := service.SendInput(context.Background(), SendInputRequest{SubAgentID: snapshot.SubAgentID, Input: "phase two"})
	if err != nil {
		t.Fatalf("send_input: %v", err)
	}
	if inputSnapshot.Status != data.SubAgentStatusResumable {
		t.Fatalf("unexpected send_input status: %s", inputSnapshot.Status)
	}

	resumed, err := service.Resume(context.Background(), ResumeRequest{SubAgentID: snapshot.SubAgentID})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if resumed.Status != data.SubAgentStatusQueued {
		t.Fatalf("unexpected resumed status: %s", resumed.Status)
	}
	if resumed.CurrentRunID == nil {
		t.Fatal("expected resumed current_run_id")
	}
	if len(jobQueue.enqueuedRunIDs) != 2 {
		t.Fatalf("expected 2 enqueued runs, got %d", len(jobQueue.enqueuedRunIDs))
	}
}

func completeSubAgentRun(t *testing.T, pool *pgxpool.Pool, subAgentID uuid.UUID, runID uuid.UUID, output string) {
	t.Helper()
	if err := MarkRunning(context.Background(), pool, runID); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(context.Background())
	if err := (data.SubAgentRepository{}).TransitionToTerminal(context.Background(), tx, runID, data.SubAgentStatusCompleted, nil); err != nil {
		t.Fatalf("transition terminal: %v", err)
	}
	orgID, threadID := mustRunContext(t, tx, subAgentID)
	messageID, err := (data.MessagesRepository{}).InsertAssistantMessage(context.Background(), tx, orgID, threadID, runID, output)
	if err != nil {
		t.Fatalf("insert assistant message: %v", err)
	}
	if err := (data.SubAgentRepository{}).SetLastOutputRefByLastCompletedRunID(context.Background(), tx, runID, "message:"+messageID.String()); err != nil {
		t.Fatalf("set output ref: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit tx: %v", err)
	}
}

func mustRunContext(t *testing.T, tx pgx.Tx, subAgentID uuid.UUID) (uuid.UUID, uuid.UUID) {
	t.Helper()
	var (
		orgID    uuid.UUID
		threadID uuid.UUID
	)
	if err := tx.QueryRow(context.Background(), `
		SELECT sa.org_id, r.thread_id
		  FROM sub_agents sa
		  JOIN runs r ON r.id = sa.last_completed_run_id
		 WHERE sa.id = $1`, subAgentID).Scan(&orgID, &threadID); err != nil {
		t.Fatalf("load sub_agent org/thread: %v", err)
	}
	return orgID, threadID
}
func seedThreadAndRun(t *testing.T, pool *pgxpool.Pool, orgID, threadID uuid.UUID, projectID, userID *uuid.UUID, runID uuid.UUID) {
	t.Helper()
	_, err := pool.Exec(
		context.Background(),
		`INSERT INTO threads (id, org_id, created_by_user_id, project_id)
		 VALUES ($1, $2, $3, $4)`,
		threadID,
		orgID,
		userID,
		projectID,
	)
	if err != nil {
		t.Fatalf("insert thread failed: %v", err)
	}
	_, err = pool.Exec(
		context.Background(),
		`INSERT INTO runs (id, org_id, thread_id, created_by_user_id, status)
		 VALUES ($1, $2, $3, $4, 'running')`,
		runID,
		orgID,
		threadID,
		userID,
	)
	if err != nil {
		t.Fatalf("insert run failed: %v", err)
	}
}
