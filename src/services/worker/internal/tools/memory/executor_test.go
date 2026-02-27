package memory

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

// --- mock ---

type mockProvider struct {
	findHits    []memory.MemoryHit
	findErr     error
	contentText string
	contentErr  error
	writeErr    error
	deleteErr   error

	findCalled    bool
	contentCalled bool
	writeCalled   bool
	deleteCalled  bool
	lastWriteEntry memory.MemoryEntry
	lastDeleteURI  string
}

func (m *mockProvider) Find(_ context.Context, _ memory.MemoryIdentity, _ memory.MemoryScope, _ string, _ int) ([]memory.MemoryHit, error) {
	m.findCalled = true
	return m.findHits, m.findErr
}

func (m *mockProvider) Content(_ context.Context, _ memory.MemoryIdentity, _ string, _ memory.MemoryLayer) (string, error) {
	m.contentCalled = true
	return m.contentText, m.contentErr
}

func (m *mockProvider) AppendSessionMessages(_ context.Context, _ memory.MemoryIdentity, _ string, _ []memory.MemoryMessage) error {
	return nil
}

func (m *mockProvider) CommitSession(_ context.Context, _ memory.MemoryIdentity, _ string) error {
	return nil
}

func (m *mockProvider) Write(_ context.Context, _ memory.MemoryIdentity, _ memory.MemoryScope, entry memory.MemoryEntry) error {
	m.writeCalled = true
	m.lastWriteEntry = entry
	return m.writeErr
}

func (m *mockProvider) Delete(_ context.Context, _ memory.MemoryIdentity, uri string) error {
	m.deleteCalled = true
	m.lastDeleteURI = uri
	return m.deleteErr
}

// --- helpers ---

func newExecCtx(userID *uuid.UUID) tools.ExecutionContext {
	orgID := uuid.New()
	return tools.ExecutionContext{
		RunID:   uuid.New(),
		TraceID: "test-trace",
		OrgID:   &orgID,
		UserID:  userID,
		AgentID: "test-agent",
		Emitter: events.NewEmitter("test-trace"),
	}
}

func newUserExecCtx() tools.ExecutionContext {
	uid := uuid.New()
	return newExecCtx(&uid)
}

// --- search ---

func TestMemoryExecutor_Search_Success(t *testing.T) {
	mp := &mockProvider{
		findHits: []memory.MemoryHit{
			{URI: "viking://user/memories/preferences/lang", Abstract: "user speaks English", Score: 0.9},
		},
	}
	ex := NewToolExecutor(mp)
	result := ex.Execute(context.Background(), "memory_search", map[string]any{"query": "language"}, newUserExecCtx(), "")

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	hits, _ := result.ResultJSON["hits"].([]map[string]any)
	if len(hits) == 0 {
		// 兼容 []interface{} 类型
		raw, _ := json.Marshal(result.ResultJSON["hits"])
		var arr []map[string]any
		_ = json.Unmarshal(raw, &arr)
		hits = arr
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d; result: %+v", len(hits), result.ResultJSON)
	}
	if hits[0]["uri"] != "viking://user/memories/preferences/lang" {
		t.Fatalf("unexpected uri: %v", hits[0]["uri"])
	}
}

func TestMemoryExecutor_Search_EmptyQuery(t *testing.T) {
	ex := NewToolExecutor(&mockProvider{})
	result := ex.Execute(context.Background(), "memory_search", map[string]any{"query": ""}, newUserExecCtx(), "")
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid, got: %+v", result.Error)
	}
}

func TestMemoryExecutor_Search_ProviderError(t *testing.T) {
	mp := &mockProvider{findErr: errors.New("connection refused")}
	ex := NewToolExecutor(mp)
	result := ex.Execute(context.Background(), "memory_search", map[string]any{"query": "test"}, newUserExecCtx(), "")
	if result.Error == nil || result.Error.ErrorClass != errorProviderFailure {
		t.Fatalf("expected provider_error, got: %+v", result.Error)
	}
}

func TestMemoryExecutor_Search_LimitParsing(t *testing.T) {
	mp := &mockProvider{}
	ex := NewToolExecutor(mp)

	// float64（JSON number 反序列化后默认类型）
	result := ex.Execute(context.Background(), "memory_search", map[string]any{"query": "q", "limit": float64(3)}, newUserExecCtx(), "")
	if result.Error != nil {
		t.Fatalf("float64 limit failed: %v", result.Error.Message)
	}

	// int
	result = ex.Execute(context.Background(), "memory_search", map[string]any{"query": "q", "limit": 5}, newUserExecCtx(), "")
	if result.Error != nil {
		t.Fatalf("int limit failed: %v", result.Error.Message)
	}

	// json.Number
	result = ex.Execute(context.Background(), "memory_search", map[string]any{"query": "q", "limit": json.Number("7")}, newUserExecCtx(), "")
	if result.Error != nil {
		t.Fatalf("json.Number limit failed: %v", result.Error.Message)
	}
}

// --- read ---

