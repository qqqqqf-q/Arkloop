//go:build !desktop

package memory

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"arkloop/services/worker/internal/events"
	workermemory "arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/memory/nowledge"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// --- mock ---

var testPool = new(pgxpool.Pool)

type mockProvider struct {
	findHits    []workermemory.MemoryHit
	findErr     error
	contentText string
	contentErr  error
	detail      nowledge.MemoryDetail
	detailErr   error
	snippet     nowledge.MemorySnippet
	snippetErr  error
	wm          nowledge.WorkingMemory
	wmErr       error
	status      nowledge.Status
	statusErr   error
	connections []nowledge.GraphConnection
	connErr     error
	timeline    []nowledge.TimelineEvent
	timelineErr error
	writeErr    error
	updateErr   error
	deleteErr   error

	findCalled     bool
	contentCalled  bool
	writeCalled    bool
	updateCalled   bool
	deleteCalled   bool
	lastWriteEntry workermemory.MemoryEntry
	lastDeleteURI  string
}

type richMockProvider struct {
	mockProvider
	richResults []nowledge.SearchResult
}

type desktopMockProvider struct {
	mockProvider
	writeURI    string
	snapshot    string
	updateErr   error
	writeURIErr error
}

func (m *desktopMockProvider) WriteReturningURI(_ context.Context, _ workermemory.MemoryIdentity, _ workermemory.MemoryScope, entry workermemory.MemoryEntry) (string, error) {
	m.writeCalled = true
	m.lastWriteEntry = entry
	if m.writeURIErr != nil {
		return "", m.writeURIErr
	}
	if strings.TrimSpace(m.writeURI) == "" {
		return "local://memory/test-id", nil
	}
	return m.writeURI, nil
}

func (m *desktopMockProvider) UpdateByURI(_ context.Context, _ workermemory.MemoryIdentity, uri string, entry workermemory.MemoryEntry) error {
	m.lastDeleteURI = uri
	m.lastWriteEntry = entry
	if m.updateErr != nil {
		return m.updateErr
	}
	return nil
}

func (m *desktopMockProvider) GetSnapshot(_ context.Context, _, _ uuid.UUID, _ string) (string, error) {
	return m.snapshot, nil
}

type noDesktopEditProvider struct{}

func (noDesktopEditProvider) Find(_ context.Context, _ workermemory.MemoryIdentity, _ string, _ string, _ int) ([]workermemory.MemoryHit, error) {
	return nil, nil
}

func (noDesktopEditProvider) Content(_ context.Context, _ workermemory.MemoryIdentity, _ string, _ workermemory.MemoryLayer) (string, error) {
	return "", nil
}

func (noDesktopEditProvider) AppendSessionMessages(_ context.Context, _ workermemory.MemoryIdentity, _ string, _ []workermemory.MemoryMessage) error {
	return nil
}

func (noDesktopEditProvider) CommitSession(_ context.Context, _ workermemory.MemoryIdentity, _ string) error {
	return nil
}

func (noDesktopEditProvider) Write(_ context.Context, _ workermemory.MemoryIdentity, _ workermemory.MemoryScope, _ workermemory.MemoryEntry) error {
	return nil
}

func (noDesktopEditProvider) Delete(_ context.Context, _ workermemory.MemoryIdentity, _ string) error {
	return nil
}

func (noDesktopEditProvider) ListDir(_ context.Context, _ workermemory.MemoryIdentity, _ string) ([]string, error) {
	return nil, nil
}

func (m *mockProvider) Find(_ context.Context, _ workermemory.MemoryIdentity, _ string, _ string, _ int) ([]workermemory.MemoryHit, error) {
	m.findCalled = true
	return m.findHits, m.findErr
}

func (m *mockProvider) Content(_ context.Context, _ workermemory.MemoryIdentity, _ string, _ workermemory.MemoryLayer) (string, error) {
	m.contentCalled = true
	return m.contentText, m.contentErr
}

func (m *mockProvider) MemoryDetail(_ context.Context, _ workermemory.MemoryIdentity, _ string) (nowledge.MemoryDetail, error) {
	return m.detail, m.detailErr
}

func (m *mockProvider) MemorySnippet(_ context.Context, _ workermemory.MemoryIdentity, _ string, _ int, _ int) (nowledge.MemorySnippet, error) {
	return m.snippet, m.snippetErr
}

func (m *mockProvider) ReadWorkingMemory(_ context.Context, _ workermemory.MemoryIdentity) (nowledge.WorkingMemory, error) {
	return m.wm, m.wmErr
}

