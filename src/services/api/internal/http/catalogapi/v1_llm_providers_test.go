package catalogapi

import (
	"context"
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/llmproviders"
	"arkloop/services/shared/localproviders"
	sharedoutbound "arkloop/services/shared/outboundurl"

	"github.com/google/uuid"
)

func TestDetermineModelTestTypePrefersImageCatalogType(t *testing.T) {
	model := data.LlmRoute{
		AdvancedJSON: map[string]any{
			llmproviders.AvailableCatalogAdvancedKey: map[string]any{
				"type": "image",
			},
		},
	}

	if got := determineModelTestType(model); got != "image" {
		t.Fatalf("determineModelTestType() = %q, want image", got)
	}
}

func TestDetermineModelTestTypeFallsBackToImageOutputModality(t *testing.T) {
	model := data.LlmRoute{
		AdvancedJSON: map[string]any{
			llmproviders.AvailableCatalogAdvancedKey: map[string]any{
				"output_modalities": []any{"image"},
			},
		},
	}

	if got := determineModelTestType(model); got != "image" {
		t.Fatalf("determineModelTestType() = %q, want image", got)
	}
}

func TestLocalProviderFromStatusIsReadOnlyAndSecretFree(t *testing.T) {
	userID := uuid.New()
	provider := localProviderFromStatus(localproviders.ProviderStatus{
		ID:          localproviders.ClaudeCodeProviderID,
		DisplayName: localproviders.ClaudeCodeDisplayName,
		Provider:    localproviders.ClaudeCodeProviderKind,
		AuthMode:    localproviders.AuthModeOAuth,
		Models: []localproviders.Model{{
			ID:              "claude-sonnet-4-6",
			ContextLength:   200000,
			MaxOutputTokens: 64000,
			ToolCalling:     true,
			Reasoning:       true,
			Default:         true,
			Hidden:          true,
			Priority:        900,
		}},
	}, userID)

	if provider.Source != localproviders.SourceLocal || !provider.ReadOnly || provider.AuthMode != localproviders.AuthModeOAuth {
		t.Fatalf("unexpected local provider flags: %#v", provider)
	}
	if provider.Credential.Provider != localproviders.ClaudeCodeProviderKind || provider.Credential.Name != localproviders.ClaudeCodeDisplayName {
		t.Fatalf("unexpected credential: %#v", provider.Credential)
	}
	if provider.Credential.SecretID != nil || provider.Credential.KeyPrefix != nil || provider.Credential.BaseURL != nil {
		t.Fatalf("local provider must not carry stored credential data: %#v", provider.Credential)
	}
	if len(provider.Models) != 1 || provider.Models[0].ShowInPicker {
		t.Fatalf("unexpected local routes: %#v", provider.Models)
	}
}

func TestLocalProviderMutationGuard(t *testing.T) {
	providerID := localProviderUUID(localproviders.ClaudeCodeProviderID)
	if !isLocalProviderUUID(providerID) {
		t.Fatalf("expected local provider uuid to be recognized")
	}
	cases := []struct {
		name   string
		parts  []string
		method string
		want   bool
	}{
		{name: "patch provider", parts: []string{providerID.String()}, method: nethttp.MethodPatch, want: true},
		{name: "delete provider", parts: []string{providerID.String()}, method: nethttp.MethodDelete, want: true},
		{name: "create model", parts: []string{providerID.String(), "models"}, method: nethttp.MethodPost, want: false},
		{name: "patch model picker", parts: []string{providerID.String(), "models", uuid.NewString()}, method: nethttp.MethodPatch, want: false},
		{name: "delete model", parts: []string{providerID.String(), "models", uuid.NewString()}, method: nethttp.MethodDelete, want: false},
		{name: "available models read", parts: []string{providerID.String(), "available-models"}, method: nethttp.MethodGet, want: false},
		{name: "model test read", parts: []string{providerID.String(), "models", uuid.NewString(), "test"}, method: nethttp.MethodPost, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isLocalProviderMutation(tc.parts, tc.method); got != tc.want {
				t.Fatalf("isLocalProviderMutation() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRunLlmProviderModelTestOpenAIImageUsesImagesEndpoint(t *testing.T) {
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")

	var gotPath string
	var gotAuthorization string
	var payload map[string]any
	upstream := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		gotPath = r.URL.Path
		gotAuthorization = r.Header.Get("Authorization")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		w.WriteHeader(nethttp.StatusOK)
	}))
	defer upstream.Close()

	baseURL := upstream.URL + "/v1"
	cfg := llmproviders.ProviderModelTestConfig{
		Credential: data.LlmCredential{
			Provider: "openai",
			BaseURL:  &baseURL,
		},
		Model: data.LlmRoute{
			Model: "gpt-image-1",
			Tags:  []string{"image"},
		},
		APIKey: "sk-image-test",
	}

	if err := runLlmProviderModelTest(context.Background(), cfg); err != nil {
		t.Fatalf("runLlmProviderModelTest() error = %v", err)
	}
	if gotPath != "/v1/images/generations" {
		t.Fatalf("unexpected path: %q", gotPath)
	}
	if gotAuthorization != "Bearer sk-image-test" {
		t.Fatalf("unexpected authorization header: %q", gotAuthorization)
	}
	if payload["model"] != "gpt-image-1" || payload["prompt"] != "ping" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestRunLlmProviderModelTestGeminiImageUsesPredict(t *testing.T) {
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")

	var gotPath string
	var gotAPIKey string
	var payload map[string]any
	upstream := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("x-goog-api-key")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		w.WriteHeader(nethttp.StatusOK)
	}))
	defer upstream.Close()

	baseURL := upstream.URL + "/v1beta"
	cfg := llmproviders.ProviderModelTestConfig{
		Credential: data.LlmCredential{
			Provider: "gemini",
			BaseURL:  &baseURL,
		},
		Model: data.LlmRoute{
			Model: "imagen-4.0-generate-001",
			AdvancedJSON: map[string]any{
				llmproviders.AvailableCatalogAdvancedKey: map[string]any{
					"type": "image",
				},
			},
		},
		APIKey: "g-image-test",
	}

	if err := runLlmProviderModelTest(context.Background(), cfg); err != nil {
		t.Fatalf("runLlmProviderModelTest() error = %v", err)
	}
	if gotPath != "/v1beta/models/imagen-4.0-generate-001:predict" {
		t.Fatalf("unexpected path: %q", gotPath)
	}
	if gotAPIKey != "g-image-test" {
		t.Fatalf("unexpected x-goog-api-key header: %q", gotAPIKey)
	}
	instances, ok := payload["instances"].([]any)
	if !ok || len(instances) != 1 {
		t.Fatalf("unexpected payload instances: %#v", payload)
	}
	first, ok := instances[0].(map[string]any)
	if !ok || first["prompt"] != "ping" {
		t.Fatalf("unexpected payload instances[0]: %#v", instances[0])
	}
}
