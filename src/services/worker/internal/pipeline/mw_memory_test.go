package pipeline_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

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
	appendErr   error
	commitErr   error

	findCalled    bool
	contentCalled bool
	appendCalled  bool
	commitCalled  bool

	appendDone chan struct{} // 关闭时通知 commit goroutine 已完成
}

func newMemMock() *memMock {
	return &memMock{appendDone: make(chan struct{})}
}

func (m *memMock) Find(_ context.Context, _ memory.MemoryIdentity, _ memory.MemoryScope, _ string, _ int) ([]memory.MemoryHit, error) {
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

func (m *memMock) AppendSessionMessages(_ context.Context, _ memory.MemoryIdentity, _ string, _ []memory.MemoryMessage) error {
	m.mu.Lock()
	m.appendCalled = true
	m.mu.Unlock()
	return m.appendErr
}

func (m *memMock) CommitSession(_ context.Context, _ memory.MemoryIdentity, _ string) error {
	m.mu.Lock()
	m.commitCalled = true
	m.mu.Unlock()
	close(m.appendDone)
	return m.commitErr
}

func (m *memMock) Write(_ context.Context, _ memory.MemoryIdentity, _ memory.MemoryScope, _ memory.MemoryEntry) error {
	return nil
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
	if userMsg != "" {
		msgs = []llm.Message{
			{Role: "user", Content: []llm.TextPart{{Text: userMsg}}},
		}
	}
	rc := &pipeline.RunContext{
		Run: data.Run{
			ID:       uuid.New(),
			OrgID:    uuid.New(),
			ThreadID: uuid.New(),
		},
		UserID:               userID,
		Messages:             msgs,
		FinalAssistantOutput: assistantOutput,
	}
	return rc
}

// --- tests ---

func TestMemoryMiddleware_NilProvider_NoOp(t *testing.T) {
	mw := pipeline.NewMemoryMiddleware(nil)

	called := false
	terminal := func(_ context.Context, rc *pipeline.RunContext) error {
		called = true
		return nil
	}

	rc := buildMemRC(userIDPtr(), "hello", "")
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, terminal)
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected next to be called when provider is nil")
	}
}

func TestMemoryMiddleware_NilUserID_NoOp(t *testing.T) {
	mp := newMemMock()
	mw := pipeline.NewMemoryMiddleware(mp)

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
	mp.findHits = []memory.MemoryHit{
		{URI: "viking://user/memories/prefs/lang", Abstract: "user prefers Go", Score: 0.7, IsLeaf: true},
	}
	mw := pipeline.NewMemoryMiddleware(mp)

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
	mp.findHits = []memory.MemoryHit{}
	mw := pipeline.NewMemoryMiddleware(mp)

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
	mw := pipeline.NewMemoryMiddleware(mp)

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

func TestMemoryMiddleware_CommitCalledAfterRun(t *testing.T) {
	mp := newMemMock()
	mp.findHits = []memory.MemoryHit{}
	mw := pipeline.NewMemoryMiddleware(mp)

	rc := buildMemRC(userIDPtr(), "user message", "assistant reply")
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// goroutine 异步执行，等待完成信号
	select {
	case <-mp.appendDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for CommitSession to be called")
	}

	mp.mu.Lock()
	appendCalled := mp.appendCalled
	commitCalled := mp.commitCalled
	mp.mu.Unlock()

	if !appendCalled {
		t.Fatal("expected AppendSessionMessages to be called")
	}
	if !commitCalled {
		t.Fatal("expected CommitSession to be called")
	}
}

func TestMemoryMiddleware_NoCommitWhenEmpty(t *testing.T) {
	mp := newMemMock()
	mp.findHits = []memory.MemoryHit{}
	mw := pipeline.NewMemoryMiddleware(mp)

	// userQuery 为空时不触发 commit
	rc := buildMemRC(userIDPtr(), "", "assistant reply")
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error { return nil })
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 短暂等待，确认 goroutine 没有被启动
	time.Sleep(50 * time.Millisecond)
	mp.mu.Lock()
	appendCalled := mp.appendCalled
	mp.mu.Unlock()
	if appendCalled {
		t.Fatal("AppendSessionMessages should not be called when userQuery is empty")
	}
}

func TestMemoryMiddleware_HighScoreNonLeaf_FetchesL1(t *testing.T) {
	mp := newMemMock()
	mp.findHits = []memory.MemoryHit{
		{
			URI:      "viking://user/memories/prefs/lang",
			Abstract: "user preferences",
			Score:    0.90, // >= 0.85
			IsLeaf:   false,
		},
	}
	mp.contentText = "user prefers Go with modules"
	mw := pipeline.NewMemoryMiddleware(mp)

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