func (m *mockProvider) PatchWorkingMemory(_ context.Context, _ workermemory.MemoryIdentity, _ string, patch nowledge.WorkingMemoryPatch) (nowledge.WorkingMemory, error) {
	if m.wmErr != nil {
		return nowledge.WorkingMemory{}, m.wmErr
	}
	updated := m.wm
	if patch.Content != nil {
		updated.Content = *patch.Content
	}
	if patch.Append != nil {
		switch {
		case strings.TrimSpace(updated.Content) == "":
			updated.Content = *patch.Append
		case strings.TrimSpace(*patch.Append) != "":
			updated.Content = updated.Content + "\n" + *patch.Append
		}
	}
	updated.Available = strings.TrimSpace(updated.Content) != ""
	m.wm = updated
	return updated, nil
}

func (m *mockProvider) Status(_ context.Context, _ workermemory.MemoryIdentity) (nowledge.Status, error) {
	return m.status, m.statusErr
}

func (m *mockProvider) Connections(_ context.Context, _ workermemory.MemoryIdentity, _ string, _ int, _ int) ([]nowledge.GraphConnection, error) {
	return m.connections, m.connErr
}

func (m *mockProvider) Timeline(_ context.Context, _ workermemory.MemoryIdentity, _ int, _ string, _ string, _ string, _ bool, _ int) ([]nowledge.TimelineEvent, error) {
	return m.timeline, m.timelineErr
}

func (m *richMockProvider) SearchRich(_ context.Context, _ workermemory.MemoryIdentity, _ string, _ int) ([]nowledge.SearchResult, error) {
	return m.richResults, m.findErr
}

func (m *richMockProvider) SearchThreads(_ context.Context, _ workermemory.MemoryIdentity, _ string, _ int) (map[string]any, error) {
	return map[string]any{"threads": []map[string]any{}, "total_found": 0}, nil
}

func (m *richMockProvider) SearchThreadsFull(_ context.Context, _ workermemory.MemoryIdentity, _ string, _ int, source string) (map[string]any, error) {
	if strings.TrimSpace(source) == "" {
		return map[string]any{"threads": []map[string]any{}, "total_found": 0}, nil
	}
	return map[string]any{
		"threads": []map[string]any{{
			"thread_id": "thread-1",
			"source":    source,
		}},
		"total_found": 1,
	}, nil
}

func (m *richMockProvider) FetchThread(_ context.Context, _ workermemory.MemoryIdentity, _ string, _ int, _ int) (map[string]any, error) {
	return map[string]any{"messages": []map[string]any{}}, nil
}

func (m *mockProvider) AppendSessionMessages(_ context.Context, _ workermemory.MemoryIdentity, _ string, _ []workermemory.MemoryMessage) error {
	return nil
}

func (m *mockProvider) CommitSession(_ context.Context, _ workermemory.MemoryIdentity, _ string) error {
	return nil
}

func (m *mockProvider) Write(_ context.Context, _ workermemory.MemoryIdentity, _ workermemory.MemoryScope, entry workermemory.MemoryEntry) error {
	m.writeCalled = true
	m.lastWriteEntry = entry
	return m.writeErr
}

func (m *mockProvider) Delete(_ context.Context, _ workermemory.MemoryIdentity, uri string) error {
	m.deleteCalled = true
	m.lastDeleteURI = uri
	return m.deleteErr
}

func (m *mockProvider) ListDir(_ context.Context, _ workermemory.MemoryIdentity, _ string) ([]string, error) {
	return nil, nil
}

func (m *mockProvider) UpdateByURI(_ context.Context, _ workermemory.MemoryIdentity, uri string, entry workermemory.MemoryEntry) error {
	m.updateCalled = true
	m.lastDeleteURI = uri
	m.lastWriteEntry = entry
	return m.updateErr
}

type snapshotMock struct {
	appendErr   error
	invalidErr  error
	called      bool
	invalidated bool
	lines       []string
}

func (s *snapshotMock) AppendMemoryLine(_ context.Context, _ *pgxpool.Pool, _ uuid.UUID, _ uuid.UUID, _ string, line string) error {
	s.called = true
	if s.appendErr != nil {
		return s.appendErr
	}
	s.lines = append(s.lines, line)
	return nil
}

func (s *snapshotMock) Invalidate(_ context.Context, _ *pgxpool.Pool, _ uuid.UUID, _ uuid.UUID, _ string) error {
	s.invalidated = true
	if s.invalidErr != nil {
		return s.invalidErr
	}
	return nil
}

