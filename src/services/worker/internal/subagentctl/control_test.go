package subagentctl

import (
	"context"
	"encoding/json"
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

func (s *stubJobQueue) EnqueueRun(ctx context.Context, accountID uuid.UUID, runID uuid.UUID, traceID string, queueJobType string, payload map[string]any, availableAt *time.Time) (uuid.UUID, error) {
	s.enqueuedRunIDs = append(s.enqueuedRunIDs, runID)
	return uuid.New(), nil
}
func (s *stubJobQueue) Lease(context.Context, int, []string) (*queue.JobLease, error) {
	return nil, nil
}
func (s *stubJobQueue) Heartbeat(context.Context, queue.JobLease, int) error { return nil }
func (s *stubJobQueue) Ack(context.Context, queue.JobLease) error            { return nil }
func (s *stubJobQueue) Nack(context.Context, queue.JobLease, *int) error     { return nil }

func isolatedSpawnRequest(input string) SpawnRequest {
	return SpawnRequest{
		PersonaID:   "researcher@1",
		ContextMode: data.SubAgentContextModeIsolated,
		Input:       input,
	}
}

func TestServiceSpawnAndWaitCompleted(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_subagentctl_spawn_wait")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	userID := uuid.New()
	seedThreadAndRun(t, pool, accountID, threadID, &projectID, &userID, runID)

	parentRun := data.Run{ID: runID, AccountID: accountID, ThreadID: threadID, ProjectID: &projectID, CreatedByUserID: &userID}
	jobQueue := &stubJobQueue{}
	service := NewService(pool, nil, jobQueue, parentRun, "trace-1", SubAgentLimits{}, BackpressureConfig{})

	snapshot, err := service.Spawn(context.Background(), isolatedSpawnRequest("collect facts"))
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

func TestServiceSendInputCreatesQueuedRunDirectly(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_subagentctl_resume")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	userID := uuid.New()
	seedThreadAndRun(t, pool, accountID, threadID, &projectID, &userID, runID)

	parentRun := data.Run{ID: runID, AccountID: accountID, ThreadID: threadID, ProjectID: &projectID, CreatedByUserID: &userID}
	jobQueue := &stubJobQueue{}
	service := NewService(pool, nil, jobQueue, parentRun, "trace-2", SubAgentLimits{}, BackpressureConfig{})

	snapshot, err := service.Spawn(context.Background(), isolatedSpawnRequest("phase one"))
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	completeSubAgentRun(t, pool, snapshot.SubAgentID, *snapshot.CurrentRunID, "phase one done")

	inputSnapshot, err := service.SendInput(context.Background(), SendInputRequest{SubAgentID: snapshot.SubAgentID, Input: "phase two"})
	if err != nil {
		t.Fatalf("send_input: %v", err)
	}
	if inputSnapshot.Status != data.SubAgentStatusQueued {
		t.Fatalf("unexpected send_input status: %s", inputSnapshot.Status)
	}
	if inputSnapshot.CurrentRunID == nil {
		t.Fatal("expected send_input current_run_id")
	}
	if len(jobQueue.enqueuedRunIDs) != 2 {
		t.Fatalf("expected 2 enqueued runs, got %d", len(jobQueue.enqueuedRunIDs))
	}
}

func TestServiceResumeRequeuesCompletedSubAgent(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_subagentctl_resume_completed")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	userID := uuid.New()
	seedThreadAndRun(t, pool, accountID, threadID, &projectID, &userID, runID)

	parentRun := data.Run{ID: runID, AccountID: accountID, ThreadID: threadID, ProjectID: &projectID, CreatedByUserID: &userID}
	jobQueue := &stubJobQueue{}
	service := NewService(pool, nil, jobQueue, parentRun, "trace-2b", SubAgentLimits{}, BackpressureConfig{})

	snapshot, err := service.Spawn(context.Background(), isolatedSpawnRequest("phase one"))
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	completeSubAgentRun(t, pool, snapshot.SubAgentID, *snapshot.CurrentRunID, "phase one done")

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

func TestServiceSendInputQueuesRunningSubAgentAndMergesPendingBatch(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_subagentctl_pending_batch")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	userID := uuid.New()
	seedThreadAndRun(t, pool, accountID, threadID, &projectID, &userID, runID)

	parentRun := data.Run{ID: runID, AccountID: accountID, ThreadID: threadID, ProjectID: &projectID, CreatedByUserID: &userID}
	jobQueue := &stubJobQueue{}
	service := NewService(pool, nil, jobQueue, parentRun, "trace-3", SubAgentLimits{}, BackpressureConfig{})

	snapshot, err := service.Spawn(context.Background(), isolatedSpawnRequest("phase one"))
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if err := MarkRunning(context.Background(), pool, *snapshot.CurrentRunID); err != nil {
		t.Fatalf("mark running: %v", err)
	}

	queued, err := service.SendInput(context.Background(), SendInputRequest{SubAgentID: snapshot.SubAgentID, Input: "phase two"})
	if err != nil {
		t.Fatalf("send_input queued: %v", err)
	}
	if queued.Status != data.SubAgentStatusRunning {
		t.Fatalf("unexpected queued status: %s", queued.Status)
	}
	queued, err = service.SendInput(context.Background(), SendInputRequest{SubAgentID: snapshot.SubAgentID, Input: "urgent", Interrupt: true})
	if err != nil {
		t.Fatalf("send_input interrupt: %v", err)
	}
	if queued.Status != data.SubAgentStatusRunning {
		t.Fatalf("unexpected interrupt status: %s", queued.Status)
	}
	if len(jobQueue.enqueuedRunIDs) != 1 {
		t.Fatalf("expected only initial enqueue, got %d", len(jobQueue.enqueuedRunIDs))
	}

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	childRun, err := (data.RunsRepository{}).GetRun(context.Background(), tx, *snapshot.CurrentRunID)
	if err != nil {
		t.Fatalf("get child run: %v", err)
	}
	if childRun == nil {
		t.Fatal("expected child run")
	}
	nextRunID, err := service.projector.ProjectRunTerminal(context.Background(), tx, *childRun, data.SubAgentStatusCancelled, map[string]any{"message": "cancelled"}, nil)
	if err != nil {
		t.Fatalf("project terminal: %v", err)
	}
	if nextRunID == nil {
		t.Fatal("expected next run id")
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	resolved, err := service.GetStatus(context.Background(), snapshot.SubAgentID)
	if err != nil {
		t.Fatalf("get status: %v", err)
	}
	if resolved.Status != data.SubAgentStatusQueued {
		t.Fatalf("unexpected resolved status: %s", resolved.Status)
	}
	if resolved.CurrentRunID == nil || *resolved.CurrentRunID != *nextRunID {
		t.Fatalf("unexpected current_run_id: %#v", resolved.CurrentRunID)
	}

	var merged string
	if err := pool.QueryRow(context.Background(), `
		SELECT content
		  FROM messages
		 WHERE thread_id = (
		 	SELECT thread_id FROM runs WHERE id = $1
		 )
		   AND role = 'user'
		 ORDER BY created_at DESC, id DESC
		 LIMIT 1`, *nextRunID).Scan(&merged); err != nil {
		t.Fatalf("load merged content: %v", err)
	}
	if merged != "urgent\n\nphase two" {
		t.Fatalf("unexpected merged content: %q", merged)
	}

	var pendingCount int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM sub_agent_pending_inputs WHERE sub_agent_id = $1`, snapshot.SubAgentID).Scan(&pendingCount); err != nil {
		t.Fatalf("count pending inputs: %v", err)
	}
	if pendingCount != 0 {
		t.Fatalf("expected pending queue drained, got %d", pendingCount)
	}
}

func TestServiceSpawnForkRecentCopiesHistoryAndStoresSnapshot(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_subagentctl_fork_recent")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	userID := uuid.New()
	seedThreadAndRun(t, pool, accountID, threadID, &projectID, &userID, runID)
	seedThreadMessages(t, pool, accountID, threadID, []string{"u1", "a1", "u2", "a2", "u3", "a3", "u4", "a4", "u5", "a5", "u6", "a6", "u7", "a7"})

	profileRef := "profile_parent"
	workspaceRef := "workspace_parent"
	if _, err := pool.Exec(context.Background(), `UPDATE runs SET profile_ref = $2, workspace_ref = $3 WHERE id = $1`, runID, profileRef, workspaceRef); err != nil {
		t.Fatalf("update parent bindings: %v", err)
	}

	parentRun := data.Run{ID: runID, AccountID: accountID, ThreadID: threadID, ProjectID: &projectID, CreatedByUserID: &userID, ProfileRef: &profileRef, WorkspaceRef: &workspaceRef}
	service := NewService(pool, nil, &stubJobQueue{}, parentRun, "trace-fork", SubAgentLimits{}, BackpressureConfig{})

	role := "worker"
	nickname := "Atlas"
	snapshot, err := service.Spawn(context.Background(), SpawnRequest{
		PersonaID:   "researcher@1",
		Role:        &role,
		Nickname:    &nickname,
		ContextMode: data.SubAgentContextModeForkRecent,
		Input:       "summarize it",
		ParentContext: SpawnParentContext{
			ToolAllowlist: []string{"browser", "web_fetch"},
			ToolDenylist:  []string{"memory_write"},
			RouteID:       "route_parent",
			Model:         "gpt-4.1",
			ProfileRef:    profileRef,
			WorkspaceRef:  workspaceRef,
			MemoryScope:   MemoryScopeSameUser,
		},
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if snapshot.Role == nil || *snapshot.Role != role {
		t.Fatalf("unexpected role: %#v", snapshot.Role)
	}
	if snapshot.ContextMode != data.SubAgentContextModeForkRecent {
		t.Fatalf("unexpected context mode: %s", snapshot.ContextMode)
	}

	var childThreadID uuid.UUID
	if err := pool.QueryRow(context.Background(), `SELECT thread_id FROM runs WHERE id = $1`, *snapshot.CurrentRunID).Scan(&childThreadID); err != nil {
		t.Fatalf("load child thread: %v", err)
	}
	rows, err := pool.Query(context.Background(), `SELECT content FROM messages WHERE thread_id = $1 ORDER BY created_at ASC, id ASC`, childThreadID)
	if err != nil {
		t.Fatalf("load child messages: %v", err)
	}
	defer rows.Close()
	contents := make([]string, 0)
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			t.Fatalf("scan child message: %v", err)
		}
		contents = append(contents, content)
	}
	expected := []string{"u2", "a2", "u3", "a3", "u4", "a4", "u5", "a5", "u6", "a6", "u7", "a7", "summarize it"}
	if len(contents) != len(expected) {
		t.Fatalf("unexpected child message count: %#v", contents)
	}
	for idx := range expected {
		if contents[idx] != expected[idx] {
			t.Fatalf("unexpected child messages: %#v", contents)
		}
	}

	var raw []byte
	if err := pool.QueryRow(context.Background(), `SELECT snapshot_json FROM sub_agent_context_snapshots WHERE sub_agent_id = $1`, snapshot.SubAgentID).Scan(&raw); err != nil {
		t.Fatalf("load snapshot_json: %v", err)
	}
	var stored ContextSnapshot
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("unmarshal snapshot_json: %v", err)
	}
	if stored.Runtime.RouteID != "route_parent" {
		t.Fatalf("unexpected stored route: %#v", stored.Runtime)
	}
	if stored.Environment.WorkspaceRef != workspaceRef {
		t.Fatalf("unexpected stored environment: %#v", stored.Environment)
	}
	if len(stored.Messages) != 12 {
		t.Fatalf("unexpected stored message count: %d", len(stored.Messages))
	}
}

func TestServiceSpawnSharedWorkspaceOnlyOmitsHistory(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_subagentctl_shared_workspace")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	userID := uuid.New()
	seedThreadAndRun(t, pool, accountID, threadID, &projectID, &userID, runID)
	seedThreadMessages(t, pool, accountID, threadID, []string{"parent one", "parent two"})

	profileRef := "profile_parent"
	workspaceRef := "workspace_parent"
	if _, err := pool.Exec(context.Background(), `UPDATE runs SET profile_ref = $2, workspace_ref = $3 WHERE id = $1`, runID, profileRef, workspaceRef); err != nil {
		t.Fatalf("update parent bindings: %v", err)
	}

	parentRun := data.Run{ID: runID, AccountID: accountID, ThreadID: threadID, ProjectID: &projectID, CreatedByUserID: &userID, ProfileRef: &profileRef, WorkspaceRef: &workspaceRef}
	service := NewService(pool, nil, &stubJobQueue{}, parentRun, "trace-shared", SubAgentLimits{}, BackpressureConfig{})

	snapshot, err := service.Spawn(context.Background(), SpawnRequest{
		PersonaID:   "researcher@1",
		ContextMode: data.SubAgentContextModeSharedWorkspaceOnly,
		Input:       "run build",
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	var (
		childThreadID uuid.UUID
		gotProfile    *string
		gotWorkspace  *string
	)
	if err := pool.QueryRow(context.Background(), `SELECT thread_id, profile_ref, workspace_ref FROM runs WHERE id = $1`, *snapshot.CurrentRunID).Scan(&childThreadID, &gotProfile, &gotWorkspace); err != nil {
		t.Fatalf("load child run: %v", err)
	}
	if gotProfile == nil || *gotProfile != profileRef || gotWorkspace == nil || *gotWorkspace != workspaceRef {
		t.Fatalf("unexpected inherited bindings: %#v %#v", gotProfile, gotWorkspace)
	}

	var count int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM messages WHERE thread_id = $1`, childThreadID).Scan(&count); err != nil {
		t.Fatalf("count child messages: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected only explicit input, got %d", count)
	}
	var content string
	if err := pool.QueryRow(context.Background(), `SELECT content FROM messages WHERE thread_id = $1 LIMIT 1`, childThreadID).Scan(&content); err != nil {
		t.Fatalf("load child message: %v", err)
	}
	if content != "run build" {
		t.Fatalf("unexpected child message: %q", content)
	}
}

func TestServiceSpawnWritesRoleToChildRunStartedEvent(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_subagentctl_spawn_role")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	userID := uuid.New()
	seedThreadAndRun(t, pool, accountID, threadID, &projectID, &userID, runID)

	parentRun := data.Run{ID: runID, AccountID: accountID, ThreadID: threadID, ProjectID: &projectID, CreatedByUserID: &userID}
	service := NewService(pool, nil, &stubJobQueue{}, parentRun, "trace-role", SubAgentLimits{}, BackpressureConfig{})
	role := "worker"

	snapshot, err := service.Spawn(context.Background(), SpawnRequest{
		PersonaID:   "researcher@1",
		Role:        &role,
		ContextMode: data.SubAgentContextModeIsolated,
		Input:       "collect facts",
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if snapshot.CurrentRunID == nil {
		t.Fatal("expected current_run_id")
	}

	var roleValue string
	err = pool.QueryRow(context.Background(), `SELECT data_json->>'role' FROM run_events WHERE run_id = $1 AND seq = 1`, *snapshot.CurrentRunID).Scan(&roleValue)
	if err != nil {
		t.Fatalf("load run.started role: %v", err)
	}
	if roleValue != role {
		t.Fatalf("unexpected role in run.started: %q", roleValue)
	}
}

func seedThreadMessages(t *testing.T, pool *pgxpool.Pool, accountID uuid.UUID, threadID uuid.UUID, contents []string) {
	t.Helper()
	for idx, content := range contents {
		role := "user"
		if idx%2 == 1 {
			role = "assistant"
		}
		if _, err := pool.Exec(context.Background(), `INSERT INTO messages (account_id, thread_id, role, content) VALUES ($1, $2, $3, $4)`, accountID, threadID, role, content); err != nil {
			t.Fatalf("insert message %q failed: %v", content, err)
		}
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
	accountID, threadID := mustRunContext(t, tx, subAgentID)
	messageID, err := (data.MessagesRepository{}).InsertAssistantMessage(context.Background(), tx, accountID, threadID, runID, output)
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
		accountID uuid.UUID
		threadID  uuid.UUID
	)
	if err := tx.QueryRow(context.Background(), `
		SELECT sa.account_id, r.thread_id
		  FROM sub_agents sa
		  JOIN runs r ON r.id = sa.last_completed_run_id
		 WHERE sa.id = $1`, subAgentID).Scan(&accountID, &threadID); err != nil {
		t.Fatalf("load sub_agent account/thread: %v", err)
	}
	return accountID, threadID
}
func seedThreadAndRun(t *testing.T, pool *pgxpool.Pool, accountID, threadID uuid.UUID, projectID, userID *uuid.UUID, runID uuid.UUID) {
	t.Helper()
	_, err := pool.Exec(
		context.Background(),
		`INSERT INTO threads (id, account_id, created_by_user_id, project_id)
		 VALUES ($1, $2, $3, $4)`,
		threadID,
		accountID,
		userID,
		projectID,
	)
	if err != nil {
		t.Fatalf("insert thread failed: %v", err)
	}
	_, err = pool.Exec(
		context.Background(),
		`INSERT INTO runs (id, account_id, thread_id, created_by_user_id, status)
		 VALUES ($1, $2, $3, $4, 'running')`,
		runID,
		accountID,
		threadID,
		userID,
	)
	if err != nil {
		t.Fatalf("insert run failed: %v", err)
	}
}
