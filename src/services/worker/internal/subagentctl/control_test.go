//go:build !desktop

package subagentctl

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"arkloop/services/shared/runkind"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/queue"
	"arkloop/services/worker/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type stubJobQueue struct {
	mu             sync.Mutex
	enqueuedRunIDs []uuid.UUID
	enqueueErr     error
	enqueueErrs    []error
	enqueueStarted chan uuid.UUID
	blockFirst     <-chan struct{}
}

func (s *stubJobQueue) EnqueueRun(ctx context.Context, accountID uuid.UUID, runID uuid.UUID, traceID string, queueJobType string, payload map[string]any, availableAt *time.Time) (uuid.UUID, error) {
	s.mu.Lock()
	index := len(s.enqueuedRunIDs)
	s.enqueuedRunIDs = append(s.enqueuedRunIDs, runID)
	var err error
	if index < len(s.enqueueErrs) {
		err = s.enqueueErrs[index]
	} else {
		err = s.enqueueErr
	}
	started := s.enqueueStarted
	blockFirst := s.blockFirst
	s.mu.Unlock()
	if started != nil {
		select {
		case started <- runID:
		default:
		}
	}
	if index == 0 && blockFirst != nil {
		<-blockFirst
	}
	s.mu.Lock()
	if err != nil {
		return uuid.Nil, err
	}
	return uuid.New(), nil
}
func (s *stubJobQueue) Lease(context.Context, int, []string) (*queue.JobLease, error) {
	return nil, nil
}
func (s *stubJobQueue) Heartbeat(context.Context, queue.JobLease, int) error { return nil }
func (s *stubJobQueue) Ack(context.Context, queue.JobLease) error            { return nil }
func (s *stubJobQueue) Nack(context.Context, queue.JobLease, *int) error     { return nil }
func (s *stubJobQueue) QueueDepth(context.Context, []string) (int, error)    { return 0, nil }

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
	service := NewService(pool, nil, jobQueue, parentRun, "trace-1", SubAgentLimits{}, BackpressureConfig{}, nil)

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

	resolved, err := service.Wait(context.Background(), WaitRequest{SubAgentIDs: []uuid.UUID{snapshot.SubAgentID}, Timeout: 2 * time.Second})
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
	service := NewService(pool, nil, jobQueue, parentRun, "trace-2", SubAgentLimits{}, BackpressureConfig{}, nil)

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
	service := NewService(pool, nil, jobQueue, parentRun, "trace-2b", SubAgentLimits{}, BackpressureConfig{}, nil)

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

func TestServiceThreadScopedOwnershipAllowsFollowUpRunControl(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_subagentctl_thread_scoped")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runAID := uuid.New()
	runBID := uuid.New()
	userID := uuid.New()
	seedThreadAndRun(t, pool, accountID, threadID, &projectID, &userID, runAID)
	seedRunOnly(t, pool, accountID, threadID, &userID, runBID)

	jobQueue := &stubJobQueue{}
	serviceA := NewService(pool, nil, jobQueue, data.Run{ID: runAID, AccountID: accountID, ThreadID: threadID, ProjectID: &projectID, CreatedByUserID: &userID}, "trace-thread-a", SubAgentLimits{}, BackpressureConfig{}, nil)
	snapshot, err := serviceA.Spawn(context.Background(), isolatedSpawnRequest("phase one"))
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	completeSubAgentRun(t, pool, snapshot.SubAgentID, *snapshot.CurrentRunID, "phase one done")

	serviceB := NewService(pool, nil, jobQueue, data.Run{ID: runBID, AccountID: accountID, ThreadID: threadID, ProjectID: &projectID, CreatedByUserID: &userID}, "trace-thread-b", SubAgentLimits{}, BackpressureConfig{}, nil)
	status, err := serviceB.GetStatus(context.Background(), snapshot.SubAgentID)
	if err != nil {
		t.Fatalf("get status from follow-up run: %v", err)
	}
	if status.Status != data.SubAgentStatusCompleted {
		t.Fatalf("unexpected status: %s", status.Status)
	}

	resumed, err := serviceB.Resume(context.Background(), ResumeRequest{SubAgentID: snapshot.SubAgentID})
	if err != nil {
		t.Fatalf("resume from follow-up run: %v", err)
	}
	if resumed.CurrentRunID == nil {
		t.Fatal("expected resumed current_run_id")
	}
}

