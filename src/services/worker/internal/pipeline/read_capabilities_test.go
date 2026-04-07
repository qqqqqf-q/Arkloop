package pipeline

import (
	"testing"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"
)

func TestApplyReadImageSourceVisibility_RemovesImageKindsFromEnum(t *testing.T) {
	specs := []llm.ToolSpec{
		{
			Name: "read",
			JSONSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"prompt":     map[string]any{"type": "string"},
					"max_bytes":  map[string]any{"type": "integer"},
					"timeout_ms": map[string]any{"type": "integer"},
					"source": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"kind": map[string]any{
								"type": "string",
								"enum": []any{"file_path", "message_attachment", "remote_url"},
							},
							"file_path":      map[string]any{"type": "string"},
							"attachment_key": map[string]any{"type": "string"},
							"url":            map[string]any{"type": "string"},
						},
					},
				},
			},
		},
	}

	patched := ApplyReadImageSourceVisibility(specs, false, false)
	readSpec, ok := findToolSpec(patched, "read")
	if !ok {
		t.Fatal("expected read spec")
	}

	source := nestedObject(readSpec.JSONSchema, "properties", "source")
	kind := nestedObject(source, "properties", "kind")
	enum, _ := kind["enum"].([]any)
	if len(enum) != 1 || enum[0] != "file_path" {
		t.Fatalf("expected only file_path after pruning, got %#v", enum)
	}
	props := nestedObject(readSpec.JSONSchema, "properties")
	if _, ok := props["prompt"]; ok {
		t.Fatal("did not expect prompt when image sources are hidden")
	}
	if _, ok := props["max_bytes"]; ok {
		t.Fatal("did not expect max_bytes when image sources are hidden")
	}
	if _, ok := props["timeout_ms"]; ok {
		t.Fatal("did not expect timeout_ms when image sources are hidden")
	}
	sourceProps := nestedObject(source, "properties")
	if _, ok := sourceProps["attachment_key"]; ok {
		t.Fatal("did not expect attachment_key when image sources are hidden")
	}
	if _, ok := sourceProps["url"]; ok {
		t.Fatal("did not expect url when image sources are hidden")
	}
}

func TestResolveReadCapabilities_UsesRouteAndReadSpec(t *testing.T) {
	route := &routing.SelectedProviderRoute{
		Route: routing.ProviderRouteRule{
			AdvancedJSON: map[string]any{
				"available_catalog": map[string]any{
					"input_modalities": []string{"text"},
				},
			},
		},
	}
	readSpec := llm.ToolSpec{
		Name: "read",
		JSONSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"source": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"kind": map[string]any{
							"type": "string",
							"enum": []any{"file_path", "message_attachment", "remote_url"},
						},
					},
				},
			},
		},
	}

	caps := ResolveReadCapabilities(
		route,
		[]llm.ToolSpec{readSpec},
		map[string]string{"read": "read.minimax"},
	)
	if caps.NativeImageInput {
		t.Fatal("expected native image input to be false")
	}
	if !caps.ImageBridgeEnabled {
		t.Fatal("expected image bridge to be true")
	}
	if !caps.ReadImageSourcesVisible {
		t.Fatal("expected read image sources to be visible")
	}
}

func TestResolveReadCapabilities_NativeImageInputBypassesPlaceholderPath(t *testing.T) {
	route := &routing.SelectedProviderRoute{
		Route: routing.ProviderRouteRule{
			AdvancedJSON: map[string]any{
				"available_catalog": map[string]any{
					"input_modalities": []string{"text", "image"},
				},
			},
		},
	}
	caps := ResolveReadCapabilities(route, nil, nil)
	if !caps.NativeImageInput {
		t.Fatal("expected native image input true")
	}
	if caps.ReadImageSourcesVisible {
		t.Fatal("expected read image sources hidden")
	}
}