// --- helpers ---

func newExecCtx(userID *uuid.UUID) tools.ExecutionContext {
	accountID := uuid.New()
	return tools.ExecutionContext{
		RunID:               uuid.New(),
		TraceID:             "test-trace",
		AccountID:           &accountID,
		UserID:              userID,
		AgentID:             "test-agent",
		Emitter:             events.NewEmitter("test-trace"),
		PendingMemoryWrites: workermemory.NewPendingWriteBuffer(),
	}
}

func newUserExecCtx() tools.ExecutionContext {
	uid := uuid.New()
	return newExecCtx(&uid)
}

func boolPtr(v bool) *bool { return &v }

// --- search ---

func TestMemoryExecutor_Search_Success(t *testing.T) {
	mp := &mockProvider{
		findHits: []workermemory.MemoryHit{
			{URI: "viking://user/memories/preferences/lang", Abstract: "user speaks English", Score: 0.9},
		},
	}
	ex := NewToolExecutor(mp, nil, nil)
	result := ex.Execute(context.Background(), "memory_search", map[string]any{"query": "language"}, newUserExecCtx(), "")

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	hits, _ := result.ResultJSON["hits"].([]map[string]any)
	if len(hits) == 0 {
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
	ex := NewToolExecutor(&mockProvider{}, nil, nil)
	result := ex.Execute(context.Background(), "memory_search", map[string]any{"query": ""}, newUserExecCtx(), "")
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid, got: %+v", result.Error)
	}
}

func TestMemoryExecutor_Search_ProviderError(t *testing.T) {
	mp := &mockProvider{findErr: errors.New("connection refused")}
	ex := NewToolExecutor(mp, nil, nil)
	result := ex.Execute(context.Background(), "memory_search", map[string]any{"query": "test"}, newUserExecCtx(), "")
	if result.Error == nil || result.Error.ErrorClass != errorProviderFailure {
		t.Fatalf("expected provider_error, got: %+v", result.Error)
	}
}

func TestMemoryExecutor_Search_LimitParsing(t *testing.T) {
	mp := &mockProvider{}
	ex := NewToolExecutor(mp, nil, nil)

	result := ex.Execute(context.Background(), "memory_search", map[string]any{"query": "q", "limit": float64(3)}, newUserExecCtx(), "")
	if result.Error != nil {
		t.Fatalf("float64 limit failed: %v", result.Error.Message)
	}

	result = ex.Execute(context.Background(), "memory_search", map[string]any{"query": "q", "limit": 5}, newUserExecCtx(), "")
	if result.Error != nil {
		t.Fatalf("int limit failed: %v", result.Error.Message)
	}

	result = ex.Execute(context.Background(), "memory_search", map[string]any{"query": "q", "limit": json.Number("7")}, newUserExecCtx(), "")
	if result.Error != nil {
		t.Fatalf("json.Number limit failed: %v", result.Error.Message)
	}
}

