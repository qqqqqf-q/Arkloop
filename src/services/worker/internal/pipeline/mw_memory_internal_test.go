//go:build !desktop

package pipeline

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/testutil"
	"arkloop/services/worker/internal/tools"
	heartbeattool "arkloop/services/worker/internal/tools/builtin/heartbeat_decision"

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
	appendCount int
	commitCount int
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
	p.mu.Lock()
	p.appendCount++
	p.mu.Unlock()
	if p.appendDelay > 0 {
		time.Sleep(p.appendDelay)
	}
	return p.appendErr
}

func (p *refreshProviderStub) CommitSession(_ context.Context, _ memory.MemoryIdentity, _ string) error {
	p.mu.Lock()
	p.commitCount++
	p.mu.Unlock()
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

func (p *refreshProviderStub) ListDir(_ context.Context, _ memory.MemoryIdentity, _ string) ([]string, error) {
	return nil, nil
}

type heartbeatDirectWriterStub struct {
	mu sync.Mutex

	appendCount int
	commitCount int
	writeCount  int
	writeDelay  time.Duration
	writeErr    error
	writeDone   chan struct{}
	fragments   []memory.MemoryFragment
}

func newHeartbeatDirectWriterStub() *heartbeatDirectWriterStub {
	return &heartbeatDirectWriterStub{
		writeDone: make(chan struct{}, 8),
	}
}

func (p *heartbeatDirectWriterStub) Find(context.Context, memory.MemoryIdentity, string, string, int) ([]memory.MemoryHit, error) {
	return nil, nil
}

func (p *heartbeatDirectWriterStub) Content(context.Context, memory.MemoryIdentity, string, memory.MemoryLayer) (string, error) {
	return "", nil
}

func (p *heartbeatDirectWriterStub) AppendSessionMessages(context.Context, memory.MemoryIdentity, string, []memory.MemoryMessage) error {
	p.mu.Lock()
	p.appendCount++
	p.mu.Unlock()
	return nil
}

func (p *heartbeatDirectWriterStub) CommitSession(context.Context, memory.MemoryIdentity, string) error {
	p.mu.Lock()
	p.commitCount++
	p.mu.Unlock()
	return nil
}

func (p *heartbeatDirectWriterStub) Write(context.Context, memory.MemoryIdentity, memory.MemoryScope, memory.MemoryEntry) error {
	return nil
}

func (p *heartbeatDirectWriterStub) WriteReturningURI(_ context.Context, _ memory.MemoryIdentity, _ memory.MemoryScope, entry memory.MemoryEntry) (string, error) {
	if p.writeDelay > 0 {
		time.Sleep(p.writeDelay)
	}
	if p.writeErr != nil {
		return "", p.writeErr
	}
	content := strings.TrimSpace(entry.Content)
	p.mu.Lock()
	p.writeCount++
	index := p.writeCount
	p.fragments = append(p.fragments, memory.MemoryFragment{
		ID:       "mem-" + strconv.Itoa(index),
		URI:      "nowledge://memory/mem-" + strconv.Itoa(index),
		Title:    content,
		Content:  content,
		Abstract: content,
		Score:    1,
	})
	p.mu.Unlock()
	if p.writeDone != nil {
		p.writeDone <- struct{}{}
	}
	return "nowledge://memory/mem-" + strconv.Itoa(index), nil
}

func (p *heartbeatDirectWriterStub) Delete(context.Context, memory.MemoryIdentity, string) error {
	return nil
}

func (p *heartbeatDirectWriterStub) ListDir(context.Context, memory.MemoryIdentity, string) ([]string, error) {
	return nil, nil
}

func (p *heartbeatDirectWriterStub) ListFragments(_ context.Context, _ memory.MemoryIdentity, _ int) ([]memory.MemoryFragment, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]memory.MemoryFragment(nil), p.fragments...), nil
}

type fragmentRefreshProviderStub struct {
	fragments           []memory.MemoryFragment
	listFragmentsCalled bool
	treeTouched         bool
}

func (p *fragmentRefreshProviderStub) Find(context.Context, memory.MemoryIdentity, string, string, int) ([]memory.MemoryHit, error) {
	return nil, nil
}

func (p *fragmentRefreshProviderStub) Content(context.Context, memory.MemoryIdentity, string, memory.MemoryLayer) (string, error) {
	p.treeTouched = true
	return "", nil
}

func (p *fragmentRefreshProviderStub) AppendSessionMessages(context.Context, memory.MemoryIdentity, string, []memory.MemoryMessage) error {
	return nil
}

