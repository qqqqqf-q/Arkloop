package pipeline_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/pipeline"

	"github.com/google/uuid"
)

// --- mock MemoryProvider ---

type memMock struct {
	mu sync.Mutex

	findHits    []memory.MemoryHit
	findErr     error
	contentText string
	contentErr  error
	writeErr    error

	findCalled    bool
	contentCalled bool
	writeCalled   bool
	writeCount    int
	writeTargets  []string
	writeEntries  []memory.MemoryEntry

	appendCalled    bool
	appendMsgs      []memory.MemoryMessage
	appendSessionID string
	commitCalled    bool
	commitSessionID string

	writeDone   chan struct{}
	distillDone chan struct{}
}

func newMemMock() *memMock {
	return &memMock{
		writeDone:   make(chan struct{}, 8),
		distillDone: make(chan struct{}, 8),
	}
}

func (m *memMock) Find(_ context.Context, _ memory.MemoryIdentity, _ string, _ string, _ int) ([]memory.MemoryHit, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.findCalled = true
	return m.findHits, m.findErr
}

func (m *memMock) Content(_ context.Context, _ memory.MemoryIdentity, _ string, _ memory.MemoryLayer) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.contentCalled = true
	return m.contentText, m.contentErr
}

func (m *memMock) AppendSessionMessages(_ context.Context, _ memory.MemoryIdentity, sessionID string, msgs []memory.MemoryMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appendCalled = true
	m.appendSessionID = sessionID
	m.appendMsgs = msgs
	return nil
}

func (m *memMock) CommitSession(_ context.Context, _ memory.MemoryIdentity, sessionID string) error {
	m.mu.Lock()
	m.commitCalled = true
	m.commitSessionID = sessionID
	m.mu.Unlock()
	m.distillDone <- struct{}{}
	return nil
}

func (m *memMock) Write(_ context.Context, _ memory.MemoryIdentity, _ memory.MemoryScope, entry memory.MemoryEntry) error {
	m.mu.Lock()
	m.writeCalled = true
	m.writeCount++
	m.writeTargets = append(m.writeTargets, "self")
	m.writeEntries = append(m.writeEntries, entry)
	m.mu.Unlock()
	m.writeDone <- struct{}{}
	return m.writeErr
}

func (m *memMock) Delete(_ context.Context, _ memory.MemoryIdentity, _ string) error {
	return nil
}

// --- helpers ---

func userIDPtr() *uuid.UUID {
	uid := uuid.New()
	return &uid
}

func buildMemRC(userID *uuid.UUID, userMsg string, assistantOutput string) *pipeline.RunContext {
	var msgs []llm.Message
	var ids []uuid.UUID
	if userMsg != "" {
		msgs = []llm.Message{{Role: "user", Content: []llm.TextPart{{Text: userMsg}}}}
		ids = []uuid.UUID{uuid.New()}
	}
	return &pipeline.RunContext{
		Run: data.Run{
			ID:        uuid.New(),
			AccountID: uuid.New(),
			ThreadID:  uuid.New(),
		},
		UserID:               userID,
		Messages:             msgs,
		ThreadMessageIDs:     ids,
		FinalAssistantOutput: assistantOutput,
		PendingMemoryWrites:  memory.NewPendingWriteBuffer(),
	}
}

// --- tests ---

func TestMemoryMiddleware_NilProvider_NoOp(t *testing.T) {
	mw := pipeline.NewMemoryMiddleware(nil, nil, nil)

	called := false
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error {
		called = true
		return nil
	})

	rc := buildMemRC(userIDPtr(), "hello", "")
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected next to be called when provider is nil")
	}
}

func TestMemoryMiddleware_NilUserID_NoOp(t *testing.T) {
	mp := newMemMock()
	mw := pipeline.NewMemoryMiddleware(mp, nil, nil)

	rc := buildMemRC(nil, "test query", "response")
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mp.findCalled {
		t.Fatal("Find should not be called when UserID is nil")
	}
}

func TestMemoryMiddleware_InjectsMemoryBlock(t *testing.T) {
	mp := newMemMock()
	mp.findHits = []memory.MemoryHit{{URI: "viking://user/memories/prefs/lang", Abstract: "user prefers Go", Score: 0.7, IsLeaf: true}}
	mw := pipeline.NewMemoryMiddleware(mp, nil, nil)

	rc := buildMemRC(userIDPtr(), "what language do you prefer?", "")
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(rc.SystemPrompt, "<memory>") {
		t.Fatalf("expected <memory> block in SystemPrompt, got: %q", rc.SystemPrompt)
	}
	if !strings.Contains(rc.SystemPrompt, "user prefers Go") {
		t.Fatalf("expected abstract in SystemPrompt, got: %q", rc.SystemPrompt)
	}
}

