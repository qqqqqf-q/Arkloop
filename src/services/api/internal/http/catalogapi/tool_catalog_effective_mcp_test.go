package catalogapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	sharedmcpoauth "arkloop/services/shared/mcpoauth"
)

func TestListEffectiveMCPHTTPToolsInitializesBeforeList(t *testing.T) {
	var sequence []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		method, _ := payload["method"].(string)
		sequence = append(sequence, method)

		switch method {
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "session-1")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      payload["id"],
				"result": map[string]any{
					"protocolVersion": "2025-06-18",
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
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	tools, err := listEffectiveMCPServerTools(t.Context(), effectiveMCPServerConfig{
		ServerID:      "demo",
		Transport:     "streamable_http",
		URL:           server.URL,
		Headers:       map[string]string{},
		CallTimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("listEffectiveMCPServerTools failed: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("unexpected tools: %#v", tools)
	}

	found := map[string]bool{}
	for _, m := range sequence {
		found[m] = true
	}
	if !found["initialize"] || !found["tools/list"] {
		t.Fatalf("expected initialize and tools/list in sequence: %v", sequence)
	}
}

func TestListEffectiveMCPHTTPToolsUsesOAuthAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if r.Header.Get("Authorization") != "Bearer access-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		method, _ := payload["method"].(string)
		w.Header().Set("Content-Type", "application/json")
		switch method {
		case "initialize":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      payload["id"],
				"result": map[string]any{
					"protocolVersion": "2025-06-18",
					"capabilities":    map[string]any{},
					"serverInfo":      map[string]any{"name": "test", "version": "0"},
				},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      payload["id"],
				"result": map[string]any{
					"tools": []any{map[string]any{"name": "echo"}},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	tools, err := listEffectiveMCPServerTools(t.Context(), effectiveMCPServerConfig{
		ServerID:      "demo",
		Transport:     "streamable_http",
		URL:           server.URL,
		Headers:       map[string]string{},
		CallTimeoutMs: 5000,
		OAuth: &sharedmcpoauth.AuthState{
			Tokens: sharedmcpoauth.Tokens{AccessToken: "access-token"},
		},
	})
	if err != nil {
		t.Fatalf("listEffectiveMCPServerTools failed: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("unexpected tools: %#v", tools)
	}
}
