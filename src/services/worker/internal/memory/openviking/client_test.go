package openviking

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"arkloop/services/worker/internal/memory"

	"github.com/google/uuid"
)

// --- test helpers ---

var (
	fixedAccountID = uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000001")
	fixedUserID    = uuid.MustParse("bbbbbbbb-0000-0000-0000-000000000002")
	fixedAgentID   = "test-agent"
)

func newIdent() memory.MemoryIdentity {
	return memory.MemoryIdentity{
		AccountID: fixedAccountID,
		UserID:    fixedUserID,
		AgentID:   fixedAgentID,
	}
}

// apiResp 快速构建标准 OpenViking JSON 响应。
func apiResp(result any) map[string]any {
	raw, _ := json.Marshal(result)
	return map[string]any{
		"status": "ok",
		"result": json.RawMessage(raw),
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// --- Find ---

func TestClient_Find_Success(t *testing.T) {
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/search/find" {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			writeJSON(w, apiResp(map[string]any{
				"memories": []map[string]any{
					{"uri": "viking://user/memories/prefs/lang", "abstract": "Go", "score": 0.95, "match_reason": "keyword", "relations": []any{}},
				},
				"resources": []any{},
				"skills":    []any{},
			}))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := newClient(srv.URL, "root-key")
	hits, err := c.Find(context.Background(), newIdent(), memory.MemoryScopeUser, "programming language", 5)
	if err != nil {
		t.Fatalf("Find failed: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	if hits[0].URI != "viking://user/memories/prefs/lang" {
		t.Fatalf("unexpected URI: %q", hits[0].URI)
	}

	// 验证 request body 中 target_uri 含 userID
	wantTargetURI := "viking://user/" + fixedUserID.String() + "/memories/"
	if gotBody["target_uri"] != wantTargetURI {
		t.Fatalf("unexpected target_uri: %v", gotBody["target_uri"])
	}
	if gotBody["query"] != "programming language" {
		t.Fatalf("unexpected query: %v", gotBody["query"])
	}
}

func TestClient_Find_AgentScope(t *testing.T) {
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		writeJSON(w, apiResp(map[string]any{
			"memories": []any{}, "resources": []any{}, "skills": []any{},
		}))
	}))
	defer srv.Close()

	c := newClient(srv.URL, "")
	_, err := c.Find(context.Background(), newIdent(), memory.MemoryScopeAgent, "retry patterns", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["target_uri"] != "viking://user/"+fixedUserID.String()+"/memories/" {
		t.Fatalf("unexpected target_uri for agent scope: %v", gotBody["target_uri"])
	}
}

func TestClient_Find_MergesMemoriesResourcesSkills(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, apiResp(map[string]any{
			"memories": []map[string]any{
				{"uri": "m1", "abstract": "mem", "score": 0.8, "match_reason": "", "relations": []any{}},
			},
			"resources": []map[string]any{
				{"uri": "r1", "abstract": "res", "score": 0.7, "match_reason": "", "relations": []any{}},
			},
			"skills": []map[string]any{
				{"uri": "s1", "abstract": "skill", "score": 0.6, "match_reason": "", "relations": []any{}},
			},
		}))
	}))
	defer srv.Close()

	c := newClient(srv.URL, "")
	hits, err := c.Find(context.Background(), newIdent(), memory.MemoryScopeUser, "q", 10)
	if err != nil {
		t.Fatalf("Find failed: %v", err)
	}
	if len(hits) != 3 {
		t.Fatalf("expected 3 hits (memories+resources+skills), got %d", len(hits))
	}
}

// --- Content ---

func TestClient_Content_StringResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, apiResp("user prefers Go"))
	}))
	defer srv.Close()

	c := newClient(srv.URL, "")
	content, err := c.Content(context.Background(), newIdent(), "viking://user/memories/prefs/lang", memory.MemoryLayerOverview)
	if err != nil {
		t.Fatalf("Content failed: %v", err)
	}
	if content != "user prefers Go" {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestClient_Content_ObjectResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, apiResp(map[string]any{"key": "value"}))
	}))
	defer srv.Close()

	c := newClient(srv.URL, "")
	content, err := c.Content(context.Background(), newIdent(), "viking://user/memories/prefs/lang", memory.MemoryLayerRead)
	if err != nil {
		t.Fatalf("Content failed: %v", err)
	}
	// fallback: 原始 JSON 字符串
	if !strings.Contains(content, "key") {
		t.Fatalf("expected raw JSON, got: %q", content)
	}
}

// --- AppendSessionMessages ---