func TestMemoryExecutor_Read_Success(t *testing.T) {
	mp := &mockProvider{contentText: "user prefers Go"}
	ex := NewToolExecutor(mp)
	result := ex.Execute(context.Background(), "memory_read", map[string]any{"uri": "viking://user/memories/preferences/lang"}, newUserExecCtx(), "")

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	if result.ResultJSON["content"] != "user prefers Go" {
		t.Fatalf("unexpected content: %v", result.ResultJSON["content"])
	}
}

func TestMemoryExecutor_Read_MissingURI(t *testing.T) {
	ex := NewToolExecutor(&mockProvider{})
	result := ex.Execute(context.Background(), "memory_read", map[string]any{}, newUserExecCtx(), "")
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid, got: %+v", result.Error)
	}
}

func TestMemoryExecutor_Read_FullDepth(t *testing.T) {
	mp := &mockProvider{contentText: "full content"}
	ex := NewToolExecutor(mp)

	calledWithRead := false
	origProvider := mp
	_ = origProvider

	result := ex.Execute(context.Background(), "memory_read",
		map[string]any{"uri": "viking://user/memories/profile/name", "depth": "full"},
		newUserExecCtx(), "")

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	// 验证 content 被正确返回（MemoryLayerRead 路径）
	if result.ResultJSON["content"] != "full content" {
		t.Fatalf("unexpected content: %v", result.ResultJSON["content"])
	}
	_ = calledWithRead
}

// --- write ---

func TestMemoryExecutor_Write_Success(t *testing.T) {
	mp := &mockProvider{}
	ex := NewToolExecutor(mp)
	result := ex.Execute(context.Background(), "memory_write", map[string]any{
		"category": "preferences",
		"key":      "language",
		"content":  "user prefers Go",
	}, newUserExecCtx(), "")

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	if result.ResultJSON["status"] != "ok" {
		t.Fatalf("unexpected status: %v", result.ResultJSON["status"])
	}
	if result.ResultJSON["key"] != "language" {
		t.Fatalf("unexpected key: %v", result.ResultJSON["key"])
	}
	uri, _ := result.ResultJSON["uri"].(string)
	if uri == "" {
		t.Fatal("expected non-empty uri in result")
	}
	if !mp.writeCalled {
		t.Fatal("expected Write to be called on provider")
	}
}

func TestMemoryExecutor_Write_MissingCategory(t *testing.T) {
	ex := NewToolExecutor(&mockProvider{})
	result := ex.Execute(context.Background(), "memory_write", map[string]any{
		"key": "lang", "content": "go",
	}, newUserExecCtx(), "")
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid, got: %+v", result.Error)
	}
}

func TestMemoryExecutor_Write_MissingKey(t *testing.T) {
	ex := NewToolExecutor(&mockProvider{})
	result := ex.Execute(context.Background(), "memory_write", map[string]any{
		"category": "preferences", "content": "go",
	}, newUserExecCtx(), "")
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid, got: %+v", result.Error)
	}
}

func TestMemoryExecutor_Write_AgentScope(t *testing.T) {
	mp := &mockProvider{}
	ex := NewToolExecutor(mp)
	result := ex.Execute(context.Background(), "memory_write", map[string]any{
		"category": "patterns",
		"key":      "retry",
		"content":  "retry on timeout",
		"scope":    "agent",
	}, newUserExecCtx(), "")

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	uri, _ := result.ResultJSON["uri"].(string)
	if uri == "" || uri[:len("viking://agent")] != "viking://agent" {
		t.Fatalf("expected agent scope uri, got: %q", uri)
	}
}

// --- forget ---

func TestMemoryExecutor_Forget_Success(t *testing.T) {
	mp := &mockProvider{}
	ex := NewToolExecutor(mp)
	targetURI := "viking://user/memories/preferences/lang"
	result := ex.Execute(context.Background(), "memory_forget", map[string]any{"uri": targetURI}, newUserExecCtx(), "")

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	if result.ResultJSON["status"] != "ok" {
		t.Fatalf("unexpected status: %v", result.ResultJSON["status"])
	}
	if !mp.deleteCalled {
		t.Fatal("expected Delete to be called on provider")
	}
	if mp.lastDeleteURI != targetURI {
		t.Fatalf("unexpected delete uri: %q", mp.lastDeleteURI)
	}
}

func TestMemoryExecutor_Forget_MissingURI(t *testing.T) {
	ex := NewToolExecutor(&mockProvider{})
	result := ex.Execute(context.Background(), "memory_forget", map[string]any{}, newUserExecCtx(), "")
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid, got: %+v", result.Error)
	}
}

// --- identity missing ---

func TestMemoryExecutor_NoUserID_IdentityMissing(t *testing.T) {
	ex := NewToolExecutor(&mockProvider{})
	result := ex.Execute(context.Background(), "memory_search", map[string]any{"query": "test"}, newExecCtx(nil), "")
	if result.Error == nil || result.Error.ErrorClass != errorIdentityMissing {
		t.Fatalf("expected identity_missing, got: %+v", result.Error)
	}
}