func TestResolveReadCapabilities_DoesNotUseSearchableReadSpec(t *testing.T) {
	route := &routing.SelectedProviderRoute{
		Route: routing.ProviderRouteRule{
			AdvancedJSON: map[string]any{
				"available_catalog": map[string]any{
					"input_modalities": []string{"text"},
				},
			},
		},
	}
	caps := ResolveReadCapabilities(
		route,
		nil,
		map[string]string{"read": "read.minimax"},
	)
	if caps.ReadImageSourcesVisible {
		t.Fatal("expected read image sources hidden when read is not in final specs")
	}
	if !caps.ImageBridgeEnabled {
		t.Fatal("expected bridge enabled from active provider")
	}
}

func makeImageSourceSpec() []llm.ToolSpec {
	return []llm.ToolSpec{
		{
			Name: "read",
			JSONSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"prompt":     map[string]any{"type": "string"},
					"max_bytes":  map[string]any{"type": "integer"},
					"timeout_ms": map[string]any{"type": "integer"},
					"source": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"kind": map[string]any{
								"type": "string",
								"enum": []any{"file_path", "message_attachment", "remote_url"},
							},
							"file_path":      map[string]any{"type": "string"},
							"attachment_key": map[string]any{"type": "string"},
							"url":            map[string]any{"type": "string"},
						},
					},
				},
			},
		},
	}
}

func TestApplyReadImageSourceVisibility_NativeImageInput_NoBridge(t *testing.T) {
	patched := ApplyReadImageSourceVisibility(makeImageSourceSpec(), false, true)
	readSpec, ok := findToolSpec(patched, "read")
	if !ok {
		t.Fatal("expected read spec")
	}

	source := nestedObject(readSpec.JSONSchema, "properties", "source")
	kind := nestedObject(source, "properties", "kind")
	enum, _ := kind["enum"].([]any)

	hasAttachment, hasRemote := false, false
	for _, v := range enum {
		switch v {
		case "message_attachment":
			hasAttachment = true
		case "remote_url":
			hasRemote = true
		}
	}
	if !hasAttachment {
		t.Fatal("expected message_attachment in enum")
	}
	if hasRemote {
		t.Fatal("did not expect remote_url in enum")
	}

	props := nestedObject(readSpec.JSONSchema, "properties")
	for _, field := range []string{"prompt", "max_bytes", "timeout_ms"} {
		if _, ok := props[field]; ok {
			t.Fatalf("did not expect %s when nativeImageInput=true", field)
		}
	}

	sourceProps := nestedObject(source, "properties")
	if _, ok := sourceProps["attachment_key"]; !ok {
		t.Fatal("expected attachment_key to remain")
	}
	if _, ok := sourceProps["url"]; ok {
		t.Fatal("did not expect url when remote_url is stripped")
	}
}

func TestApplyReadImageSourceVisibility_NativeImageInput_WithBridge(t *testing.T) {
	patched := ApplyReadImageSourceVisibility(makeImageSourceSpec(), true, true)
	readSpec, ok := findToolSpec(patched, "read")
	if !ok {
		t.Fatal("expected read spec")
	}

	source := nestedObject(readSpec.JSONSchema, "properties", "source")
	kind := nestedObject(source, "properties", "kind")
	enum, _ := kind["enum"].([]any)

	hasAttachment, hasRemote := false, false
	for _, v := range enum {
		switch v {
		case "message_attachment":
			hasAttachment = true
		case "remote_url":
			hasRemote = true
		}
	}
	if !hasAttachment {
		t.Fatal("expected message_attachment in enum")
	}
	if !hasRemote {
		t.Fatal("expected remote_url in enum when exposeImageSources=true")
	}

	props := nestedObject(readSpec.JSONSchema, "properties")
	for _, field := range []string{"prompt", "max_bytes", "timeout_ms"} {
		if _, ok := props[field]; ok {
			t.Fatalf("did not expect %s when nativeImageInput=true", field)
		}
	}
}