func TestMemoryExecutor_Search_EnrichesNowledgeResults(t *testing.T) {
	mp := &richMockProvider{
		richResults: []nowledge.SearchResult{
			{
				Kind:            "memory",
				ID:              "mem-1",
				Title:           "Deploy decision",
				Content:         "Use SeaweedFS",
				RelevanceReason: "keyword + bm25",
				Importance:      0.8,
				Labels:          []string{"decision"},
				SourceThreadID:  "thread-1",
				RelatedThreads: []nowledge.ThreadSearchResult{
					{ThreadID: "thread-1", Title: "Deploy chat", Source: "arkloop", MessageCount: 3, Score: 0.7, Snippets: []string{"Use SeaweedFS"}},
				},
			},
		},
	}
	ex := NewToolExecutor(mp, nil, nil)
	result := ex.Execute(context.Background(), "memory_search", map[string]any{"query": "deploy"}, newUserExecCtx(), "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	hits, _ := result.ResultJSON["hits"].([]map[string]any)
	if len(hits) == 0 {
		raw, _ := json.Marshal(result.ResultJSON["hits"])
		var arr []map[string]any
		_ = json.Unmarshal(raw, &arr)
		hits = arr
	}
	if len(hits) != 1 {
		t.Fatalf("unexpected hits: %#v", result.ResultJSON)
	}
	if hits[0]["source_thread_id"] != "thread-1" {
		t.Fatalf("missing source_thread_id: %#v", hits[0])
	}
	if hits[0]["matched_via"] != "keyword + bm25" {
		t.Fatalf("missing matched_via: %#v", hits[0])
	}
	related, ok := hits[0]["related_threads"].([]map[string]any)
	if !ok || len(related) != 1 {
		raw, _ := json.Marshal(hits[0]["related_threads"])
		var arr []map[string]any
		_ = json.Unmarshal(raw, &arr)
		related = arr
	}
	if len(related) != 1 || related[0]["thread_id"] != "thread-1" {
		t.Fatalf("unexpected related threads: %#v", hits[0]["related_threads"])
	}
	if hits[0]["kind"] != "memory" {
		t.Fatalf("unexpected kind: %#v", hits[0])
	}
}

func TestMemoryExecutor_Search_ReturnsUnifiedThreadHits(t *testing.T) {
	mp := &richMockProvider{
		richResults: []nowledge.SearchResult{
			{
				Kind:           "thread",
				ID:             "thread-9",
				ThreadID:       "thread-9",
				Title:          "Architecture chat",
				Content:        "统一 Memory 后端",
				Score:          0.73,
				MatchedSnippet: "统一 Memory 后端",
				Snippets:       []string{"统一 Memory 后端", "修掉双 run"},
			},
		},
	}
	ex := NewToolExecutor(mp, nil, nil)
	result := ex.Execute(context.Background(), "memory_search", map[string]any{"query": "memory"}, newUserExecCtx(), "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	hits, _ := result.ResultJSON["hits"].([]map[string]any)
	if len(hits) == 0 {
		raw, _ := json.Marshal(result.ResultJSON["hits"])
		var arr []map[string]any
		_ = json.Unmarshal(raw, &arr)
		hits = arr
	}
	if len(hits) != 1 {
		t.Fatalf("unexpected hits: %#v", result.ResultJSON)
	}
	if hits[0]["kind"] != "thread" {
		t.Fatalf("unexpected kind: %#v", hits[0])
	}
	if hits[0]["uri"] != "nowledge://thread/thread-9" {
		t.Fatalf("unexpected thread uri: %#v", hits[0])
	}
	if hits[0]["thread_id"] != "thread-9" {
		t.Fatalf("unexpected thread id: %#v", hits[0])
	}
	if hits[0]["matched_snippet"] != "统一 Memory 后端" {
		t.Fatalf("unexpected matched snippet: %#v", hits[0])
	}
}

// --- read ---

func TestMemoryExecutor_Read_Success(t *testing.T) {
	mp := &mockProvider{contentText: "user prefers Go"}
	ex := NewToolExecutor(mp, nil, nil)
	result := ex.Execute(context.Background(), "memory_read", map[string]any{"uri": "viking://user/memories/preferences/lang"}, newUserExecCtx(), "")

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	if result.ResultJSON["content"] != "user prefers Go" {
		t.Fatalf("unexpected content: %v", result.ResultJSON["content"])
	}
}

func TestMemoryExecutor_Read_MissingURI(t *testing.T) {
	ex := NewToolExecutor(&mockProvider{}, nil, nil)
	result := ex.Execute(context.Background(), "memory_read", map[string]any{}, newUserExecCtx(), "")
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid, got: %+v", result.Error)
	}
}

func TestMemoryExecutor_Read_FullDepth(t *testing.T) {
	mp := &mockProvider{contentText: "full content"}
	ex := NewToolExecutor(mp, nil, nil)
	result := ex.Execute(context.Background(), "memory_read",
		map[string]any{"uri": "viking://user/memories/profile/name", "depth": "full"},
		newUserExecCtx(), "")

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	if result.ResultJSON["content"] != "full content" {
		t.Fatalf("unexpected content: %v", result.ResultJSON["content"])
	}
}

func TestMemoryExecutor_Read_NowledgeWorkingMemoryAlias(t *testing.T) {
	mp := &mockProvider{wm: nowledge.WorkingMemory{Content: "today focus", Available: true}}
	ex := NewToolExecutor(mp, nil, nil)
	result := ex.Execute(context.Background(), "memory_read", map[string]any{"uri": "MEMORY.md"}, newUserExecCtx(), "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	if result.ResultJSON["content"] != "today focus" {
		t.Fatalf("unexpected content: %#v", result.ResultJSON)
	}
	if result.ResultJSON["source"] != "working_memory" {
		t.Fatalf("unexpected source: %#v", result.ResultJSON)
	}
}

func TestMemoryExecutor_Read_NowledgeDetailIncludesProvenance(t *testing.T) {
	mp := &mockProvider{detail: nowledge.MemoryDetail{Title: "Decision", Content: "Use SeaweedFS", SourceThreadID: "thread-1"}}
	ex := NewToolExecutor(mp, nil, nil)
	result := ex.Execute(context.Background(), "memory_read", map[string]any{"uri": "nowledge://memory/mem-1", "depth": "full"}, newUserExecCtx(), "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	if result.ResultJSON["source_thread_id"] != "thread-1" {
		t.Fatalf("unexpected provenance: %#v", result.ResultJSON)
	}
	if !strings.Contains(result.ResultJSON["content"].(string), "SeaweedFS") {
		t.Fatalf("unexpected content: %#v", result.ResultJSON)
	}
}

func TestMemoryExecutor_Read_NowledgeSnippetRange(t *testing.T) {
	mp := &mockProvider{
		snippet: nowledge.MemorySnippet{
			MemoryDetail: nowledge.MemoryDetail{Title: "Decision", SourceThreadID: "thread-1"},
			Text:         "line2\nline3",
			StartLine:    2,
			EndLine:      3,
			TotalLines:   4,
		},
	}
	ex := NewToolExecutor(mp, nil, nil)
	result := ex.Execute(context.Background(), "memory_read", map[string]any{
		"uri":   "nowledge://memory/mem-1",
		"from":  float64(2),
		"lines": float64(2),
	}, newUserExecCtx(), "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	if result.ResultJSON["content"] != "line2\nline3" || result.ResultJSON["start_line"] != 2 || result.ResultJSON["end_line"] != 3 {
		t.Fatalf("unexpected snippet result: %#v", result.ResultJSON)
	}
	if result.ResultJSON["source_thread_id"] != "thread-1" {
		t.Fatalf("unexpected provenance: %#v", result.ResultJSON)
	}
}

func TestMemoryExecutor_Read_NowledgeThreadURI(t *testing.T) {
	mp := &richMockProvider{}
	ex := NewToolExecutor(mp, nil, nil)
	result := ex.Execute(context.Background(), "memory_read", map[string]any{
		"uri":   "nowledge://thread/thread-9",
		"depth": "full",
	}, newUserExecCtx(), "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	if result.ResultJSON["thread_id"] != "thread-9" {
		t.Fatalf("unexpected thread id: %#v", result.ResultJSON)
	}
	if result.ResultJSON["source"] != "thread" {
		t.Fatalf("unexpected source: %#v", result.ResultJSON)
	}
}

func TestMemoryExecutor_Read_NowledgeOverviewKeepsDepthContract(t *testing.T) {
	mp := &mockProvider{detail: nowledge.MemoryDetail{
		Title:   "Decision",
		Content: "Use SeaweedFS as the only attachment backend for now and do not integrate MinIO in this phase because the rollout should stay simple.",
	}}
	ex := NewToolExecutor(mp, nil, nil)
	result := ex.Execute(context.Background(), "memory_read", map[string]any{
		"uri":   "nowledge://memory/mem-1",
		"depth": "overview",
	}, newUserExecCtx(), "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	content, _ := result.ResultJSON["content"].(string)
	if !strings.Contains(content, "Decision") {
		t.Fatalf("unexpected content: %#v", result.ResultJSON)
	}
	if strings.Contains(content, "do not integrate MinIO in this phase") {
		t.Fatalf("overview should not return full content: %#v", result.ResultJSON)
	}
}

func TestMemoryExecutor_Context_ReadWorkingMemory(t *testing.T) {
	mp := &mockProvider{wm: nowledge.WorkingMemory{Content: "## Focus\n\nShip nowledge", Available: true}}
	ex := NewToolExecutor(mp, nil, nil)
	result := ex.Execute(context.Background(), "memory_context", map[string]any{}, newUserExecCtx(), "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	if result.ResultJSON["source"] != "working_memory" || result.ResultJSON["available"] != true {
		t.Fatalf("unexpected context payload: %#v", result.ResultJSON)
	}
}

func TestMemoryExecutor_Context_PatchAppend(t *testing.T) {
	mp := &mockProvider{wm: nowledge.WorkingMemory{Content: "## Notes\n\nBefore", Available: true}}
	ex := NewToolExecutor(mp, nil, nil)
	result := ex.Execute(context.Background(), "memory_context", map[string]any{
		"patch_section": "## Notes",
		"patch_append":  "After",
	}, newUserExecCtx(), "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	if result.ResultJSON["action"] != "append" {
		t.Fatalf("unexpected action: %#v", result.ResultJSON)
	}
	if !strings.Contains(result.ResultJSON["content"].(string), "After") {
		t.Fatalf("unexpected content: %#v", result.ResultJSON)
	}
}

func TestMemoryExecutor_Status(t *testing.T) {
	mp := &mockProvider{status: nowledge.Status{
		Mode:                   "local",
		BaseURL:                "http://127.0.0.1:14242",
		APIKeyConfigured:       false,
		Healthy:                true,
		Version:                "0.4.1",
		WorkingMemoryAvailable: boolPtr(true),
	}}
	ex := NewToolExecutor(mp, nil, nil)
	result := ex.Execute(context.Background(), "memory_status", map[string]any{}, newUserExecCtx(), "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	if result.ResultJSON["provider"] != "nowledge" || result.ResultJSON["healthy"] != true {
		t.Fatalf("unexpected status: %#v", result.ResultJSON)
	}
}

func TestMemoryExecutor_Connections_ByMemoryID(t *testing.T) {
	mp := &mockProvider{
		connections: []nowledge.GraphConnection{
			{MemoryID: "mem-1", NodeID: "node-1", NodeType: "memory", Title: "Deploy note", Snippet: "Use SeaweedFS", EdgeType: "EVOLVES", Relation: "enriches", Weight: 0.8},
		},
	}
	ex := NewToolExecutor(mp, nil, nil)
	result := ex.Execute(context.Background(), "memory_connections", map[string]any{"memory_id": "mem-1"}, newUserExecCtx(), "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	if result.ResultJSON["total_found"] != 1 {
		t.Fatalf("unexpected result: %#v", result.ResultJSON)
	}
	connections, _ := result.ResultJSON["connections"].([]map[string]any)
	if len(connections) == 0 {
		raw, _ := json.Marshal(result.ResultJSON["connections"])
		var arr []map[string]any
		_ = json.Unmarshal(raw, &arr)
		connections = arr
	}
	if len(connections) != 1 || connections[0]["node_id"] != "node-1" {
		t.Fatalf("unexpected connections: %#v", result.ResultJSON)
	}
}

func TestMemoryExecutor_Connections_ByQuery(t *testing.T) {
	mp := &richMockProvider{
		mockProvider: mockProvider{
			connections: []nowledge.GraphConnection{
				{MemoryID: "mem-1", NodeID: "node-2", NodeType: "entity", Title: "SeaweedFS", EdgeType: "MENTIONS"},
			},
		},
		richResults: []nowledge.SearchResult{{ID: "mem-1", Title: "Decision"}},
	}
	ex := NewToolExecutor(mp, nil, nil)
	result := ex.Execute(context.Background(), "memory_connections", map[string]any{"query": "deploy"}, newUserExecCtx(), "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	if result.ResultJSON["memory_id"] != "mem-1" {
		t.Fatalf("unexpected memory_id: %#v", result.ResultJSON)
	}
}

func TestMemoryExecutor_Timeline(t *testing.T) {
	mp := &mockProvider{
		timeline: []nowledge.TimelineEvent{
			{ID: "evt-1", EventType: "memory_created", Label: "Memory saved", Title: "Yesterday", CreatedAt: "2026-04-11T10:00:00Z", MemoryID: "mem-1"},
			{ID: "evt-2", EventType: "insight_generated", Label: "Insight", Title: "Today", CreatedAt: "2026-04-12T11:00:00Z", MemoryID: "mem-2"},
		},
	}
	ex := NewToolExecutor(mp, nil, nil)
	result := ex.Execute(context.Background(), "memory_timeline", map[string]any{"last_n_days": float64(3)}, newUserExecCtx(), "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	if result.ResultJSON["total_found"] != 2 {
		t.Fatalf("unexpected result: %#v", result.ResultJSON)
	}
	days, _ := result.ResultJSON["days"].([]map[string]any)
	if len(days) == 0 {
		raw, _ := json.Marshal(result.ResultJSON["days"])
		var arr []map[string]any
		_ = json.Unmarshal(raw, &arr)
		days = arr
	}
	if len(days) != 2 || days[0]["date"] != "2026-04-12" || days[1]["date"] != "2026-04-11" {
		t.Fatalf("unexpected grouped days: %#v", result.ResultJSON)
	}
}

func TestMemoryExecutor_ThreadSearchWithSourceFilter(t *testing.T) {
	mp := &richMockProvider{}
	ex := NewToolExecutor(mp, nil, nil)
	result := ex.Execute(context.Background(), "memory_thread_search", map[string]any{
		"query":  "deploy",
		"source": "arkloop",
	}, newUserExecCtx(), "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	if result.ResultJSON["total_found"] != 1 {
		t.Fatalf("unexpected result: %#v", result.ResultJSON)
	}
}

// --- write ---

func TestMemoryExecutor_Write_Success(t *testing.T) {
	mp := &mockProvider{}
	snapshots := &snapshotMock{}
	execCtx := newUserExecCtx()
	ex := NewToolExecutor(mp, testPool, snapshots)
	result := ex.Execute(context.Background(), "memory_write", map[string]any{
		"category": "preferences",
		"key":      "language",
		"content":  "user prefers Go",
	}, execCtx, "")

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	if result.ResultJSON["status"] != "queued" {
		t.Fatalf("unexpected status: %v", result.ResultJSON["status"])
	}
	taskID, _ := result.ResultJSON["task_id"].(string)
	if strings.TrimSpace(taskID) == "" {
		t.Fatalf("expected non-empty task_id, got: %v", result.ResultJSON["task_id"])
	}
	if result.ResultJSON["snapshot_updated"] != false {
		t.Fatalf("expected snapshot_updated=false, got: %v", result.ResultJSON["snapshot_updated"])
	}
	if snapshots.called {
		t.Fatal("snapshot should not be updated before provider.Write succeeds")
	}
	if mp.writeCalled {
		t.Fatal("provider.Write should not be called synchronously")
	}
	if execCtx.PendingMemoryWrites.Len() != 1 {
		t.Fatalf("expected 1 pending write, got %d", execCtx.PendingMemoryWrites.Len())
	}
}

func TestMemoryExecutor_Write_SnapshotFailure(t *testing.T) {
	mp := &mockProvider{}
	snapshots := &snapshotMock{appendErr: errors.New("db down")}
	execCtx := newUserExecCtx()
	ex := NewToolExecutor(mp, testPool, snapshots)
	result := ex.Execute(context.Background(), "memory_write", map[string]any{
		"category": "preferences",
		"key":      "language",
		"content":  "user prefers Go",
	}, execCtx, "")

	if result.Error != nil {
		t.Fatalf("did not expect snapshot error during queued write: %+v", result.Error)
	}
	if execCtx.PendingMemoryWrites.Len() != 1 {
		t.Fatalf("expected queued pending write, got %d", execCtx.PendingMemoryWrites.Len())
	}
}

func TestMemoryExecutor_Write_MissingCategory(t *testing.T) {
	execCtx := newUserExecCtx()
	ex := NewToolExecutor(&mockProvider{}, testPool, &snapshotMock{})
	result := ex.Execute(context.Background(), "memory_write", map[string]any{
		"key": "lang", "content": "go",
	}, execCtx, "")
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid, got: %+v", result.Error)
	}
}

func TestMemoryExecutor_Write_MissingKey(t *testing.T) {
	execCtx := newUserExecCtx()
	ex := NewToolExecutor(&mockProvider{}, testPool, &snapshotMock{})
	result := ex.Execute(context.Background(), "memory_write", map[string]any{
		"category": "preferences", "content": "go",
	}, execCtx, "")
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid, got: %+v", result.Error)
	}
}