func TestProjectRunTerminalCreatesCallbackAndIdleWakeRun(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_subagentctl_callback_wake")
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
	service := NewService(pool, nil, &stubJobQueue{}, parentRun, "trace-callback", SubAgentLimits{}, BackpressureConfig{}, nil)
	snapshot, err := service.Spawn(context.Background(), isolatedSpawnRequest("collect facts"))
	if err != nil {
		t.Fatalf("spawn: %v", err)
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
	projection, err := service.projector.ProjectRunTerminal(context.Background(), tx, *childRun, data.SubAgentStatusCompleted, map[string]any{}, nil)
	if err != nil {
		t.Fatalf("project terminal: %v", err)
	}
	if projection.Callback == nil {
		t.Fatal("expected callback projection")
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit tx: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE runs SET status = 'completed' WHERE id = $1`, runID); err != nil {
		t.Fatalf("complete parent run: %v", err)
	}
	profileRef := "profile_parent"
	workspaceRef := "workspace_parent"
	if _, err := pool.Exec(context.Background(), `UPDATE runs SET profile_ref = $2, workspace_ref = $3 WHERE id = $1`, runID, profileRef, workspaceRef); err != nil {
		t.Fatalf("update parent bindings: %v", err)
	}
	rootStarted := map[string]any{
		"persona_id":             "root-persona@7",
		"role":                   "reviewer",
		"route_id":               "route-main",
		"output_route_id":        "route-out",
		"model":                  "openai^gpt-5",
		"work_dir":               "/tmp/project",
		"reasoning_mode":         "high",
		"thread_tail_message_id": uuid.NewString(),
		"channel_delivery": map[string]any{
			"channel_id": uuid.NewString(),
		},
	}
	rawStarted, err := json.Marshal(rootStarted)
	if err != nil {
		t.Fatalf("marshal root started: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO run_events (run_id, seq, type, data_json)
		 VALUES ($1, 1, 'run.started', $2::jsonb)`,
		runID,
		string(rawStarted),
	); err != nil {
		t.Fatalf("insert root run.started: %v", err)
	}

	jobQueue := &stubJobQueue{}
	projector := NewSubAgentStateProjector(pool, nil, jobQueue)
	if err := projector.EnqueueCallbackRunIfIdle(context.Background(), *projection.Callback, "trace-callback-run"); err != nil {
		t.Fatalf("enqueue callback run: %v", err)
	}
	if len(jobQueue.enqueuedRunIDs) != 1 {
		t.Fatalf("expected one callback run, got %d", len(jobQueue.enqueuedRunIDs))
	}

	callbacks, err := (data.ThreadSubAgentCallbacksRepository{}).ListPendingByThread(context.Background(), pool, threadID)
	if err != nil {
		t.Fatalf("list callbacks: %v", err)
	}
	if len(callbacks) != 1 || callbacks[0].ID != projection.Callback.ID {
		t.Fatalf("unexpected callbacks: %#v", callbacks)
	}

	callbackRunID := jobQueue.enqueuedRunIDs[0]
	var (
		gotCreatedBy *uuid.UUID
		gotProfile   *string
		gotWorkspace *string
	)
	if err := pool.QueryRow(context.Background(),
		`SELECT created_by_user_id, profile_ref, workspace_ref
		   FROM runs
		  WHERE id = $1`,
		callbackRunID,
	).Scan(&gotCreatedBy, &gotProfile, &gotWorkspace); err != nil {
		t.Fatalf("load callback run row: %v", err)
	}
	if gotCreatedBy == nil || *gotCreatedBy != userID {
		t.Fatalf("unexpected created_by_user_id: %#v", gotCreatedBy)
	}
	if gotProfile == nil || *gotProfile != profileRef {
		t.Fatalf("unexpected profile_ref: %#v", gotProfile)
	}
	if gotWorkspace == nil || *gotWorkspace != workspaceRef {
		t.Fatalf("unexpected workspace_ref: %#v", gotWorkspace)
	}

	var rawCallbackStarted []byte
	if err := pool.QueryRow(context.Background(),
		`SELECT data_json
		   FROM run_events
		  WHERE run_id = $1
		    AND type = 'run.started'
		  ORDER BY seq ASC
		  LIMIT 1`,
		callbackRunID,
	).Scan(&rawCallbackStarted); err != nil {
		t.Fatalf("load callback run.started: %v", err)
	}
	var callbackStarted map[string]any
	if err := json.Unmarshal(rawCallbackStarted, &callbackStarted); err != nil {
		t.Fatalf("unmarshal callback run.started: %v", err)
	}
	for _, key := range []string{"persona_id", "role", "route_id", "output_route_id", "model", "work_dir", "reasoning_mode", "channel_delivery", "thread_tail_message_id"} {
		if got := callbackStarted[key]; got == nil {
			t.Fatalf("expected inherited %s, got %#v", key, callbackStarted)
		}
	}
	if callbackStarted["persona_id"] != rootStarted["persona_id"] {
		t.Fatalf("unexpected persona_id: %#v", callbackStarted["persona_id"])
	}
	if callbackStarted["route_id"] != rootStarted["route_id"] {
		t.Fatalf("unexpected route_id: %#v", callbackStarted["route_id"])
	}
	if callbackStarted["work_dir"] != rootStarted["work_dir"] {
		t.Fatalf("unexpected work_dir: %#v", callbackStarted["work_dir"])
	}
	if _, ok := callbackStarted["callback_id"].(string); !ok {
		t.Fatalf("expected callback_id in run.started: %#v", callbackStarted)
	}
	if callbackStarted["run_kind"] != runkind.SubagentCallback {
		t.Fatalf("unexpected run_kind: %#v", callbackStarted["run_kind"])
	}
}

func TestEnqueueCallbackRunIfIdleSerializesThreadWakeup(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_subagentctl_callback_serialized_wake")
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
	if _, err := pool.Exec(context.Background(), `UPDATE runs SET status = 'completed' WHERE id = $1`, runID); err != nil {
		t.Fatalf("complete parent run: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO run_events (run_id, seq, type, data_json)
		 VALUES ($1, 1, 'run.started', '{"persona_id":"root-persona@1"}'::jsonb)`,
		runID,
	); err != nil {
		t.Fatalf("insert root run.started: %v", err)
	}

	callback := data.ThreadSubAgentCallbackRecord{
		ID:          uuid.New(),
		AccountID:   accountID,
		ThreadID:    threadID,
		SubAgentID:  uuid.New(),
		SourceRunID: uuid.New(),
		Status:      data.SubAgentStatusCompleted,
	}
	jobQueue := &stubJobQueue{}
	projector := NewSubAgentStateProjector(pool, nil, jobQueue)

	var wg sync.WaitGroup
	start := make(chan struct{})
	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errCh <- projector.EnqueueCallbackRunIfIdle(context.Background(), callback, "trace-race")
		}()
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("enqueue callback run: %v", err)
		}
	}
	if len(jobQueue.enqueuedRunIDs) != 1 {
		t.Fatalf("expected one callback run, got %d", len(jobQueue.enqueuedRunIDs))
	}
}

func TestEnqueueCallbackRunIfIdleInheritsLatestThreadRunSeed(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_subagentctl_callback_latest_thread_seed")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	rootRunID := uuid.New()
	childRunID := uuid.New()
	userID := uuid.New()
	seedThreadAndRun(t, pool, accountID, threadID, &projectID, &userID, rootRunID)
	if _, err := pool.Exec(context.Background(), `UPDATE runs SET status = 'completed' WHERE id = $1`, rootRunID); err != nil {
		t.Fatalf("complete root run: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO runs (id, account_id, thread_id, parent_run_id, created_by_user_id, profile_ref, workspace_ref, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, 'completed')`,
		childRunID,
		accountID,
		threadID,
		rootRunID,
		userID,
		"profile_child",
		"workspace_child",
	); err != nil {
		t.Fatalf("insert child run: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO run_events (run_id, seq, type, data_json)
		 VALUES ($1, 1, 'run.started', $2::jsonb)`,
		childRunID,
		`{"persona_id":"child-persona@1","route_id":"child-route","thread_tail_message_id":"tail-1"}`,
	); err != nil {
		t.Fatalf("insert child run.started: %v", err)
	}

	callback := data.ThreadSubAgentCallbackRecord{
		ID:          uuid.New(),
		AccountID:   accountID,
		ThreadID:    threadID,
		SubAgentID:  uuid.New(),
		SourceRunID: uuid.New(),
		Status:      data.SubAgentStatusCompleted,
	}
	jobQueue := &stubJobQueue{}
	projector := NewSubAgentStateProjector(pool, nil, jobQueue)
	if err := projector.EnqueueCallbackRunIfIdle(context.Background(), callback, "trace-seed"); err != nil {
		t.Fatalf("enqueue callback run: %v", err)
	}
	if len(jobQueue.enqueuedRunIDs) != 1 {
		t.Fatalf("expected one callback run, got %d", len(jobQueue.enqueuedRunIDs))
	}

	callbackRunID := jobQueue.enqueuedRunIDs[0]
	var (
		gotProfile   *string
		gotWorkspace *string
		rawStarted   []byte
	)
	if err := pool.QueryRow(context.Background(),
		`SELECT profile_ref, workspace_ref
		   FROM runs
		  WHERE id = $1`,
		callbackRunID,
	).Scan(&gotProfile, &gotWorkspace); err != nil {
		t.Fatalf("load callback run row: %v", err)
	}
	if gotProfile == nil || *gotProfile != "profile_child" {
		t.Fatalf("unexpected profile_ref: %#v", gotProfile)
	}
	if gotWorkspace == nil || *gotWorkspace != "workspace_child" {
		t.Fatalf("unexpected workspace_ref: %#v", gotWorkspace)
	}
	if err := pool.QueryRow(context.Background(),
		`SELECT data_json
		   FROM run_events
		  WHERE run_id = $1
		    AND type = 'run.started'
		  ORDER BY seq ASC
		  LIMIT 1`,
		callbackRunID,
	).Scan(&rawStarted); err != nil {
		t.Fatalf("load callback run.started: %v", err)
	}
	var started map[string]any
	if err := json.Unmarshal(rawStarted, &started); err != nil {
		t.Fatalf("unmarshal callback run.started: %v", err)
	}
	if started["persona_id"] != "child-persona@1" {
		t.Fatalf("unexpected persona_id: %#v", started["persona_id"])
	}
	if started["route_id"] != "child-route" {
		t.Fatalf("unexpected route_id: %#v", started["route_id"])
	}
}

func TestEnqueueCallbackRunIfIdleMarksFailedRunAndUnblocksThreadOnQueueFailure(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_subagentctl_callback_enqueue_failure")
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
	if _, err := pool.Exec(context.Background(), `UPDATE runs SET status = 'completed' WHERE id = $1`, runID); err != nil {
		t.Fatalf("complete parent run: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO run_events (run_id, seq, type, data_json)
		 VALUES ($1, 1, 'run.started', '{"persona_id":"root-persona@1"}'::jsonb)`,
		runID,
	); err != nil {
		t.Fatalf("insert root run.started: %v", err)
	}

	failingQueue := &stubJobQueue{enqueueErr: context.DeadlineExceeded}
	projector := NewSubAgentStateProjector(pool, nil, failingQueue)
	firstCallback := data.ThreadSubAgentCallbackRecord{
		ID:          uuid.New(),
		AccountID:   accountID,
		ThreadID:    threadID,
		SubAgentID:  uuid.New(),
		SourceRunID: uuid.New(),
		Status:      data.SubAgentStatusCompleted,
	}
	if err := projector.EnqueueCallbackRunIfIdle(context.Background(), firstCallback, "trace-enqueue-fail"); err == nil {
		t.Fatal("expected enqueue callback run to fail")
	}

	failingQueue.mu.Lock()
	if len(failingQueue.enqueuedRunIDs) != 1 {
		failingQueue.mu.Unlock()
		t.Fatalf("expected one attempted callback run, got %d", len(failingQueue.enqueuedRunIDs))
	}
	failedRunID := failingQueue.enqueuedRunIDs[0]
	failingQueue.mu.Unlock()

	var status string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM runs WHERE id = $1`, failedRunID).Scan(&status); err != nil {
		t.Fatalf("load failed callback run: %v", err)
	}
	if status != "failed" {
		t.Fatalf("expected failed callback run status, got %q", status)
	}

	recoveredQueue := &stubJobQueue{}
	projector = NewSubAgentStateProjector(pool, nil, recoveredQueue)
	secondCallback := data.ThreadSubAgentCallbackRecord{
		ID:          uuid.New(),
		AccountID:   accountID,
		ThreadID:    threadID,
		SubAgentID:  uuid.New(),
		SourceRunID: uuid.New(),
		Status:      data.SubAgentStatusCompleted,
	}
	if err := projector.EnqueueCallbackRunIfIdle(context.Background(), secondCallback, "trace-enqueue-recover"); err != nil {
		t.Fatalf("enqueue callback run after failure: %v", err)
	}
	if len(recoveredQueue.enqueuedRunIDs) != 1 {
		t.Fatalf("expected one recovered callback run, got %d", len(recoveredQueue.enqueuedRunIDs))
	}
}

func TestEnqueueCallbackRunIfIdleRecoversSkippedPendingCallbackAfterConcurrentEnqueueFailure(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "arkloop_subagentctl_callback_concurrent_enqueue_failure")
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
	if _, err := pool.Exec(context.Background(), `UPDATE runs SET status = 'completed' WHERE id = $1`, runID); err != nil {
		t.Fatalf("complete parent run: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO run_events (run_id, seq, type, data_json)
		 VALUES ($1, 1, 'run.started', '{"persona_id":"root-persona@1"}'::jsonb)`,
		runID,
	); err != nil {
		t.Fatalf("insert root run.started: %v", err)
	}

	unblockFirst := make(chan struct{})
	queue := &stubJobQueue{
		enqueueErrs:    []error{context.DeadlineExceeded, nil},
		enqueueStarted: make(chan uuid.UUID, 2),
		blockFirst:     unblockFirst,
	}
	projector := NewSubAgentStateProjector(pool, nil, queue)
	firstCallback := data.ThreadSubAgentCallbackRecord{
		ID:          uuid.New(),
		AccountID:   accountID,
		ThreadID:    threadID,
		SubAgentID:  uuid.New(),
		SourceRunID: uuid.New(),
		Status:      data.SubAgentStatusCompleted,
	}
	secondCallback := data.ThreadSubAgentCallbackRecord{
		ID:          uuid.New(),
		AccountID:   accountID,
		ThreadID:    threadID,
		SubAgentID:  uuid.New(),
		SourceRunID: uuid.New(),
		Status:      data.SubAgentStatusCompleted,
	}

	firstErrCh := make(chan error, 1)
	go func() {
		firstErrCh <- projector.EnqueueCallbackRunIfIdle(context.Background(), firstCallback, "trace-concurrent-first")
	}()

	var firstRunID uuid.UUID
	select {
	case firstRunID = <-queue.enqueueStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for first enqueue attempt")
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- projector.EnqueueCallbackRunIfIdle(context.Background(), secondCallback, "trace-concurrent-second")
	}()

	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("second enqueue should have short-circuited cleanly: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for second enqueue attempt to observe active run")
	}

	close(unblockFirst)
	if err := <-firstErrCh; err == nil {
		t.Fatal("expected first enqueue to fail")
	}

	queue.mu.Lock()
	if len(queue.enqueuedRunIDs) != 2 {
		queue.mu.Unlock()
		t.Fatalf("expected two enqueue attempts, got %d", len(queue.enqueuedRunIDs))
	}
	recoveredRunID := queue.enqueuedRunIDs[1]
	queue.mu.Unlock()

	var failedStatus string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM runs WHERE id = $1`, firstRunID).Scan(&failedStatus); err != nil {
		t.Fatalf("load failed callback run: %v", err)
	}
	if failedStatus != "failed" {
		t.Fatalf("expected first callback run to fail, got %q", failedStatus)
	}

	var recoveredStatus string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM runs WHERE id = $1`, recoveredRunID).Scan(&recoveredStatus); err != nil {
		t.Fatalf("load recovered callback run: %v", err)
	}
	if recoveredStatus != "running" {
		t.Fatalf("expected recovered callback run to be running, got %q", recoveredStatus)
	}

	var rawStarted []byte
	if err := pool.QueryRow(context.Background(),
		`SELECT data_json
		   FROM run_events
		  WHERE run_id = $1
		    AND type = 'run.started'
		  ORDER BY seq ASC
		  LIMIT 1`,
		recoveredRunID,
	).Scan(&rawStarted); err != nil {
		t.Fatalf("load recovered run.started: %v", err)
	}
	var started map[string]any
	if err := json.Unmarshal(rawStarted, &started); err != nil {
		t.Fatalf("unmarshal recovered run.started: %v", err)
	}
	if started["callback_id"] != secondCallback.ID.String() {
		t.Fatalf("expected recovered run to target second callback, got %#v", started["callback_id"])
	}
}

