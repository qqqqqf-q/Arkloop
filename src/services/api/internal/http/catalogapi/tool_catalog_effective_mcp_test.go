package catalogapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestListEffectiveMCPHTTPToolsInitializesBeforeList(t *testing.T) {
	var (
		mu       sync.Mutex
		sequence []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		method, _ := payload["method"].(string)
		mu.Lock()
		sequence = append(sequence, method)
		mu.Unlock()

		if method != "initialize" && r.Header.Get("Mcp-Session-Id") != "session-1" {
			t.Errorf("%s missing session header", method)
		}
		switch method {
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "session-1")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      payload["id"],
				"result": map[string]any{
					"protocolVersion": effectiveMCPProtocolVersion,
					"capabilities":    map[string]any{},
					"serverInfo":      map[string]any{"name": "test", "version": "0"},
				},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      payload["id"],
				"result": map[string]any{
					"tools": []any{
						map[string]any{"name": "echo", "description": "echo back"},
					},
				},
			})
		default:
			t.Errorf("unexpected method %q", method)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	tools, err := listEffectiveMCPHTTPTools(t.Context(), effectiveMCPServerConfig{
		ServerID:      "demo",
		Transport:     "streamable_http",
		URL:           server.URL,
		Headers:       map[string]string{},
		CallTimeoutMs: 1000,
	})
	if err != nil {
		t.Fatalf("listEffectiveMCPHTTPTools failed: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("unexpected tools: %#v", tools)
	}

	mu.Lock()
	got := append([]string{}, sequence...)
	mu.Unlock()
	want := []string{"initialize", "notifications/initialized", "tools/list"}
	if len(got) != len(want) {
		t.Fatalf("sequence=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sequence=%v want=%v", got, want)
		}
	}
}