func TestMemoryExecutor_Write_AgentScope(t *testing.T) {
	mp := &mockProvider{}
	snapshots := &snapshotMock{}
	execCtx := newUserExecCtx()
	ex := NewToolExecutor(mp, testPool, snapshots)
	result := ex.Execute(context.Background(), "memory_write", map[string]any{
		"category": "patterns",
		"key":      "retry",
		"content":  "retry on timeout",
		"scope":    "agent",
	}, execCtx, "")

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	if result.ResultJSON["status"] != "queued" {
		t.Fatalf("unexpected status: %v", result.ResultJSON["status"])
	}
	taskID, _ := result.ResultJSON["task_id"].(string)
	if strings.TrimSpace(taskID) == "" {
		t.Fatalf("expected non-empty task_id, got: %v", result.ResultJSON["task_id"])
	}
	if result.ResultJSON["snapshot_updated"] != false {
		t.Fatalf("expected snapshot_updated=false, got: %v", result.ResultJSON["snapshot_updated"])
	}
	if len(snapshots.lines) != 0 {
		t.Fatalf("expected no optimistic snapshot lines, got: %v", snapshots.lines)
	}
	pending := execCtx.PendingMemoryWrites.Drain()
	if len(pending) != 1 || pending[0].Scope != workermemory.MemoryScopeUser {
		t.Fatalf("expected pending write scope=user after normalization, got: %+v", pending)
	}
}

