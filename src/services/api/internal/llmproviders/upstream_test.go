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

func TestListOpenAIModelsRejectsUnsafeBaseURL(t *testing.T) {
	baseURL := "https://10.0.0.1/v1"
	accountID := uuid.New()
	provider := data.LlmCredential{
		ID:       uuid.New(),
		AccountID:    &accountID,
		Provider: "openai",
		Name:     "unsafe",
		BaseURL:  &baseURL,
	}

	_, err := listOpenAIModels(context.Background(), provider, "sk-test")
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

func TestListOpenAIModelsAllowsLoopbackHTTPInTests(t *testing.T) {
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4.1"},{"id":"gpt-4o"}]}`))
	}))
	defer server.Close()

	baseURL := server.URL + "/v1"
	accountID := uuid.New()
	provider := data.LlmCredential{
		ID:       uuid.New(),
		AccountID:    &accountID,
		Provider: "openai",
		Name:     "safe",
		BaseURL:  &baseURL,
	}

	models, err := listOpenAIModels(context.Background(), provider, "sk-test")
	if err != nil {
		t.Fatalf("listOpenAIModels: %v", err)
	}
	if len(models) != 2 || models[0].ID != "gpt-4.1" || models[1].ID != "gpt-4o" {
		t.Fatalf("unexpected models: %#v", models)
	}
}
