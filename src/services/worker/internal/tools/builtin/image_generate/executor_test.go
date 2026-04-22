package imagegenerate

import (
	"context"
	"testing"

	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/shared/objectstore"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

type testStore struct {
	key     string
	data    []byte
	options objectstore.PutOptions
	objects map[string]storedObject
}

func (s *testStore) PutObject(_ context.Context, key string, data []byte, options objectstore.PutOptions) error {
	s.key = key
	s.data = append([]byte(nil), data...)
	s.options = options
	return nil
}

func (s *testStore) Put(_ context.Context, key string, data []byte) error {
	return s.PutObject(context.Background(), key, data, objectstore.PutOptions{})
}

func (s *testStore) Get(_ context.Context, key string) ([]byte, error) {
	if obj, ok := s.objects[key]; ok {
		return append([]byte(nil), obj.data...), nil
	}
	return nil, context.DeadlineExceeded
}

func (s *testStore) GetWithContentType(_ context.Context, key string) ([]byte, string, error) {
	if obj, ok := s.objects[key]; ok {
		return append([]byte(nil), obj.data...), obj.contentType, nil
	}
	return nil, "", context.DeadlineExceeded
}

func (s *testStore) Head(_ context.Context, key string) (objectstore.ObjectInfo, error) {
	if obj, ok := s.objects[key]; ok {
		return objectstore.ObjectInfo{Key: key, ContentType: obj.contentType, Size: int64(len(obj.data))}, nil
	}
	return objectstore.ObjectInfo{}, context.DeadlineExceeded
}

func (s *testStore) Delete(_ context.Context, key string) error { return nil }

func (s *testStore) ListPrefix(_ context.Context, _ string) ([]objectstore.ObjectInfo, error) {
	return nil, nil
}

type storedObject struct {
	data        []byte
	contentType string
}

type resolverStub struct {
	value string
}

func (r resolverStub) Resolve(_ context.Context, _ string, _ sharedconfig.Scope) (string, error) {
	return r.value, nil
}

func (r resolverStub) ResolvePrefix(_ context.Context, _ string, _ sharedconfig.Scope) (map[string]string, error) {
	return map[string]string{}, nil
}

func TestToolExecutorExecuteWritesArtifact(t *testing.T) {
	accountID := uuid.New()
	store := &testStore{
		objects: map[string]storedObject{
			accountID.String() + "/demo/source.png": {
				data:        []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0},
				contentType: "image/png",
			},
		},
	}
	routingLoader := routing.NewConfigLoader(nil, routing.ProviderRoutingConfig{
		Credentials: []routing.ProviderCredential{{
			ID:           "cred-openai",
			Name:         "img-openai",
			OwnerKind:    routing.CredentialScopePlatform,
			ProviderKind: routing.ProviderKindOpenAI,
			APIKeyValue:  stringPtr("sk-test"),
		}},
		Routes: []routing.ProviderRouteRule{{
			ID:           "route-openai",
			Model:        "gpt-image-1",
			CredentialID: "cred-openai",
			AdvancedJSON: map[string]any{
				"available_catalog": map[string]any{
					"output_modalities": []any{"image"},
				},
			},
		}},
		DefaultRouteID: "route-openai",
	})
	executor := NewToolExecutor(store, nil, resolverStub{value: "img-openai^gpt-image-1"}, routingLoader)
	executor.generate = func(_ context.Context, _ llm.ResolvedGatewayConfig, req llm.ImageGenerationRequest) (llm.GeneratedImage, error) {
		if len(req.InputImages) != 1 {
			t.Fatalf("unexpected input image count: %d", len(req.InputImages))
		}
		if req.Size != "1024x1024" || req.OutputFormat != "png" {
			t.Fatalf("unexpected request options: %#v", req)
		}
		if !req.ForceOpenAIImageAPI {
			t.Fatalf("expected ForceOpenAIImageAPI to be enabled: %#v", req)
		}
		return llm.GeneratedImage{
			Bytes:         []byte("png-bytes"),
			MimeType:      "image/png",
			ProviderKind:  "openai",
			Model:         "gpt-image-1",
			RevisedPrompt: "draw a precise cat",
		}, nil
	}

	runID := uuid.New()
	threadID := uuid.New()
	result := executor.Execute(context.Background(), AgentSpec.Name, map[string]any{
		"prompt":        "draw a cat",
		"input_images":  []any{"artifact:" + accountID.String() + "/demo/source.png"},
		"size":          "1024x1024",
		"output_format": "png",
	}, tools.ExecutionContext{
		RunID:     runID,
		AccountID: &accountID,
		ThreadID:  &threadID,
	}, "call_1")
	if result.Error != nil {
		t.Fatalf("unexpected error: %#v", result.Error)
	}
	if store.key != accountID.String()+"/"+runID.String()+"/generated-image.png" {
		t.Fatalf("unexpected artifact key: %s", store.key)
	}
	if store.options.ContentType != "image/png" {
		t.Fatalf("unexpected content type: %s", store.options.ContentType)
	}
	if len(store.data) == 0 {
		t.Fatal("expected artifact bytes")
	}
	if result.ResultJSON["provider"] != "openai" || result.ResultJSON["model"] != "gpt-image-1" {
		t.Fatalf("unexpected result json: %#v", result.ResultJSON)
	}
	if result.ResultJSON["revised_prompt"] != "draw a precise cat" {
		t.Fatalf("unexpected revised prompt: %#v", result.ResultJSON["revised_prompt"])
	}
	artifacts, ok := result.ResultJSON["artifacts"].([]map[string]any)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("unexpected artifacts: %#v", result.ResultJSON["artifacts"])
	}
	if artifacts[0]["mime_type"] != "image/png" {
		t.Fatalf("unexpected artifact mime_type: %#v", artifacts[0]["mime_type"])
	}
}

