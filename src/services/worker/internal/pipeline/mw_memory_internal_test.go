//go:build !desktop

package pipeline

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/testutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type refreshProviderStub struct {
	mu sync.Mutex

	findSeq [][]memory.MemoryHit
	errSeq  []error

	appendErr error
	commitErr error

	appendDelay time.Duration
	commitDelay time.Duration
	commitDone  chan struct{}
}

func newRefreshProviderStub() *refreshProviderStub {
	return &refreshProviderStub{
		commitDone: make(chan struct{}, 4),
	}
}

func (p *refreshProviderStub) Find(_ context.Context, _ memory.MemoryIdentity, _ string, _ string, _ int) ([]memory.MemoryHit, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.errSeq) > 0 {
		err := p.errSeq[0]
		if len(p.errSeq) > 1 {
			p.errSeq = p.errSeq[1:]
		}
		if err != nil {
			return nil, err
		}
	}

	if len(p.findSeq) == 0 {
		return nil, nil
	}
	hits := p.findSeq[0]
	if len(p.findSeq) > 1 {
		p.findSeq = p.findSeq[1:]
	}
	return hits, nil
}

func (p *refreshProviderStub) Content(_ context.Context, _ memory.MemoryIdentity, _ string, _ memory.MemoryLayer) (string, error) {
	return "", nil
}

func (p *refreshProviderStub) AppendSessionMessages(_ context.Context, _ memory.MemoryIdentity, _ string, _ []memory.MemoryMessage) error {
	if p.appendDelay > 0 {
		time.Sleep(p.appendDelay)
	}
	return p.appendErr
}

func (p *refreshProviderStub) CommitSession(_ context.Context, _ memory.MemoryIdentity, _ string) error {
	if p.commitDelay > 0 {
		time.Sleep(p.commitDelay)
	}
	if p.commitDone != nil {
		p.commitDone <- struct{}{}
	}
	return p.commitErr
}

func (p *refreshProviderStub) Write(_ context.Context, _ memory.MemoryIdentity, _ memory.MemoryScope, _ memory.MemoryEntry) error {
	return nil
}

func (p *refreshProviderStub) Delete(_ context.Context, _ memory.MemoryIdentity, _ string) error {
	return nil
}

func withShortSnapshotRefresh(t *testing.T) {
	t.Helper()
	prevWindow := snapshotRefreshWindow
	prevInterval := snapshotRefreshRetryInterval
	prevAttempts := snapshotRefreshMaxAttempts
	snapshotRefreshWindow = 80 * time.Millisecond
	snapshotRefreshRetryInterval = 10 * time.Millisecond
	snapshotRefreshMaxAttempts = 3
	t.Cleanup(func() {
		snapshotRefreshWindow = prevWindow
		snapshotRefreshRetryInterval = prevInterval
		snapshotRefreshMaxAttempts = prevAttempts
	})
}

func setupMemoryRun(t *testing.T, dbName string) (*pgxpool.Pool, data.Run, memory.MemoryIdentity) {
	t.Helper()
	db := testutil.SetupPostgresDatabase(t, dbName)
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

	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)

	return pool, data.Run{ID: runID, AccountID: accountID, ThreadID: threadID}, memory.MemoryIdentity{
		AccountID: accountID,
		UserID:    userID,
		AgentID:   "user_" + userID.String(),
	}
}

func waitForSnapshotBlock(t *testing.T, pool *pgxpool.Pool, ident memory.MemoryIdentity, want string) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		block, found, err := snapshotRepo.Get(context.Background(), pool, ident.AccountID, ident.UserID, ident.AgentID)
		if err == nil && found && block == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	block, _, _ := snapshotRepo.Get(context.Background(), pool, ident.AccountID, ident.UserID, ident.AgentID)
	t.Fatalf("snapshot block not updated, got %q want %q", block, want)
}

