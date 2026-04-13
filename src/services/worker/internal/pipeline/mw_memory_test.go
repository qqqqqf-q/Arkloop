package pipeline_test

import (
	"context"
	"errors"
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

func (m *memMock) ListDir(_ context.Context, _ memory.MemoryIdentity, _ string) ([]string, error) {
	return nil, nil
}

// --- helpers ---

func userIDPtr() *uuid.UUID {
	uid := uuid.New()
	return &uid
}

// memSnapStub 供测试提供 memory 快照读取结果。
type memSnapStub struct {
	block string
	found bool
	err   error
}

func (s *memSnapStub) Get(_ context.Context, _, _ uuid.UUID, _ string) (string, bool, error) {
	if s.err != nil {
		return "", false, s.err
	}
	if s.found {
		return s.block, true, nil
	}
	return "", false, nil
}

func (s *memSnapStub) UpsertWithHits(_ context.Context, _, _ uuid.UUID, _, _ string, _ []data.MemoryHitCache) error {
	return nil
}

type notebookSnapshotReaderStub struct {
	block string
	err   error
}

func (s *notebookSnapshotReaderStub) GetSnapshot(_ context.Context, _, _ uuid.UUID, _ string) (string, error) {
	if s == nil {
		return "", nil
	}
	return s.block, s.err
}

type impressionStoreStub struct {
	impression string
	found      bool
	err        error
}

func (s *impressionStoreStub) Get(_ context.Context, _, _ uuid.UUID, _ string) (string, bool, error) {
	if s.err != nil {
		return "", false, s.err
	}
	if s.found {
		return s.impression, true, nil
	}
	return "", false, nil
}

func (s *impressionStoreStub) Upsert(_ context.Context, _, _ uuid.UUID, _, _ string) error {
	return nil
}

func (s *impressionStoreStub) AddScore(_ context.Context, _, _ uuid.UUID, _ string, _ int) (int, error) {
	return 0, nil
}

func (s *impressionStoreStub) ResetScore(_ context.Context, _, _ uuid.UUID, _ string) error {
	return nil
}

type threadObserverStub struct {
	called chan pipeline.ThreadPersistResult
}

func (s *threadObserverStub) HookProviderName() string { return "thread_observer_stub" }

func (s *threadObserverStub) AfterThreadPersist(_ context.Context, _ *pipeline.RunContext, _ pipeline.ThreadDelta, result pipeline.ThreadPersistResult) (pipeline.PersistObservers, error) {
	if s.called != nil {
		s.called <- result
	}
	return nil, nil
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
		ThreadPersistReady:   true,
		PendingMemoryWrites:  memory.NewPendingWriteBuffer(),
	}
}

type beforeThreadPersistHookStub struct {
	called bool
}

func (s *beforeThreadPersistHookStub) HookProviderName() string { return "before_thread" }
func (s *beforeThreadPersistHookStub) BeforeThreadPersist(context.Context, *pipeline.RunContext, pipeline.ThreadDelta) (pipeline.ThreadPersistHints, error) {
	s.called = true
	return pipeline.ThreadPersistHints{{Key: "provider", Value: "external", Priority: 1}}, nil
}

type threadProviderStub struct {
	called bool
	hints  pipeline.ThreadPersistHints
	result pipeline.ThreadPersistResult
}

func (s *threadProviderStub) HookProviderName() string { return "thread_provider" }
func (s *threadProviderStub) PersistThread(_ context.Context, _ *pipeline.RunContext, _ pipeline.ThreadDelta, hints pipeline.ThreadPersistHints) pipeline.ThreadPersistResult {
	s.called = true
	s.hints = append(pipeline.ThreadPersistHints(nil), hints...)
	return s.result
}

type afterThreadPersistHookStub struct {
	called bool
}