func (p *fragmentRefreshProviderStub) CommitSession(context.Context, memory.MemoryIdentity, string) error {
	return nil
}

func (p *fragmentRefreshProviderStub) Write(context.Context, memory.MemoryIdentity, memory.MemoryScope, memory.MemoryEntry) error {
	return nil
}

func (p *fragmentRefreshProviderStub) Delete(context.Context, memory.MemoryIdentity, string) error {
	return nil
}

func (p *fragmentRefreshProviderStub) ListDir(context.Context, memory.MemoryIdentity, string) ([]string, error) {
	p.treeTouched = true
	return nil, nil
}

func (p *fragmentRefreshProviderStub) ListFragments(_ context.Context, _ memory.MemoryIdentity, _ int) ([]memory.MemoryFragment, error) {
	p.listFragmentsCalled = true
	return append([]memory.MemoryFragment(nil), p.fragments...), nil
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
		block, found, err := data.MemorySnapshotRepository{}.Get(context.Background(), pool, ident.AccountID, ident.UserID, ident.AgentID)
		if err == nil && found && block == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	block, _, _ := data.MemorySnapshotRepository{}.Get(context.Background(), pool, ident.AccountID, ident.UserID, ident.AgentID)
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

type failingNotebookSnapshotReader struct {
	err error
}

func (r failingNotebookSnapshotReader) GetSnapshot(context.Context, uuid.UUID, uuid.UUID, string) (string, error) {
	return "", r.err
}

func TestScheduleSnapshotRefreshPreservesOldSnapshotOnMiss(t *testing.T) {
	withShortSnapshotRefresh(t)
	pool, run, ident := setupMemoryRun(t, "memory_snapshot_preserve_old")
	oldBlock := "\n\n<memory>\n- old memory\n</memory>"
	repo := data.MemorySnapshotRepository{}
	if err := repo.Upsert(context.Background(), pool, ident.AccountID, ident.UserID, ident.AgentID, oldBlock); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}

	provider := newRefreshProviderStub()
	provider.findSeq = [][]memory.MemoryHit{{}, {}, {}}

	snap := NewPgxMemorySnapshotStore(pool)
	scheduleSnapshotRefresh(provider, snap, pool, run.ID, "trace-preserve", ident, "", map[string][]string{
		string(memory.MemoryScopeUser): {"fresh query"},
	}, "", "write")

	time.Sleep(200 * time.Millisecond)
	block, found, err := data.MemorySnapshotRepository{}.Get(context.Background(), pool, ident.AccountID, ident.UserID, ident.AgentID)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if !found || block != oldBlock {
		t.Fatalf("expected old snapshot preserved, got found=%v block=%q", found, block)
	}
}

func TestTryRefreshSnapshotFromQueriesUsesFragments(t *testing.T) {
	pool, _, ident := setupMemoryRun(t, "memory_snapshot_fragments")
	provider := &fragmentRefreshProviderStub{
		fragments: []memory.MemoryFragment{{
			ID:       "mem-1",
			URI:      "nowledge://memory/mem-1",
			Title:    "Preference",
			Content:  "Owner prefers concise Chinese replies for engineering tasks.",
			Abstract: "Preference",
			Score:    0.91,
		}},
	}

	updated, err := tryRefreshSnapshotFromQueries(context.Background(), NewPgxMemorySnapshotStore(pool), provider, ident, map[string][]string{
		string(memory.MemoryScopeUser): {"concise replies"},
	})
	if err != nil {
		t.Fatalf("tryRefreshSnapshotFromQueries: %v", err)
	}
	if !updated {
		t.Fatal("expected snapshot refresh to update")
	}
	if !provider.listFragmentsCalled {
		t.Fatal("expected fragment listing path to be used")
	}
	if provider.treeTouched {
		t.Fatal("did not expect tree path for fragment source")
	}

	block, found, err := data.MemorySnapshotRepository{}.Get(context.Background(), pool, ident.AccountID, ident.UserID, ident.AgentID)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if !found || !strings.Contains(block, "[Preference]") {
		t.Fatalf("unexpected snapshot block: found=%v block=%q", found, block)
	}

	hits, hitsFound, err := data.MemorySnapshotRepository{}.GetHits(context.Background(), pool, ident.AccountID, ident.UserID, ident.AgentID)
	if err != nil {
		t.Fatalf("load hits: %v", err)
	}
	if !hitsFound || len(hits) != 1 {
		t.Fatalf("expected one cached hit, got found=%v hits=%#v", hitsFound, hits)
	}
	if hits[0].URI != "nowledge://memory/mem-1" || !hits[0].IsLeaf {
		t.Fatalf("unexpected hit: %#v", hits[0])
	}
}