func waitForEventTypes(t *testing.T, pool *pgxpool.Pool, runID uuid.UUID, want ...string) {
	t.Helper()
	wantSet := map[string]struct{}{}
	for _, item := range want {
		wantSet[item] = struct{}{}
	}
	deadline := time.Now().Add(800 * time.Millisecond)
	for time.Now().Before(deadline) {
		rows, err := pool.Query(context.Background(), `SELECT type FROM run_events WHERE run_id = $1`, runID)
		if err != nil {
			t.Fatalf("query run_events: %v", err)
		}
		gotSet := map[string]struct{}{}
		for rows.Next() {
			var eventType string
			if err := rows.Scan(&eventType); err != nil {
				rows.Close()
				t.Fatalf("scan run event: %v", err)
			}
			gotSet[eventType] = struct{}{}
		}
		rows.Close()

		ok := true
		for item := range wantSet {
			if _, found := gotSet[item]; !found {
				ok = false
				break
			}
		}
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for events %#v", want)
}

func TestScheduleSnapshotRefreshPreservesOldSnapshotOnMiss(t *testing.T) {
	withShortSnapshotRefresh(t)
	pool, run, ident := setupMemoryRun(t, "memory_snapshot_preserve_old")
	oldBlock := "\n\n<memory>\n- old memory\n</memory>"
	if err := snapshotRepo.Upsert(context.Background(), pool, ident.AccountID, ident.UserID, ident.AgentID, oldBlock); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}

	provider := newRefreshProviderStub()
	provider.findSeq = [][]memory.MemoryHit{{}, {}, {}}

	scheduleSnapshotRefresh(provider, pool, run.ID, "trace-preserve", ident, "", map[string][]string{
		string(memory.MemoryScopeUser): {"fresh query"},
	}, "", "write")

	time.Sleep(200 * time.Millisecond)
	block, found, err := snapshotRepo.Get(context.Background(), pool, ident.AccountID, ident.UserID, ident.AgentID)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if !found || block != oldBlock {
		t.Fatalf("expected old snapshot preserved, got found=%v block=%q", found, block)
	}
}

func TestScheduleSnapshotRefreshUpdatesSnapshotWhenHitAppears(t *testing.T) {
	withShortSnapshotRefresh(t)
	pool, run, ident := setupMemoryRun(t, "memory_snapshot_updates_later")
	if err := snapshotRepo.Upsert(context.Background(), pool, ident.AccountID, ident.UserID, ident.AgentID, "\n\n<memory>\n- old\n</memory>"); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}

	provider := newRefreshProviderStub()
	provider.findSeq = [][]memory.MemoryHit{
		{},
		{{URI: "viking://user/memories/new", Abstract: "new memory", Score: 0.9, IsLeaf: true}},
	}

	scheduleSnapshotRefresh(provider, pool, run.ID, "trace-update", ident, "", map[string][]string{
		string(memory.MemoryScopeUser): {"new memory"},
	}, "", "write")

	waitForSnapshotBlock(t, pool, ident, "\n\n<memory>\n- new memory\n</memory>")
}

func TestDistillAfterRunEmitsEventsAndPendingSnapshot(t *testing.T) {
	withShortSnapshotRefresh(t)
	pool, run, ident := setupMemoryRun(t, "memory_distill_events")

	provider := newRefreshProviderStub()
	provider.findSeq = [][]memory.MemoryHit{{}, {}, {}}
	rc := &RunContext{
		Run:                  run,
		TraceID:              "trace-distill",
		FinalAssistantOutput: "assistant reply",
		RunToolCallCount:     3,
	}

	distillAfterRun(provider, pool, nil, rc, ident, []memory.MemoryMessage{
		{Role: "user", Content: "first prompt"},
	})

	waitForEventTypes(t, pool, run.ID,
		eventTypeMemoryDistillStarted,
		eventTypeMemoryDistillCommitted,
		eventTypeMemoryDistillSnapshotPending,
	)
}