func TestMemoryExecutor_Write_MissingPendingBuffer(t *testing.T) {
	execCtx := newUserExecCtx()
	execCtx.PendingMemoryWrites = nil
	ex := NewToolExecutor(&mockProvider{}, testPool, &snapshotMock{})
	result := ex.Execute(context.Background(), "memory_write", map[string]any{
		"category": "preferences",
		"key":      "language",
		"content":  "user prefers Go",
	}, execCtx, "")
	if result.Error == nil || result.Error.ErrorClass != errorStateMissing {
		t.Fatalf("expected state_missing, got: %+v", result.Error)
	}
}

// --- forget ---

func TestMemoryExecutor_Forget_Success(t *testing.T) {
	mp := &mockProvider{}
	ex := NewToolExecutor(mp, nil, nil)
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
	ex := NewToolExecutor(&mockProvider{}, nil, nil)
	result := ex.Execute(context.Background(), "memory_forget", map[string]any{}, newUserExecCtx(), "")
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid, got: %+v", result.Error)
	}
}

func TestMemoryExecutor_Edit_Success(t *testing.T) {
	mp := &mockProvider{}
	ex := NewToolExecutor(mp, nil, nil)
	targetURI := "viking://user/memories/preferences/lang.md"
	result := ex.Execute(context.Background(), "memory_edit", map[string]any{
		"uri":     targetURI,
		"content": "updated memory body",
	}, newUserExecCtx(), "")

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	if result.ResultJSON["status"] != "ok" {
		t.Fatalf("unexpected status: %v", result.ResultJSON["status"])
	}
	if !mp.updateCalled {
		t.Fatal("expected UpdateByURI to be called on provider")
	}
	if mp.lastDeleteURI != targetURI {
		t.Fatalf("unexpected edit uri: %q", mp.lastDeleteURI)
	}
	if mp.lastWriteEntry.Content != "updated memory body" {
		t.Fatalf("unexpected edit content: %q", mp.lastWriteEntry.Content)
	}
}

