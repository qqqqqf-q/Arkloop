package websearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseExaArgsValidatesProviderContract(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
	}{
		{
			name: "rejects queries",
			args: map[string]any{"queries": []any{"arkloop"}},
		},
		{
			name: "rejects old count field",
			args: map[string]any{"query": "arkloop", "count": 3},
		},
		{
			name: "rejects date filters",
			args: map[string]any{"query": "arkloop", "date_after": "2026-01-01"},
		},
		{
			name: "rejects contents",
			args: map[string]any{"query": "arkloop", "contents": map[string]any{"highlights": true}},
		},
		{
			name: "rejects fractional numResults",
			args: map[string]any{"query": "arkloop", "numResults": 1.5},
		},
		{
			name: "rejects zero numResults",
			args: map[string]any{"query": "arkloop", "numResults": 0},
		},
		{
			name: "rejects large numResults",
			args: map[string]any{"query": "arkloop", "numResults": float64(exaMaxSearchCount + 1)},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseExaArgs(tc.args)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if err.ErrorClass != errorArgsInvalid {
				t.Fatalf("unexpected error class: %s", err.ErrorClass)
			}
		})
	}
}

func TestParseExaArgsDefaults(t *testing.T) {
	params, err := parseExaArgs(map[string]any{
		"query":      "  arkloop  ",
		"numResults": float64(2),
	})
	if err != nil {
		t.Fatalf("parseExaArgs returned error: %v", err)
	}
	if params.Query != "arkloop" || params.NumResults != 2 {
		t.Fatalf("unexpected params: %+v", params)
	}

	defaulted, err := parseExaArgs(map[string]any{"query": "arkloop"})
	if err != nil {
		t.Fatalf("parseExaArgs default returned error: %v", err)
	}
	if defaulted.NumResults != defaultMaxResults {
		t.Fatalf("unexpected default numResults: %+v", defaulted)
	}
}

func TestExaProviderSearchCallsHostedMCPAndParsesResponse(t *testing.T) {
	var initialized bool
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.RawQuery != "tools=web_search_exa" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}

		var req exaMCPRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")

		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "session-1")
			writeMCPSSE(t, w, map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{},
					"serverInfo":      map[string]any{"name": "exa"},
				},
			})
		case "notifications/initialized":
			if r.Header.Get("Mcp-Session-Id") != "session-1" {
				t.Fatalf("missing mcp session id")
			}
			initialized = true
			w.WriteHeader(http.StatusAccepted)
		case "tools/call":
			if r.Header.Get("Mcp-Session-Id") != "session-1" {
				t.Fatalf("missing mcp session id")
			}
			params, ok := req.Params.(map[string]any)
			if !ok {
				t.Fatalf("unexpected params: %#v", req.Params)
			}
			if params["name"] != "web_search_exa" {
				t.Fatalf("unexpected tool name: %#v", params["name"])
			}
			args, ok := params["arguments"].(map[string]any)
			if !ok {
				t.Fatalf("unexpected arguments: %#v", params["arguments"])
			}
			if args["query"] != "arkloop" || args["numResults"] != float64(1) {
				t.Fatalf("unexpected tool arguments: %#v", args)
			}
			called = true
			writeMCPSSE(t, w, map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"content": []map[string]any{{
						"type": "text",
						"text": strings.Join([]string{
							"Title: Arkloop",
							"URL: https://arkloop.example/docs",
							"Published: 2026-01-02",
							"Highlights:",
							"first highlight",
							"---",
							"Title: Skip",
							"URL: https://skip.example",
							"Highlights:",
							"second highlight",
						}, "\n"),
					}},
				},
			})
		default:
			t.Fatalf("unexpected MCP method: %s", req.Method)
		}
	}))
	defer server.Close()

	provider := newExaProviderWithEndpoint(server.URL + "/mcp?tools=web_search_exa")
	results, err := provider.Search(context.Background(), SearchRequest{Args: map[string]any{
		"query":      "arkloop",
		"numResults": float64(1),
	}})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if !initialized || !called {
		t.Fatalf("expected initialized=%v called=%v", initialized, called)
	}
	if len(results) != 1 {
		t.Fatalf("expected one normalized result, got %d", len(results))
	}
	if results[0].Title != "Arkloop" || results[0].URL != "https://arkloop.example/docs" || results[0].Published != "2026-01-02" {
		t.Fatalf("unexpected result: %+v", results[0])
	}
	if !strings.Contains(results[0].Snippet, "first highlight") || !strings.Contains(results[0].Text, "Title: Arkloop") {
		t.Fatalf("unexpected result text: %+v", results[0])
	}
}

func TestExaProviderReturnsHTTPBodyOnMCPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("bad gateway"))
	}))
	defer server.Close()

	provider := newExaProviderWithEndpoint(server.URL + "/mcp?tools=web_search_exa")
	_, err := provider.Search(context.Background(), SearchRequest{Args: map[string]any{"query": "arkloop"}})
	if err == nil {
		t.Fatal("expected error")
	}
	httpErr, ok := err.(HttpError)
	if !ok {
		t.Fatalf("expected HttpError, got %T", err)
	}
	if httpErr.StatusCode != http.StatusBadGateway || httpErr.Body != "bad gateway" {
		t.Fatalf("unexpected http error: %+v", httpErr)
	}
}

func TestExaResultJSONPreservesExtractedText(t *testing.T) {
	longText := strings.Repeat("section ", 200)
	payload := Result{
		Title:   "Title",
		URL:     "https://example.com",
		Snippet: longText,
		Summary: longText,
		Text:    longText,
	}.ToJSON()

	if got := payload["snippet"].(string); len(got) >= len(longText) {
		t.Fatalf("expected compact snippet, got len=%d", len(got))
	}
	if got := payload["summary"].(string); got != strings.TrimSpace(longText) {
		t.Fatalf("expected full summary, got len=%d", len(got))
	}
	if got := payload["text"].(string); got != strings.TrimSpace(longText) {
		t.Fatalf("expected full text, got len=%d", len(got))
	}
}

func writeMCPSSE(t *testing.T, w http.ResponseWriter, payload map[string]any) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal sse payload: %v", err)
	}
	_, _ = w.Write([]byte("event: message\n"))
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(data)
	_, _ = w.Write([]byte("\n\n"))
}