func TestToolExecutorExecutePropagatesGatewayErrorClass(t *testing.T) {
	store := &testStore{}
	routingLoader := routing.NewConfigLoader(nil, routing.ProviderRoutingConfig{
		Credentials: []routing.ProviderCredential{{
			ID:           "cred-gemini",
			Name:         "img-gemini",
			OwnerKind:    routing.CredentialScopePlatform,
			ProviderKind: routing.ProviderKindGemini,
			APIKeyValue:  stringPtr("g-test"),
		}},
		Routes: []routing.ProviderRouteRule{{
			ID:           "route-gemini",
			Model:        "imagen-4.0-generate-001",
			CredentialID: "cred-gemini",
		}},
		DefaultRouteID: "route-gemini",
	})
	executor := NewToolExecutor(store, nil, resolverStub{value: "img-gemini^imagen-4.0-generate-001"}, routingLoader)
	executor.generate = func(context.Context, llm.ResolvedGatewayConfig, llm.ImageGenerationRequest) (llm.GeneratedImage, error) {
		return llm.GeneratedImage{}, llm.GatewayError{
			ErrorClass: llm.ErrorClassProviderNonRetryable,
			Message:    "provider failed",
			Details: map[string]any{
				"status_code":         400,
				"provider_error_body": `{"error":{"message":"bad request"}}`,
			},
		}
	}

	result := executor.Execute(context.Background(), AgentSpec.Name, map[string]any{
		"prompt": "draw a cat",
	}, tools.ExecutionContext{
		RunID:     uuid.New(),
		AccountID: uuidPtr(uuid.New()),
	}, "call_1")
	if result.Error == nil {
		t.Fatal("expected error")
	}
	if result.Error.ErrorClass != llm.ErrorClassProviderNonRetryable {
		t.Fatalf("unexpected error class: %s", result.Error.ErrorClass)
	}
	if result.Error.Details["status_code"] != 400 {
		t.Fatalf("unexpected error details: %#v", result.Error.Details)
	}
	if result.Error.Details["provider_error_body"] != `{"error":{"message":"bad request"}}` {
		t.Fatalf("unexpected provider_error_body: %#v", result.Error.Details)
	}
}

func TestToolExecutorExecuteRejectsCrossAccountInputImage(t *testing.T) {
	accountID := uuid.New()
	otherAccountID := uuid.New()
	store := &testStore{
		objects: map[string]storedObject{
			otherAccountID.String() + "/demo/source.png": {
				data:        []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0},
				contentType: "image/png",
			},
		},
	}
	routingLoader := routing.NewConfigLoader(nil, routing.ProviderRoutingConfig{
		Credentials: []routing.ProviderCredential{{
			ID:           "cred-openai",
			Name:         "img-openai",
			OwnerKind:    routing.CredentialScopePlatform,
			ProviderKind: routing.ProviderKindOpenAI,
			APIKeyValue:  stringPtr("sk-test"),
		}},
		Routes: []routing.ProviderRouteRule{{
			ID:           "route-openai",
			Model:        "gpt-image-1",
			CredentialID: "cred-openai",
		}},
		DefaultRouteID: "route-openai",
	})
	executor := NewToolExecutor(store, nil, resolverStub{value: "img-openai^gpt-image-1"}, routingLoader)

	result := executor.Execute(context.Background(), AgentSpec.Name, map[string]any{
		"prompt":       "draw a cat",
		"input_images": []any{"artifact:" + otherAccountID.String() + "/demo/source.png"},
	}, tools.ExecutionContext{
		RunID:     uuid.New(),
		AccountID: &accountID,
	}, "call_1")
	if result.Error == nil {
		t.Fatal("expected error")
	}
	if result.Error.Message != "input_images[0] is outside the current account" {
		t.Fatalf("unexpected error: %#v", result.Error)
	}
}

func stringPtr(s string) *string { return &s }

func uuidPtr(id uuid.UUID) *uuid.UUID { return &id }
