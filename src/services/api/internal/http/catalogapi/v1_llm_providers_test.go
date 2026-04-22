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
	sharedoutbound "arkloop/services/shared/outboundurl"
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