func TestMemoryExecutor_Edit_MissingURI(t *testing.T) {
	ex := NewToolExecutor(&mockProvider{}, nil, nil)
	result := ex.Execute(context.Background(), "memory_edit", map[string]any{
		"content": "updated memory body",
	}, newUserExecCtx(), "")
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid, got: %+v", result.Error)
	}
}

func TestMemoryExecutor_Edit_MissingContent(t *testing.T) {
	ex := NewToolExecutor(&mockProvider{}, nil, nil)
	result := ex.Execute(context.Background(), "memory_edit", map[string]any{
		"uri": "viking://user/memories/preferences/lang.md",
	}, newUserExecCtx(), "")
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid, got: %+v", result.Error)
	}
}

// --- identity missing ---

func TestMemoryExecutor_NoUserID_IdentityMissing(t *testing.T) {
	ex := NewToolExecutor(&mockProvider{}, nil, nil)
	result := ex.Execute(context.Background(), "memory_search", map[string]any{"query": "test"}, newExecCtx(nil), "")
	if result.Error == nil || result.Error.ErrorClass != errorIdentityMissing {
		t.Fatalf("expected identity_missing, got: %+v", result.Error)
	}
}

func TestNotebookTools_NotAvailableOutsideDesktop(t *testing.T) {
	mp := noDesktopEditProvider{}
	ex := NewToolExecutor(mp, nil, nil)
	execCtx := newUserExecCtx()

	writeResult := ex.Execute(context.Background(), "notebook_write", map[string]any{
		"category": "preferences",
		"key":      "style",
		"content":  "concise",
	}, execCtx, "")
	if writeResult.Error == nil || writeResult.Error.ErrorClass != errorStateMissing {
		t.Fatalf("expected state_missing write error, got: %+v", writeResult.Error)
	}

	editResult := ex.Execute(context.Background(), "notebook_edit", map[string]any{
		"uri":      "local://memory/test-id",
		"category": "preferences",
		"key":      "style",
		"content":  "formal",
	}, execCtx, "")
	if editResult.Error == nil || editResult.Error.ErrorClass != errorStateMissing {
		t.Fatalf("expected state_missing edit error, got: %+v", editResult.Error)
	}
}