func seedRunOnly(t *testing.T, pool *pgxpool.Pool, accountID, threadID uuid.UUID, userID *uuid.UUID, runID uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO runs (id, account_id, thread_id, created_by_user_id, status)
		 VALUES ($1, $2, $3, $4, 'running')`,
		runID,
		accountID,
		threadID,
		userID,
	); err != nil {
		t.Fatalf("insert run: %v", err)
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
	service := NewService(pool, nil, jobQueue, parentRun, "trace-3", SubAgentLimits{}, BackpressureConfig{}, nil)

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
	projection, err := service.projector.ProjectRunTerminal(context.Background(), tx, *childRun, data.SubAgentStatusCancelled, map[string]any{"message": "cancelled"}, nil)
	if err != nil {
		t.Fatalf("project terminal: %v", err)
	}
	if projection.NextRunID == nil {
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
	if resolved.CurrentRunID == nil || *resolved.CurrentRunID != *projection.NextRunID {
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
		 LIMIT 1`, *projection.NextRunID).Scan(&merged); err != nil {
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
	service := NewService(pool, nil, &stubJobQueue{}, parentRun, "trace-fork", SubAgentLimits{}, BackpressureConfig{}, nil)

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
	service := NewService(pool, nil, &stubJobQueue{}, parentRun, "trace-shared", SubAgentLimits{}, BackpressureConfig{}, nil)

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
	service := NewService(pool, nil, &stubJobQueue{}, parentRun, "trace-role", SubAgentLimits{}, BackpressureConfig{}, nil)
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
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := (data.SubAgentRepository{}).TransitionToTerminal(context.Background(), tx, runID, data.SubAgentStatusCompleted, nil); err != nil {
		t.Fatalf("transition terminal: %v", err)
	}
	accountID, threadID := mustRunContext(t, tx, subAgentID)
	messageID, err := (data.MessagesRepository{}).InsertAssistantMessage(context.Background(), tx, accountID, threadID, runID, output, nil, false)
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