func TestClient_AppendSessionMessages_SendsOneByOne(t *testing.T) {
	var callCount int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages") {
			atomic.AddInt32(&callCount, 1)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := newClient(srv.URL, "")
	msgs := []memory.MemoryMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
		{Role: "user", Content: "again"},
	}
	if err := c.AppendSessionMessages(context.Background(), newIdent(), "sess-123", msgs); err != nil {
		t.Fatalf("AppendSessionMessages failed: %v", err)
	}
	if atomic.LoadInt32(&callCount) != 3 {
		t.Fatalf("expected 3 individual requests, got %d", callCount)
	}
}

// --- Write ---

func TestClient_Write_FullFlow(t *testing.T) {
	var order []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions":
			order = append(order, "create_session")
			writeJSON(w, apiResp(map[string]string{"session_id": "sid-abc"}))

		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			order = append(order, "add_msg:"+body["role"])
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/commit"):
			order = append(order, "commit")
			w.WriteHeader(http.StatusOK)

		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := newClient(srv.URL, "")
	entry := memory.MemoryEntry{Content: "user likes Go"}
	if err := c.Write(context.Background(), newIdent(), memory.MemoryScopeUser, entry); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	want := []string{"create_session", "add_msg:user", "add_msg:assistant", "commit"}
	if len(order) != len(want) {
		t.Fatalf("unexpected call order: %v", order)
	}
	for i, v := range want {
		if order[i] != v {
			t.Errorf("order[%d]: got %q, want %q", i, order[i], v)
		}
	}
}

func TestClient_Write_EmptyContent_Error(t *testing.T) {
	requestMade := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestMade = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newClient(srv.URL, "")
	err := c.Write(context.Background(), newIdent(), memory.MemoryScopeUser, memory.MemoryEntry{Content: "   "})
	if err == nil {
		t.Fatal("expected error for empty content")
	}
	if requestMade {
		t.Fatal("no HTTP request should be made for empty content")
	}
}

// --- Delete ---

func TestClient_Delete_Correct(t *testing.T) {
	var gotURI string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/api/v1/fs" {
			gotURI = r.URL.Query().Get("uri")
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := newClient(srv.URL, "")
	targetURI := "viking://user/memories/prefs/lang"
	if err := c.Delete(context.Background(), newIdent(), targetURI); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if gotURI != targetURI {
		t.Fatalf("expected uri=%q, got %q", targetURI, gotURI)
	}
}

// --- Retry ---

func TestClient_Retry_5xx(t *testing.T) {
	var callCount int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"error"}`))
	}))
	defer srv.Close()

	c := newClient(srv.URL, "")
	// Find 使用 doJSONWithRetry，会重试 maxReadRetries 次
	_, err := c.Find(context.Background(), newIdent(), memory.MemoryScopeUser, "q", 5)
	if err == nil {
		t.Fatal("expected error after all retries")
	}
	expected := int32(maxReadRetries + 1)
	if atomic.LoadInt32(&callCount) != expected {
		t.Fatalf("expected %d calls (1 + %d retries), got %d", expected, maxReadRetries, callCount)
	}
}

func TestClient_Retry_4xx_NoRetry(t *testing.T) {
	var callCount int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":"error"}`))
	}))
	defer srv.Close()

	c := newClient(srv.URL, "")
	_, err := c.Find(context.Background(), newIdent(), memory.MemoryScopeUser, "q", 5)
	if err == nil {
		t.Fatal("expected error on 400")
	}
	// 4xx 不重试，应该只发一次请求
	if atomic.LoadInt32(&callCount) != 1 {
		t.Fatalf("expected 1 call for 4xx, got %d", callCount)
	}
}