func TestMemoryMiddleware_NoHits_NoInjection(t *testing.T) {
	mp := newMemMock()
	mw := pipeline.NewMemoryMiddleware(mp, nil, nil)

	rc := buildMemRC(userIDPtr(), "hello", "")
	original := rc.SystemPrompt
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.SystemPrompt != original {
		t.Fatalf("SystemPrompt changed unexpectedly: %q", rc.SystemPrompt)
	}
}

func TestMemoryMiddleware_FindError_Continues(t *testing.T) {
	mp := newMemMock()
	mp.findErr = context.DeadlineExceeded
	mw := pipeline.NewMemoryMiddleware(mp, nil, nil)

	nextCalled := false
	rc := buildMemRC(userIDPtr(), "query", "")
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error {
		nextCalled = true
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !nextCalled {
		t.Fatal("expected next to be called even when Find errors")
	}
}

func TestMemoryMiddleware_FlushesPendingWritesAfterRun(t *testing.T) {
	mp := newMemMock()
	mw := pipeline.NewMemoryMiddleware(mp, nil, nil)

	rc := buildMemRC(userIDPtr(), "user message", "assistant reply")
	rc.PendingMemoryWrites.Append(memory.PendingWrite{
		Ident: memory.MemoryIdentity{AccountID: rc.Run.AccountID, UserID: *rc.UserID, AgentID: "default"},
		Scope: memory.MemoryScopeUser,
		Entry: memory.MemoryEntry{Content: "[user/preferences/language] user prefers Go"},
	})

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-mp.writeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for deferred write")
	}

	mp.mu.Lock()
	writeCalled := mp.writeCalled
	writeCount := mp.writeCount
	writeEntry := mp.writeEntries[0]
	mp.mu.Unlock()

	if !writeCalled {
		t.Fatal("expected provider.Write to be called")
	}
	if writeCount != 1 {
		t.Fatalf("expected exactly 1 write, got %d", writeCount)
	}
	if writeEntry.Content != "[user/preferences/language] user prefers Go" {
		t.Fatalf("unexpected write entry: %q", writeEntry.Content)
	}
}

func TestMemoryMiddleware_FlushesPendingWritesEvenWhenNextFails(t *testing.T) {
	mp := newMemMock()
	mw := pipeline.NewMemoryMiddleware(mp, nil, nil)

	rc := buildMemRC(userIDPtr(), "user message", "assistant reply")
	rc.PendingMemoryWrites.Append(memory.PendingWrite{
		Ident: memory.MemoryIdentity{AccountID: rc.Run.AccountID, UserID: *rc.UserID, AgentID: "default"},
		Scope: memory.MemoryScopeAgent,
		Entry: memory.MemoryEntry{Content: "[agent/patterns/retry] retry on timeout"},
	})

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error {
		return context.Canceled
	})
	if err := h(context.Background(), rc); err == nil {
		t.Fatal("expected error from next")
	}

	select {
	case <-mp.writeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for deferred write after failure")
	}
}

func TestMemoryMiddleware_NoFlushWhenNoPendingWrites(t *testing.T) {
	mp := newMemMock()
	mw := pipeline.NewMemoryMiddleware(mp, nil, nil)

	rc := buildMemRC(userIDPtr(), "", "assistant reply")
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	mp.mu.Lock()
	writeCalled := mp.writeCalled
	mp.mu.Unlock()
	if writeCalled {
		t.Fatal("Write should not be called when there are no pending writes")
	}
}

func TestMemoryMiddleware_HighScoreNonLeaf_FetchesL1(t *testing.T) {
	mp := newMemMock()
	mp.findHits = []memory.MemoryHit{{
		URI:      "viking://user/memories/prefs/lang",
		Abstract: "user preferences",
		Score:    0.90,
		IsLeaf:   false,
	}}
	mp.contentText = "user prefers Go with modules"
	mw := pipeline.NewMemoryMiddleware(mp, nil, nil)

	rc := buildMemRC(userIDPtr(), "programming preferences", "")
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !mp.contentCalled {
		t.Fatal("expected Content(L1) to be called for high-score non-leaf node")
	}
	if !strings.Contains(rc.SystemPrompt, "user prefers Go with modules") {
		t.Fatalf("expected L1 content in SystemPrompt, got: %q", rc.SystemPrompt)
	}
}