func (s *afterThreadPersistHookStub) HookProviderName() string { return "after_thread" }
func (s *afterThreadPersistHookStub) AfterThreadPersist(context.Context, *pipeline.RunContext, pipeline.ThreadDelta, pipeline.ThreadPersistResult) (pipeline.PersistObservers, error) {
	s.called = true
	return nil, nil
}

func buildHookRuntime(
	t *testing.T,
	contributors []pipeline.ContextContributor,
	afterHooks []pipeline.AfterThreadPersistHook,
	threadProvider pipeline.ThreadPersistenceProvider,
) *pipeline.HookRuntime {
	t.Helper()

	registry := pipeline.NewHookRegistry()
	for _, contributor := range contributors {
		registry.RegisterContextContributor(contributor)
	}
	for _, hook := range afterHooks {
		registry.RegisterAfterThreadPersistHook(hook)
	}
	if threadProvider != nil {
		if err := registry.SetThreadPersistenceProvider(threadProvider); err != nil {
			t.Fatalf("set thread provider: %v", err)
		}
	}
	return pipeline.NewHookRuntime(registry, pipeline.NewDefaultHookResultApplier())
}

// --- tests ---

func TestMemoryMiddleware_NilProvider_NoOp(t *testing.T) {
	mw := pipeline.NewMemoryMiddleware(nil, nil, nil, nil, nil, nil)

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
	mw := pipeline.NewMemoryMiddleware(mp, nil, nil, nil, nil, nil)

	rc := buildMemRC(nil, "test query", "response")
	rc.PendingMemoryWrites.Append(memory.PendingWrite{
		Ident: memory.MemoryIdentity{AccountID: uuid.New(), UserID: uuid.New(), AgentID: "default"},
		Scope: memory.MemoryScopeUser,
		Entry: memory.MemoryEntry{Content: "[user/preferences/style] concise"},
	})
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	mp.mu.Lock()
	writeCalled := mp.writeCalled
	mp.mu.Unlock()
	if writeCalled {
		t.Fatal("Write should not be called when UserID is nil")
	}
}

func TestPromptHookMiddleware_InjectsImpressionWithoutMemoryBlock(t *testing.T) {
	impStore := &impressionStoreStub{
		found:      true,
		impression: "owner prefers Go for backend work",
	}
	mw := pipeline.NewPromptHookMiddleware()

	rc := buildMemRC(userIDPtr(), "what language do you prefer?", "")
	rc.HookRuntime = buildHookRuntime(t, []pipeline.ContextContributor{
		pipeline.NewImpressionContextContributor(impStore),
	}, nil, nil)
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(rc.SystemPrompt, "<impression>") {
		t.Fatalf("expected <impression> block in SystemPrompt, got: %q", rc.SystemPrompt)
	}
	if !strings.Contains(rc.SystemPrompt, "owner prefers Go for backend work") {
		t.Fatalf("expected impression text in SystemPrompt, got: %q", rc.SystemPrompt)
	}
	if strings.Contains(rc.SystemPrompt, "<memory>") {
		t.Fatalf("expected no <memory> block in SystemPrompt, got: %q", rc.SystemPrompt)
	}
}

func TestPromptHookMiddleware_NotebookAndImpressionBlocksCanCoexist(t *testing.T) {
	notebookReader := &notebookSnapshotReaderStub{
		block: "\n\n<notebook>\n- stable note\n</notebook>",
	}
	impStore := &impressionStoreStub{
		found:      true,
		impression: "owner values concise answers",
	}
	mw := pipeline.NewPromptHookMiddleware()

	rc := buildMemRC(userIDPtr(), "hello", "")
	rc.HookRuntime = buildHookRuntime(t, []pipeline.ContextContributor{
		pipeline.NewNotebookContextContributor(notebookReader),
		pipeline.NewImpressionContextContributor(impStore),
	}, nil, nil)
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(rc.SystemPrompt, "<notebook>") || !strings.Contains(rc.SystemPrompt, "<impression>") {
		t.Fatalf("expected notebook and impression blocks, got: %q", rc.SystemPrompt)
	}
	if strings.Index(rc.SystemPrompt, "<notebook>") > strings.Index(rc.SystemPrompt, "<impression>") {
		t.Fatalf("expected notebook block before impression block, got: %q", rc.SystemPrompt)
	}
	if strings.Contains(rc.SystemPrompt, "<memory>") {
		t.Fatalf("expected no <memory> block in SystemPrompt, got: %q", rc.SystemPrompt)
	}
}