func TestClient_Retry_CtxCancel_Stops(t *testing.T) {
	var callCount int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := &client{
		baseURL:    srv.URL,
		rootAPIKey: "",
		http:       &http.Client{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	_, err := c.Find(ctx, newIdent(), memory.MemoryScopeUser, "q", 5)
	if err == nil {
		t.Fatal("expected error when context cancelled")
	}
	// ctx 已取消，doJSONWithRetry 在第一次检查时就应返回
	if atomic.LoadInt32(&callCount) > 1 {
		t.Fatalf("expected at most 1 request after ctx cancel, got %d", callCount)
	}
}

// --- Identity Headers ---

func TestClient_IdentityHeaders(t *testing.T) {
	var gotHeaders http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		writeJSON(w, apiResp(map[string]any{
			"memories": []any{}, "resources": []any{}, "skills": []any{},
		}))
	}))
	defer srv.Close()

	ident := memory.MemoryIdentity{
		AccountID: uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000001"),
		UserID:    uuid.MustParse("bbbbbbbb-0000-0000-0000-000000000002"),
		AgentID:   "my-agent",
	}
	c := newClient(srv.URL, "root-key-123")
	_, err := c.Find(context.Background(), ident, memory.MemoryScopeUser, "q", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotHeaders.Get("X-API-Key") != "root-key-123" {
		t.Errorf("X-API-Key: got %q", gotHeaders.Get("X-API-Key"))
	}
	if gotHeaders.Get("X-OpenViking-Account") != ident.AccountID.String() {
		t.Errorf("X-OpenViking-Account: got %q", gotHeaders.Get("X-OpenViking-Account"))
	}
	if gotHeaders.Get("X-OpenViking-User") != ident.UserID.String() {
		t.Errorf("X-OpenViking-User: got %q", gotHeaders.Get("X-OpenViking-User"))
	}
	if gotHeaders.Get("X-OpenViking-Agent") != "my-agent" {
		t.Errorf("X-OpenViking-Agent: got %q", gotHeaders.Get("X-OpenViking-Agent"))
	}
}

func TestClient_AgentScope_Write_StillUsesUserIdentity(t *testing.T) {
	var sessionHeaders, msgHeaders http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions":
			sessionHeaders = r.Header.Clone()
			writeJSON(w, apiResp(map[string]string{"session_id": "sid-xyz"}))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			msgHeaders = r.Header.Clone()
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/commit"):
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ident := memory.MemoryIdentity{
		AccountID: uuid.New(),
		UserID:    uuid.New(),
		AgentID:   "agent-001",
	}
	c := newClient(srv.URL, "")
	err := c.Write(context.Background(), ident, memory.MemoryScopeAgent, memory.MemoryEntry{Content: "agent pattern"})
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if sessionHeaders.Get("X-OpenViking-User") != ident.UserID.String() {
		t.Errorf("expected X-OpenViking-User=%q for agent scope, got %q", ident.UserID.String(), sessionHeaders.Get("X-OpenViking-User"))
	}
	if msgHeaders != nil && msgHeaders.Get("X-OpenViking-User") != ident.UserID.String() {
		t.Errorf("msg X-OpenViking-User should keep user id for agent scope, got %q", msgHeaders.Get("X-OpenViking-User"))
	}
}

func TestNewClient_AllowsInternalServiceHTTPBaseURL(t *testing.T) {
	c := newClient("http://openviking:19010/api/", "root-key")
	if c.baseURLErr != nil {
		t.Fatalf("expected internal base URL to be accepted, got %v", c.baseURLErr)
	}
	if c.baseURL != "http://openviking:19010/api" {
		t.Fatalf("unexpected normalized base URL: %q", c.baseURL)
	}
}

func TestClient_Content_OverviewFallsBackToReadForLeafURI(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path+"?"+r.URL.RawQuery)
		switch r.URL.Path {
		case "/api/v1/content/overview":
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]any{
				"status": "error",
				"result": nil,
				"error": map[string]any{
					"code":    "INTERNAL",
					"message": "viking://user/memories/preferences/foo.md is not a directory",
				},
			})
		case "/api/v1/content/read":
			writeJSON(w, apiResp("full leaf content"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := newClient(srv.URL, "")
	content, err := c.Content(context.Background(), newIdent(), "viking://user/memories/preferences/foo.md", memory.MemoryLayerOverview)
	if err != nil {
		t.Fatalf("Content failed: %v", err)
	}
	if content != "full leaf content" {
		t.Fatalf("unexpected content: %q", content)
	}
	if len(paths) != maxReadRetries+2 {
		t.Fatalf("expected %d requests, got %d", maxReadRetries+2, len(paths))
	}
	for i := 0; i <= maxReadRetries; i++ {
		if !strings.HasPrefix(paths[i], "/api/v1/content/overview?") {
			t.Fatalf("request[%d] should hit overview, got %q", i, paths[i])
		}
	}
	if !strings.HasPrefix(paths[len(paths)-1], "/api/v1/content/read?") {
		t.Fatalf("last request should hit read, got %q", paths[len(paths)-1])
	}
}

func TestClient_Content_OverviewDoesNotFallbackForOtherErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]any{
			"status": "error",
			"result": nil,
			"error": map[string]any{
				"code":    "INTERNAL",
				"message": "unexpected backend failure",
			},
		})
	}))
	defer srv.Close()

	c := newClient(srv.URL, "")
	_, err := c.Content(context.Background(), newIdent(), "viking://user/memories/preferences/foo.md", memory.MemoryLayerOverview)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unexpected backend failure") {
		t.Fatalf("unexpected error: %v", err)
	}
}