func TestMemoryMiddleware_UsesRunContextMemoryProviderWhenStaticProviderNil(t *testing.T) {
	mp := newMemMock()
	mp.findHits = []memory.MemoryHit{{URI: "u1", Abstract: "remembered"}}
	mw := pipeline.NewMemoryMiddleware(nil, nil, nil)
	rc := buildMemRC(userIDPtr(), "hello", "")
	rc.MemoryProvider = mp
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		if !strings.Contains(rc.SystemPrompt, "remembered") {
			t.Fatalf("expected injected memory block, got %q", rc.SystemPrompt)
		}
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- configResolverStub ---

type configResolverStub struct {
	values map[string]string
}

func (s *configResolverStub) Resolve(_ context.Context, key string, _ sharedconfig.Scope) (string, error) {
	if s == nil || s.values == nil {
		return "", fmt.Errorf("not found")
	}
	v, ok := s.values[key]
	if !ok {
		return "", fmt.Errorf("not found")
	}
	return v, nil
}

func (s *configResolverStub) ResolvePrefix(_ context.Context, _ string, _ sharedconfig.Scope) (map[string]string, error) {
	return nil, fmt.Errorf("not implemented")
}

// --- distill tests ---

func TestMemoryMiddleware_DistillTriggeredWithIncrementalUserInput(t *testing.T) {
	mp := newMemMock()
	mw := pipeline.NewMemoryMiddleware(mp, nil, nil)

	rc := buildMemRC(userIDPtr(), "help me search", "found 3 results")

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-mp.distillDone:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for distill commit")
	}

	mp.mu.Lock()
	defer mp.mu.Unlock()
	if !mp.appendCalled {
		t.Fatal("expected AppendSessionMessages to be called")
	}
	if !mp.commitCalled {
		t.Fatal("expected CommitSession to be called")
	}
	if mp.commitSessionID != rc.Run.ThreadID.String() {
		t.Fatalf("expected session ID %s, got %s", rc.Run.ThreadID.String(), mp.commitSessionID)
	}
	hasUser := false
	hasAssistant := false
	for _, msg := range mp.appendMsgs {
		if msg.Role == "user" {
			hasUser = true
		}
		if msg.Role == "assistant" && strings.Contains(msg.Content, "found 3 results") {
			hasAssistant = true
		}
	}
	if !hasUser || !hasAssistant {
		t.Fatalf("expected both user and assistant messages, got %+v", mp.appendMsgs)
	}
}

func TestMemoryMiddleware_DistillTriggeredWithoutToolOrIterationThreshold(t *testing.T) {
	mp := newMemMock()
	mw := pipeline.NewMemoryMiddleware(mp, nil, nil)

	rc := buildMemRC(userIDPtr(), "complex question", "detailed answer")

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-mp.distillDone:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for distill commit")
	}

	mp.mu.Lock()
	defer mp.mu.Unlock()
	if !mp.commitCalled {
		t.Fatal("expected CommitSession to be called")
	}
}

func TestMemoryMiddleware_DistillIncludesRuntimeUserMessages(t *testing.T) {
	mp := newMemMock()
	mw := pipeline.NewMemoryMiddleware(mp, nil, nil)

	rc := buildMemRC(userIDPtr(), "first prompt", "final answer")

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		rc.AppendRuntimeUserMessage("follow-up prompt")
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-mp.distillDone:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for distill commit")
	}

	mp.mu.Lock()
	defer mp.mu.Unlock()
	gotUsers := []string{}
	for _, msg := range mp.appendMsgs {
		if msg.Role == "user" {
			gotUsers = append(gotUsers, msg.Content)
		}
	}
	if len(gotUsers) != 2 {
		t.Fatalf("expected 2 user messages, got %#v", gotUsers)
	}
	if gotUsers[0] != "first prompt" || gotUsers[1] != "follow-up prompt" {
		t.Fatalf("unexpected distill users: %#v", gotUsers)
	}
}

func TestMemoryMiddleware_DistillSkippedWhenNoIncrementalInput(t *testing.T) {
	mp := newMemMock()
	mw := pipeline.NewMemoryMiddleware(mp, nil, nil)

	rc := buildMemRC(userIDPtr(), "", "simple answer")

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	mp.mu.Lock()
	defer mp.mu.Unlock()
	if mp.appendCalled {
		t.Fatal("AppendSessionMessages should not be called without incremental user input")
	}
	if mp.commitCalled {
		t.Fatal("CommitSession should not be called without incremental user input")
	}
}

func TestMemoryMiddleware_DistillSkippedWhenDisabled(t *testing.T) {
	mp := newMemMock()
	resolver := &configResolverStub{values: map[string]string{
		"memory.distill_enabled": "false",
	}}
	mw := pipeline.NewMemoryMiddleware(mp, nil, resolver)

	rc := buildMemRC(userIDPtr(), "query", "response")

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	mp.mu.Lock()
	defer mp.mu.Unlock()
	if mp.commitCalled {
		t.Fatal("CommitSession should not be called when distill is disabled")
	}
}

func TestMemoryMiddleware_DistillSkippedWhenNoAssistantOutput(t *testing.T) {
	mp := newMemMock()
	mw := pipeline.NewMemoryMiddleware(mp, nil, nil)

	rc := buildMemRC(userIDPtr(), "query", "")

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	mp.mu.Lock()
	defer mp.mu.Unlock()
	if mp.commitCalled {
		t.Fatal("CommitSession should not be called when FinalAssistantOutput is empty")
	}
}