func TestMemoryMiddleware_FlushesPendingWritesAfterRun(t *testing.T) {
	mp := newMemMock()
	mw := pipeline.NewMemoryMiddleware(mp, nil, nil, nil, nil, nil)

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
	mw := pipeline.NewMemoryMiddleware(mp, nil, nil, nil, nil, nil)

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
	mw := pipeline.NewMemoryMiddleware(mp, nil, nil, nil, nil, nil)

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

func TestMemoryMiddleware_UsesRunContextMemoryProviderForPendingWritesWhenStaticProviderNil(t *testing.T) {
	mp := newMemMock()
	mw := pipeline.NewMemoryMiddleware(nil, nil, nil, nil, nil, nil)
	rc := buildMemRC(userIDPtr(), "hello", "")
	rc.MemoryProvider = mp
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
}

func TestPromptHookMiddleware_SkipsInjectionForImpressionRun(t *testing.T) {
	impStore := &impressionStoreStub{
		found:      true,
		impression: "owner profile should not be injected during impression run",
	}
	mw := pipeline.NewPromptHookMiddleware()

	rc := buildMemRC(userIDPtr(), "rebuild impression", "")
	rc.SystemPrompt = "base prompt"
	rc.InputJSON = map[string]any{"run_kind": "impression"}
	rc.HookRuntime = buildHookRuntime(t, []pipeline.ContextContributor{
		pipeline.NewImpressionContextContributor(impStore),
	}, nil, nil)

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rc.SystemPrompt != "base prompt" {
		t.Fatalf("expected system prompt unchanged for impression run, got: %q", rc.SystemPrompt)
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

func TestThreadPersistHookMiddleware_DistillTriggeredWithIncrementalUserInput(t *testing.T) {
	mp := newMemMock()
	mw := pipeline.NewThreadPersistHookMiddleware()

	rc := buildMemRC(userIDPtr(), "help me search", "found 3 results")
	rc.MemoryProvider = mp
	rc.HookRuntime = buildHookRuntime(t, nil, []pipeline.AfterThreadPersistHook{
		pipeline.NewLegacyMemoryDistillObserver(nil, nil, nil, nil, nil),
	}, nil)

	h := pipeline.Build([]pipeline.RunMiddleware{
		pipeline.NewPromptHookMiddleware(),
		mw,
	}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
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

func TestThreadPersistHookMiddleware_DistillTriggeredWithoutToolOrIterationThreshold(t *testing.T) {
	mp := newMemMock()
	mw := pipeline.NewThreadPersistHookMiddleware()

	rc := buildMemRC(userIDPtr(), "complex question", "detailed answer")
	rc.MemoryProvider = mp
	rc.HookRuntime = buildHookRuntime(t, nil, []pipeline.AfterThreadPersistHook{
		pipeline.NewLegacyMemoryDistillObserver(nil, nil, nil, nil, nil),
	}, nil)

	h := pipeline.Build([]pipeline.RunMiddleware{
		pipeline.NewPromptHookMiddleware(),
		mw,
	}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
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

func TestThreadPersistHookMiddleware_DistillIncludesRuntimeUserMessages(t *testing.T) {
	mp := newMemMock()
	mw := pipeline.NewThreadPersistHookMiddleware()

	rc := buildMemRC(userIDPtr(), "first prompt", "final answer")
	rc.MemoryProvider = mp
	rc.HookRuntime = buildHookRuntime(t, nil, []pipeline.AfterThreadPersistHook{
		pipeline.NewLegacyMemoryDistillObserver(nil, nil, nil, nil, nil),
	}, nil)

	h := pipeline.Build([]pipeline.RunMiddleware{
		pipeline.NewPromptHookMiddleware(),
		mw,
	}, func(_ context.Context, rc *pipeline.RunContext) error {
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

func TestThreadPersistHookMiddleware_DistillSkippedWhenNoIncrementalInput(t *testing.T) {
	mp := newMemMock()
	mw := pipeline.NewThreadPersistHookMiddleware()

	rc := buildMemRC(userIDPtr(), "", "simple answer")
	rc.MemoryProvider = mp
	rc.HookRuntime = buildHookRuntime(t, nil, []pipeline.AfterThreadPersistHook{
		pipeline.NewLegacyMemoryDistillObserver(nil, nil, nil, nil, nil),
	}, nil)

	h := pipeline.Build([]pipeline.RunMiddleware{
		pipeline.NewPromptHookMiddleware(),
		mw,
	}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
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

func TestThreadPersistHookMiddleware_DistillSkippedWhenDisabled(t *testing.T) {
	mp := newMemMock()
	resolver := &configResolverStub{values: map[string]string{
		"memory.distill_enabled": "false",
	}}
	mw := pipeline.NewThreadPersistHookMiddleware()

	rc := buildMemRC(userIDPtr(), "query", "response")
	rc.MemoryProvider = mp
	rc.HookRuntime = buildHookRuntime(t, nil, []pipeline.AfterThreadPersistHook{
		pipeline.NewLegacyMemoryDistillObserver(nil, nil, resolver, nil, nil),
	}, nil)

	h := pipeline.Build([]pipeline.RunMiddleware{
		pipeline.NewPromptHookMiddleware(),
		mw,
	}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
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

func TestThreadPersistHookMiddleware_DistillSkippedWhenNoAssistantOutput(t *testing.T) {
	mp := newMemMock()
	mw := pipeline.NewThreadPersistHookMiddleware()

	rc := buildMemRC(userIDPtr(), "query", "")
	rc.MemoryProvider = mp
	rc.HookRuntime = buildHookRuntime(t, nil, []pipeline.AfterThreadPersistHook{
		pipeline.NewLegacyMemoryDistillObserver(nil, nil, nil, nil, nil),
	}, nil)

	h := pipeline.Build([]pipeline.RunMiddleware{
		pipeline.NewPromptHookMiddleware(),
		mw,
	}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
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

func TestThreadPersistHookMiddleware_RunsHooksAfterPostPersistError(t *testing.T) {
	before := &beforeThreadPersistHookStub{}
	provider := &threadProviderStub{
		result: pipeline.ThreadPersistResult{Handled: true, Provider: "thread_provider"},
	}
	after := &afterThreadPersistHookStub{}

	registry := pipeline.NewHookRegistry()
	registry.RegisterBeforeThreadPersistHook(before)
	if err := registry.SetThreadPersistenceProvider(provider); err != nil {
		t.Fatalf("set thread provider: %v", err)
	}
	registry.RegisterAfterThreadPersistHook(after)

	rc := buildMemRC(userIDPtr(), "user prompt", "assistant output")
	rc.HookRuntime = pipeline.NewHookRuntime(registry, pipeline.NewDefaultHookResultApplier())

	mw := pipeline.NewThreadPersistHookMiddleware()
	wantErr := context.Canceled
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error {
		return wantErr
	})
	if err := h(context.Background(), rc); !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
	if !before.called {
		t.Fatal("expected before thread persist hook to run")
	}
	if !provider.called {
		t.Fatal("expected thread provider to run")
	}
	if !after.called {
		t.Fatal("expected after thread persist hook to run")
	}
	if len(provider.hints) != 1 || provider.hints[0].Key != "provider" {
		t.Fatalf("unexpected hints: %#v", provider.hints)
	}
}

func TestThreadPersistHookMiddleware_SkipsWhenThreadPersistNotReady(t *testing.T) {
	provider := &threadProviderStub{
		result: pipeline.ThreadPersistResult{Handled: true, Provider: "thread_provider"},
	}
	registry := pipeline.NewHookRegistry()
	if err := registry.SetThreadPersistenceProvider(provider); err != nil {
		t.Fatalf("set thread provider: %v", err)
	}

	rc := buildMemRC(userIDPtr(), "user prompt", "assistant output")
	rc.ThreadPersistReady = false
	rc.HookRuntime = pipeline.NewHookRuntime(registry, pipeline.NewDefaultHookResultApplier())

	mw := pipeline.NewThreadPersistHookMiddleware()
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.called {
		t.Fatal("expected thread provider to be skipped when thread persist is not ready")
	}
}

func TestThreadPersistHookMiddleware_SkipsForImpressionRun(t *testing.T) {
	provider := &threadProviderStub{
		result: pipeline.ThreadPersistResult{Handled: true, Provider: "thread_provider"},
	}
	registry := pipeline.NewHookRegistry()
	if err := registry.SetThreadPersistenceProvider(provider); err != nil {
		t.Fatalf("set thread provider: %v", err)
	}

	rc := buildMemRC(userIDPtr(), "user prompt", "assistant output")
	rc.InputJSON = map[string]any{"run_kind": "impression"}
	rc.ImpressionRun = true
	rc.HookRuntime = pipeline.NewHookRuntime(registry, pipeline.NewDefaultHookResultApplier())

	mw := pipeline.NewThreadPersistHookMiddleware()
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.called {
		t.Fatal("expected thread provider to be skipped for impression runs")
	}
}

func TestThreadPersistHookMiddleware_DistillStillRunsWhenThreadProviderFails(t *testing.T) {
	mp := newMemMock()
	provider := &threadProviderStub{
		result: pipeline.ThreadPersistResult{
			Handled:  true,
			Provider: "thread_provider",
			Err:      context.DeadlineExceeded,
		},
	}

	registry := pipeline.NewHookRegistry()
	if err := registry.SetThreadPersistenceProvider(provider); err != nil {
		t.Fatalf("set thread provider: %v", err)
	}
	registry.RegisterAfterThreadPersistHook(pipeline.NewLegacyMemoryDistillObserver(nil, nil, nil, nil, nil))

	rc := buildMemRC(userIDPtr(), "user prompt", "assistant output")
	rc.MemoryProvider = mp
	rc.HookRuntime = pipeline.NewHookRuntime(registry, pipeline.NewDefaultHookResultApplier())

	mw := pipeline.NewThreadPersistHookMiddleware()
	h := pipeline.Build([]pipeline.RunMiddleware{
		pipeline.NewPromptHookMiddleware(),
		mw,
	}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-mp.distillDone:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for distill commit after provider failure")
	}
}

func TestThreadPersistHookRunsBeforeOuterDeliveryError(t *testing.T) {
	mw := pipeline.NewThreadPersistHookMiddleware()
	observer := &threadObserverStub{called: make(chan pipeline.ThreadPersistResult, 1)}

	rc := buildMemRC(userIDPtr(), "query", "response")
	rc.HookRuntime = buildHookRuntime(t, nil, []pipeline.AfterThreadPersistHook{observer}, nil)

	deliveryErr := fmt.Errorf("delivery failed")
	h := pipeline.Build([]pipeline.RunMiddleware{
		func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
			if err := next(ctx, rc); err != nil {
				return err
			}
			return deliveryErr
		},
		mw,
	}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })

	if err := h(context.Background(), rc); err == nil || err.Error() != deliveryErr.Error() {
		t.Fatalf("expected delivery error, got %v", err)
	}

	select {
	case <-observer.called:
	case <-time.After(time.Second):
		t.Fatal("expected thread persist observer to run before delivery error returned")
	}
}
