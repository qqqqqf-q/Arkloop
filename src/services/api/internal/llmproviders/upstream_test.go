package llmproviders

import (
	"context"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"arkloop/services/api/internal/data"
	sharedoutbound "arkloop/services/shared/outboundurl"
	"github.com/google/uuid"
)

func TestListUpstreamModelsRejectsUnsafeBaseURL(t *testing.T) {
	baseURL := "https://10.0.0.1/v1"
	accountID := uuid.New()
	provider := data.LlmCredential{
		ID:          uuid.New(),
		OwnerUserID: &accountID,
		Provider:    "openai",
		Name:        "unsafe",
		BaseURL:     &baseURL,
	}

	_, err := listUpstreamModels(context.Background(), provider, "sk-test")
	if err == nil {
		t.Fatal("expected error")
	}
	upstreamErr, ok := err.(*UpstreamListModelsError)
	if !ok {
		t.Fatalf("expected UpstreamListModelsError, got %T: %v", err, err)
	}
	if upstreamErr.Kind != "request" {
		t.Fatalf("Kind = %q, want request", upstreamErr.Kind)
	}
}

func TestListUpstreamModelsAllowsLoopbackHTTPInTests(t *testing.T) {
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.RawQuery != "" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4.1"},{"id":"gpt-4o"}]}`))
	}))
	defer server.Close()

	baseURL := server.URL + "/v1"
	accountID := uuid.New()
	provider := data.LlmCredential{
		ID:          uuid.New(),
		OwnerUserID: &accountID,
		Provider:    "openai",
		Name:        "safe",
		BaseURL:     &baseURL,
	}

	models, err := listUpstreamModels(context.Background(), provider, "sk-test")
	if err != nil {
		t.Fatalf("listUpstreamModels: %v", err)
	}
	if len(models) != 2 || models[0].ID != "gpt-4.1" || models[1].ID != "gpt-4o" {
		t.Fatalf("unexpected models: %#v", models)
	}
}

func TestListUpstreamModelsAnthropicUsesVersionedPath(t *testing.T) {
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")
	requestCount := 0
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		requestCount++
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "sk-ant" {
			t.Fatalf("unexpected x-api-key: %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2024-01-01" {
			t.Fatalf("unexpected anthropic-version: %q", got)
		}
		if got := r.Header.Get("anthropic-beta"); got != "beta-test" {
			t.Fatalf("unexpected anthropic-beta: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-sonnet","display_name":"Claude Sonnet"}]}`))
	}))
	defer server.Close()

	baseURL := server.URL
	provider := data.LlmCredential{
		ID:       uuid.New(),
		Provider: "anthropic",
		Name:     "anthropic",
		BaseURL:  &baseURL,
		AdvancedJSON: map[string]any{
			"anthropic_version": "2024-01-01",
			"extra_headers": map[string]any{
				"anthropic-beta": "beta-test",
			},
		},
	}

	models, err := listUpstreamModels(context.Background(), provider, "sk-ant")
	if err != nil {
		t.Fatalf("listUpstreamModels: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("requestCount = %d, want 1", requestCount)
	}
	if len(models) != 1 || models[0].ID != "claude-sonnet" {
		t.Fatalf("unexpected models: %#v", models)
	}
}

func TestListUpstreamModelsAnthropicRejectsInvalidExtraHeaders(t *testing.T) {
	provider := data.LlmCredential{
		ID:       uuid.New(),
		Provider: "anthropic",
		Name:     "anthropic",
		AdvancedJSON: map[string]any{
			"extra_headers": map[string]any{
				"x-custom": "bad",
			},
		},
	}

	_, err := listUpstreamModels(context.Background(), provider, "sk-ant")
	if err == nil {
		t.Fatal("expected error")
	}
	upstreamErr, ok := err.(*UpstreamListModelsError)
	if !ok {
		t.Fatalf("expected UpstreamListModelsError, got %T: %v", err, err)
	}
	if upstreamErr.Kind != "unsupported_provider" {
		t.Fatalf("Kind = %q, want unsupported_provider", upstreamErr.Kind)
	}
	if upstreamErr.Err == nil || upstreamErr.Err.Error() != "advanced_json.extra_headers only supports anthropic-beta" {
		t.Fatalf("unexpected raw error: %v", upstreamErr.Err)
	}
}

func TestListUpstreamModelsGeminiUsesModelsList(t *testing.T) {
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.URL.Path != "/v1beta/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("x-goog-api-key"); got != "g-key" {
			t.Fatalf("unexpected x-goog-api-key: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "models": [
    {
      "name": "models/gemini-2.0-flash",
      "displayName": "Gemini 2.0 Flash",
      "inputTokenLimit": 1048576,
      "outputTokenLimit": 8192,
      "supportedGenerationMethods": ["generateContent","countTokens"]
    },
    {
      "name": "models/text-embedding-004",
      "displayName": "Text Embedding 004",
      "inputTokenLimit": 2048,
      "supportedGenerationMethods": ["embedContent"]
    }
  ]
}`))
	}))
	defer server.Close()

	baseURL := server.URL + "/v1beta"
	provider := data.LlmCredential{
		ID:       uuid.New(),
		Provider: "gemini",
		Name:     "gemini",
		BaseURL:  &baseURL,
	}

	models, err := listUpstreamModels(context.Background(), provider, "g-key")
	if err != nil {
		t.Fatalf("listUpstreamModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("unexpected models: %#v", models)
	}
	if models[0].ID != "gemini-2.0-flash" || models[0].Type != "chat" {
		t.Fatalf("unexpected chat model: %#v", models[0])
	}
	if models[0].ContextLength == nil || *models[0].ContextLength != 1048576 {
		t.Fatalf("unexpected context length: %#v", models[0])
	}
	if models[0].MaxOutputTokens == nil || *models[0].MaxOutputTokens != 8192 {
		t.Fatalf("unexpected max output tokens: %#v", models[0])
	}
	if models[1].ID != "text-embedding-004" || models[1].Type != "embedding" {
		t.Fatalf("unexpected embedding model: %#v", models[1])
	}
}
