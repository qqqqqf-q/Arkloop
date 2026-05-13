package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	sharedmcpinstall "arkloop/services/shared/mcpinstall"
	sharedmcpoauth "arkloop/services/shared/mcpoauth"

	"github.com/google/uuid"
)

func TestHTTPClientInitializesBeforeListTools(t *testing.T) {
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
					"protocolVersion": defaultProtocolVersion,
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
						map[string]any{
							"name":        "echo",
							"description": "echo back",
							"inputSchema": map[string]any{"type": "object"},
						},
					},
				},
			})
		default:
			t.Errorf("unexpected method %q", method)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := &HTTPClient{
		server: ServerConfig{
			Transport:     "streamable_http",
			URL:           server.URL,
			Headers:       map[string]string{},
			CallTimeoutMs: 1000,
		},
		httpClient: server.Client(),
	}
	client.nextID.Store(1)

	tools, err := client.ListTools(t.Context(), 1000)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
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

func TestHTTPClientRefreshesOAuthBeforeRequest(t *testing.T) {
	secretID := uuid.New()
	store := &stubAuthStore{}
	var tokenURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = r.ParseForm()
			if r.Form.Get("refresh_token") != "refresh-old" {
				t.Errorf("refresh_token=%q", r.Form.Get("refresh_token"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access-new",
				"refresh_token": "refresh-new",
				"expires_in":    3600,
			})
		case "/mcp":
			if got := r.Header.Get("Authorization"); got != "Bearer access-new" {
				t.Errorf("Authorization=%q", got)
			}
			writeMCPResponse(t, w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	tokenURL = server.URL + "/token"

	client := newTestOAuthHTTPClient(t, server, store, secretID, tokenURL, sharedmcpoauth.Tokens{
		AccessToken:  "access-old",
		RefreshToken: "refresh-old",
		ExpiresAt:    time.Now().Add(-time.Minute),
	})

	if _, err := client.ListTools(context.Background(), 1000); err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if store.secretID != secretID {
		t.Fatalf("secretID=%s want %s", store.secretID, secretID)
	}
	if store.payload.OAuth == nil {
		t.Fatal("expected persisted oauth")
	}
	if store.payload.OAuth.Tokens.AccessToken != "access-new" || store.payload.OAuth.Tokens.RefreshToken != "refresh-new" {
		t.Fatalf("unexpected persisted oauth: %#v", store.payload.OAuth.Tokens)
	}
}

func TestHTTPClientRefreshesOAuthAfterAuthStatus(t *testing.T) {
	secretID := uuid.New()
	store := &stubAuthStore{}
	var tokenURL string
	var sawOld bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "access-new",
				"expires_in":   3600,
			})
		case "/mcp":
			switch r.Header.Get("Authorization") {
			case "Bearer access-old":
				sawOld = true
				w.WriteHeader(http.StatusUnauthorized)
			case "Bearer access-new":
				writeMCPResponse(t, w, r)
			default:
				t.Errorf("unexpected Authorization=%q", r.Header.Get("Authorization"))
				w.WriteHeader(http.StatusUnauthorized)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	tokenURL = server.URL + "/token"

	client := newTestOAuthHTTPClient(t, server, store, secretID, tokenURL, sharedmcpoauth.Tokens{
		AccessToken:  "access-old",
		RefreshToken: "refresh-old",
		ExpiresAt:    time.Now().Add(time.Hour),
	})

	if _, err := client.ListTools(context.Background(), 1000); err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if !sawOld {
		t.Fatal("expected first request to use old access token")
	}
	if store.payload.OAuth == nil {
		t.Fatal("expected refreshed payload to be persisted")
	}
}

func TestHTTPClientReturnsAuthRequiredWithoutRefreshToken(t *testing.T) {
	secretID := uuid.New()
	store := &stubAuthStore{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := newTestOAuthHTTPClient(t, server, store, secretID, server.URL+"/token", sharedmcpoauth.Tokens{
		AccessToken: "access-old",
		ExpiresAt:   time.Now().Add(time.Hour),
	})

	_, err := client.ListTools(context.Background(), 1000)
	var authErr AuthRequiredError
	if !errors.As(err, &authErr) {
		t.Fatalf("error=%v want AuthRequiredError", err)
	}
	if authErr.StatusCode != http.StatusUnauthorized || authErr.Reason != "missing_refresh_token" {
		t.Fatalf("unexpected auth error: %#v", authErr)
	}
}

type stubAuthStore struct {
	secretID uuid.UUID
	payload  sharedmcpinstall.AuthPayload
}

func (s *stubAuthStore) Save(_ context.Context, secretID uuid.UUID, payload sharedmcpinstall.AuthPayload) error {
	s.secretID = secretID
	s.payload = payload
	return nil
}

func newTestOAuthHTTPClient(t *testing.T, server *httptest.Server, store *stubAuthStore, secretID uuid.UUID, tokenURL string, tokens sharedmcpoauth.Tokens) *HTTPClient {
	t.Helper()
	oauth := &sharedmcpoauth.AuthState{
		Discovery: sharedmcpoauth.DiscoveryState{
			AuthorizationServerMetadata: &sharedmcpoauth.AuthorizationServerMetadata{TokenEndpoint: tokenURL},
		},
		Client: sharedmcpoauth.ClientInformation{ClientID: "client-1", ClientSecret: "secret-1"},
		Tokens: tokens,
	}
	client := &HTTPClient{
		server: ServerConfig{
			ServerID:      "demo",
			Transport:     "streamable_http",
			URL:           server.URL + "/mcp",
			Headers:       map[string]string{},
			CallTimeoutMs: 1000,
			AuthSecretID:  secretID.String(),
			OAuth:         oauth,
		},
		authStore:  store,
		httpClient: server.Client(),
	}
	client.loadAuthState()
	client.nextID.Store(1)
	return client
}

func writeMCPResponse(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		t.Errorf("decode request: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	method, _ := payload["method"].(string)
	if method == "notifications/initialized" {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	result := map[string]any{}
	if method == "tools/list" {
		result["tools"] = []any{map[string]any{"name": "echo", "inputSchema": map[string]any{"type": "object"}}}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      payload["id"],
		"result":  result,
	})
}