func TestDistillAfterRunEmitsSkippedWhenNoIncrementalMessages(t *testing.T) {
	pool, run, ident := setupMemoryRun(t, "memory_distill_skipped")
	provider := newRefreshProviderStub()
	rc := &RunContext{
		Run:                  run,
		TraceID:              "trace-skipped",
		FinalAssistantOutput: "assistant reply",
		RunToolCallCount:     3,
	}

	distillAfterRun(provider, pool, nil, rc, ident, nil)

	waitForEventTypes(t, pool, run.ID, eventTypeMemoryDistillSkipped)
	var reason string
	if err := pool.QueryRow(context.Background(), `
		SELECT data_json->>'reason'
		  FROM run_events
		 WHERE run_id = $1 AND type = $2
		 ORDER BY seq DESC
		 LIMIT 1
	`, run.ID, eventTypeMemoryDistillSkipped).Scan(&reason); err != nil {
		t.Fatalf("load skipped reason: %v", err)
	}
	if reason != distillSkipReasonNoIncrementalInput {
		t.Fatalf("unexpected skip reason: %q", reason)
	}
}

func TestHeartbeatFragmentsCommitRunsAsyncAndEmitsEvents(t *testing.T) {
	withShortSnapshotRefresh(t)
	pool, run, ident := setupMemoryRun(t, "memory_heartbeat_async")

	provider := newRefreshProviderStub()
	provider.appendDelay = 150 * time.Millisecond
	provider.findSeq = [][]memory.MemoryHit{{}, {}, {}}
	userID := ident.UserID
	rc := &RunContext{
		Run:                  run,
		Pool:                 pool,
		TraceID:              "trace-heartbeat",
		UserID:               &userID,
		MemoryProvider:       provider,
		JobPayload:           map[string]any{"run_kind": "heartbeat"},
		HeartbeatToolOutcome: &HeartbeatDecisionOutcome{Fragments: []string{"remember this"}},
	}

	mw := NewHeartbeatPrepareMiddleware()
	started := time.Now()
	if err := mw(context.Background(), rc, func(_ context.Context, rc *RunContext) error {
		rc.HeartbeatToolOutcome = &HeartbeatDecisionOutcome{Fragments: []string{"remember this"}}
		return nil
	}); err != nil {
		t.Fatalf("heartbeat middleware: %v", err)
	}
	if elapsed := time.Since(started); elapsed >= provider.appendDelay {
		t.Fatalf("expected async heartbeat memory commit, elapsed=%s delay=%s", elapsed, provider.appendDelay)
	}

	select {
	case <-provider.commitDone:
	case <-time.After(400 * time.Millisecond):
		t.Fatal("timeout waiting for heartbeat commit")
	}

	waitForEventTypes(t, pool, run.ID,
		eventTypeMemoryHeartbeatStarted,
		eventTypeMemoryHeartbeatCommitted,
		"memory.heartbeat.snapshot_pending",
	)
}

func TestScheduleSnapshotRefreshKeepsRetryingAfterTransientErrors(t *testing.T) {
	withShortSnapshotRefresh(t)
	pool, run, ident := setupMemoryRun(t, "memory_snapshot_retry_errors")
	if err := snapshotRepo.Upsert(context.Background(), pool, ident.AccountID, ident.UserID, ident.AgentID, "\n\n<memory>\n- old\n</memory>"); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}

	provider := newRefreshProviderStub()
	provider.errSeq = []error{errors.New("temporary"), nil}
	provider.findSeq = [][]memory.MemoryHit{
		nil,
		{{URI: "viking://user/memories/recovered", Abstract: "recovered memory", Score: 0.8, IsLeaf: true}},
	}

	scheduleSnapshotRefresh(provider, pool, run.ID, "trace-retry", ident, "", map[string][]string{
		string(memory.MemoryScopeUser): {"recovered memory"},
	}, "", "write")

	waitForSnapshotBlock(t, pool, ident, "\n\n<memory>\n- recovered memory\n</memory>")
}
