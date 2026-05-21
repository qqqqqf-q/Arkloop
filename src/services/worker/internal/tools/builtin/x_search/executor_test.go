package xsearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"arkloop/services/worker/internal/tools"
)

func TestXAIProviderSendsXSearchTool(t *testing.T) {
	var gotAuth string
	var gotPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/responses" {
			t.Fatalf("path = %s, want /responses", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		_, _ = w.Write([]byte(`{"output_text":"answer","citations":[{"url":"https://x.com/a/status/1"}]}`))
	}))
	defer server.Close()

	provider, err := NewXAIProvider(XAIProviderConfig{
		APIKey:  "xai-key",
		BaseURL: server.URL,
		Model:   "grok-test",
	})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}

	result, err := provider.Search(t.Context(), Request{
		Query:           "arkloop",
		AllowedXHandles: []string{"NousResearch"},
		FromDate:        "2026-05-01",
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if gotAuth != "Bearer xai-key" {
		t.Fatalf("authorization = %q", gotAuth)
	}
	if gotPayload["model"] != "grok-test" || gotPayload["store"] != false {
		t.Fatalf("unexpected payload: %#v", gotPayload)
	}
	input, ok := gotPayload["input"].([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("unexpected input: %#v", gotPayload["input"])
	}
	message, ok := input[0].(map[string]any)
	if !ok || message["role"] != "user" || message["content"] != "arkloop" {
		t.Fatalf("unexpected input message: %#v", input[0])
	}
	tools, ok := gotPayload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("unexpected tools: %#v", gotPayload["tools"])
	}
	tool, ok := tools[0].(map[string]any)
	if !ok || tool["type"] != "x_search" || tool["from_date"] != "2026-05-01" {
		t.Fatalf("unexpected tool payload: %#v", tool)
	}
	if result.Answer != "answer" || result.CredentialSource != "api_key" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if len(result.Citations) != 1 || result.Citations[0] != "https://x.com/a/status/1" {
		t.Fatalf("unexpected citations: %#v", result.Citations)
	}
}

func TestXAIProviderPrefersOAuthToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer oauth-token" {
			t.Fatalf("authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"output_text":"answer"}`))
	}))
	defer server.Close()

	provider, err := NewXAIProvider(XAIProviderConfig{
		APIKey:     "xai-key",
		OAuthValue: `{"access_token":"oauth-token"}`,
		BaseURL:    server.URL,
	})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	result, err := provider.Search(t.Context(), Request{Query: "arkloop"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if result.CredentialSource != "oauth" {
		t.Fatalf("credential source = %q", result.CredentialSource)
	}
}

func TestXAIProviderAuthModeCanSelectAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer xai-key" {
			t.Fatalf("authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"output_text":"answer"}`))
	}))
	defer server.Close()

	provider, err := NewXAIProvider(XAIProviderConfig{
		APIKey:     "xai-key",
		OAuthValue: `{"access_token":"oauth-token"}`,
		BaseURL:    server.URL,
		AuthMode:   "api_key",
	})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	result, err := provider.Search(t.Context(), Request{Query: "arkloop"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if result.CredentialSource != "api_key" {
		t.Fatalf("credential source = %q", result.CredentialSource)
	}
}

func TestXAIProviderRetriesServerError(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, "temporary", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"output_text":"recovered"}`))
	}))
	defer server.Close()

	provider, err := NewXAIProvider(XAIProviderConfig{
		APIKey:  "xai-key",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}

	result, err := provider.Search(t.Context(), Request{Query: "arkloop"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if result.Answer != "recovered" {
		t.Fatalf("answer = %q", result.Answer)
	}
}

type delayedProvider struct {
	delay time.Duration
}

func (p delayedProvider) Search(ctx context.Context, request Request) (Result, error) {
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-time.After(p.delay):
		return Result{Query: request.Query, Answer: "ok"}, nil
	}
}

func TestToolExecutorUsesXSearchMinimumTimeout(t *testing.T) {
	exec := &ToolExecutor{
		provider: delayedProvider{delay: 5 * time.Millisecond},
		timeout:  20 * time.Millisecond,
	}
	timeoutMs := 1
	result := exec.Execute(t.Context(), GroupName, map[string]any{"query": "arkloop"}, tools.ExecutionContext{TimeoutMs: &timeoutMs}, "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if got := result.ResultJSON["answer"]; got != "ok" {
		t.Fatalf("answer = %#v", got)
	}
}