func TestTryRefreshSnapshotFromQueriesWritesEmptyBlockWhenFragmentsEmpty(t *testing.T) {
	pool, _, ident := setupMemoryRun(t, "memory_snapshot_fragments_empty")
	provider := &fragmentRefreshProviderStub{}

	updated, err := tryRefreshSnapshotFromQueries(context.Background(), NewPgxMemorySnapshotStore(pool), provider, ident, map[string][]string{
		string(memory.MemoryScopeUser): {"empty case"},
	})
	if err != nil {
		t.Fatalf("tryRefreshSnapshotFromQueries: %v", err)
	}
	if !updated {
		t.Fatal("expected empty fragment result to still update snapshot")
	}

	block, found, err := data.MemorySnapshotRepository{}.Get(context.Background(), pool, ident.AccountID, ident.UserID, ident.AgentID)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if !found || block != "" {
		t.Fatalf("expected empty snapshot block, got found=%v block=%q", found, block)
	}

	hits, hitsFound, err := data.MemorySnapshotRepository{}.GetHits(context.Background(), pool, ident.AccountID, ident.UserID, ident.AgentID)
	if err != nil {
		t.Fatalf("load hits: %v", err)
	}
	if hitsFound || len(hits) != 0 {
		t.Fatalf("expected no cached hits, got found=%v hits=%#v", hitsFound, hits)
	}
}

func TestPromptHookMiddleware_NotebookReadFailureEmitsRunEventAndHookTrace(t *testing.T) {
	pool, run, ident := setupMemoryRun(t, "prompt_hook_notebook_failure")
	tracer := &spyTracer{}
	registry := NewHookRegistry()
	registry.RegisterContextContributor(NewNotebookContextContributor(failingNotebookSnapshotReader{
		err: errors.New("snapshot boom"),
	}))

	rc := &RunContext{
		Run:             run,
		UserID:          &ident.UserID,
		MemoryServiceDB: pool,
		Emitter:         events.NewEmitter("trace-notebook"),
		TraceID:         "trace-notebook",
		Tracer:          tracer,
		HookRuntime:     NewHookRuntime(registry, NewDefaultHookResultApplier()),
	}

	mw := NewPromptHookMiddleware()
	if err := mw(context.Background(), rc, func(context.Context, *RunContext) error { return nil }); err != nil {
		t.Fatalf("prompt hook failed: %v", err)
	}

	waitForEventTypes(t, pool, run.ID, "notebook.snapshot.read_failed")
	failed := findTraceEvent(tracer.records, "runtime_hook.failed")
	if failed == nil {
		t.Fatal("expected runtime_hook.failed event")
	}
	if failed.fields["hook_name"] != string(HookBeforePromptAssemble) {
		t.Fatalf("unexpected hook_name: %#v", failed.fields["hook_name"])
	}
	if failed.fields["provider"] != "notebook" {
		t.Fatalf("unexpected provider: %#v", failed.fields["provider"])
	}
	if strings.Contains(rc.SystemPrompt, "notebook_unavailable") {
		t.Fatalf("unexpected fallback prompt block: %q", rc.SystemPrompt)
	}
}

func TestScheduleSnapshotRefreshUpdatesSnapshotWhenHitAppears(t *testing.T) {
	withShortSnapshotRefresh(t)
	pool, run, ident := setupMemoryRun(t, "memory_snapshot_updates_later")
	repoSeed := data.MemorySnapshotRepository{}
	if err := repoSeed.Upsert(context.Background(), pool, ident.AccountID, ident.UserID, ident.AgentID, "\n\n<memory>\n- old\n</memory>"); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}

	provider := newRefreshProviderStub()
	provider.findSeq = [][]memory.MemoryHit{
		{},
		{{URI: "viking://user/memories/new", Abstract: "new memory", Score: 0.9, IsLeaf: true}},
	}

	snap := NewPgxMemorySnapshotStore(pool)
	scheduleSnapshotRefresh(provider, snap, pool, run.ID, "trace-update", ident, "", map[string][]string{
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

	distillAfterRun(provider, NewPgxMemorySnapshotStore(pool), pool, nil, rc, ident, []memory.MemoryMessage{
		{Role: "user", Content: "first prompt"},
	}, nil, nil)

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

	distillAfterRun(provider, NewPgxMemorySnapshotStore(pool), pool, nil, rc, ident, nil, nil, nil)

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

func TestDistillAfterRunSkipsHeartbeatRuns(t *testing.T) {
	pool, run, ident := setupMemoryRun(t, "memory_distill_heartbeat_skip")
	provider := newRefreshProviderStub()
	rc := &RunContext{
		Run:                  run,
		Pool:                 pool,
		TraceID:              "trace-heartbeat-skip",
		HeartbeatRun:         true,
		FinalAssistantOutput: "assistant reply",
		RunToolCallCount:     3,
	}

	distillAfterRun(provider, NewPgxMemorySnapshotStore(pool), pool, nil, rc, ident, []memory.MemoryMessage{
		{Role: "user", Content: "heartbeat payload"},
	}, nil, nil)

	time.Sleep(80 * time.Millisecond)

	provider.mu.Lock()
	appendCount := provider.appendCount
	commitCount := provider.commitCount
	provider.mu.Unlock()
	if appendCount != 0 || commitCount != 0 {
		t.Fatalf("expected heartbeat run to skip auto distill, append=%d commit=%d", appendCount, commitCount)
	}

	var count int
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*)
		  FROM run_events
		 WHERE run_id = $1
	`, run.ID).Scan(&count); err != nil {
		t.Fatalf("count run events: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no distill events for heartbeat run, got %d", count)
	}
}

func TestHeartbeatPrepareMiddlewareDoesNotDuplicateHeartbeatDecisionTool(t *testing.T) {
	rc := &RunContext{
		InputJSON:     map[string]any{"run_kind": "heartbeat"},
		JobPayload:    map[string]any{"run_kind": "heartbeat"},
		AllowlistSet:  map[string]struct{}{},
		ToolExecutors: map[string]tools.Executor{},
		ToolSpecs:     []llm.ToolSpec{heartbeattool.Spec},
		PersonaDefinition: &personas.Definition{
			CoreTools: []string{heartbeattool.ToolName},
		},
	}

	mw := NewHeartbeatPrepareMiddleware()
	if err := mw(context.Background(), rc, func(_ context.Context, rc *RunContext) error {
		if len(rc.Messages) != 0 {
			t.Fatalf("expected heartbeat middleware not to mutate base messages, got %d", len(rc.Messages))
		}
		text := rc.MaterializedRuntimePrompt()
		if !strings.Contains(text, "[SYSTEM_HEARTBEAT_CHECK]") {
			t.Fatalf("expected SYSTEM_HEARTBEAT_CHECK marker, got %q", text)
		}
		count := 0
		for _, spec := range rc.ToolSpecs {
			if spec.Name == heartbeattool.ToolName {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("expected one heartbeat tool spec, got %d", count)
		}
		coreCount := 0
		for _, name := range rc.PersonaDefinition.CoreTools {
			if name == heartbeattool.ToolName {
				coreCount++
			}
		}
		if coreCount != 1 {
			t.Fatalf("expected one heartbeat core tool, got %d", coreCount)
		}
		return nil
	}); err != nil {
		t.Fatalf("heartbeat middleware: %v", err)
	}
}

func TestScheduleSnapshotRefreshKeepsRetryingAfterTransientErrors(t *testing.T) {
	withShortSnapshotRefresh(t)
	pool, run, ident := setupMemoryRun(t, "memory_snapshot_retry_errors")
	repoRetry := data.MemorySnapshotRepository{}
	if err := repoRetry.Upsert(context.Background(), pool, ident.AccountID, ident.UserID, ident.AgentID, "\n\n<memory>\n- old\n</memory>"); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}

	provider := newRefreshProviderStub()
	provider.errSeq = []error{errors.New("temporary"), nil}
	provider.findSeq = [][]memory.MemoryHit{
		nil,
		{{URI: "viking://user/memories/recovered", Abstract: "recovered memory", Score: 0.8, IsLeaf: true}},
	}

	snap := NewPgxMemorySnapshotStore(pool)
	scheduleSnapshotRefresh(provider, snap, pool, run.ID, "trace-retry", ident, "", map[string][]string{
		string(memory.MemoryScopeUser): {"recovered memory"},
	}, "", "write")

	waitForSnapshotBlock(t, pool, ident, "\n\n<memory>\n- recovered memory\n</memory>")
}
