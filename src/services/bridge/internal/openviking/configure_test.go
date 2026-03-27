package openviking

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRenderConfigWritesGenericBackendsAndClearsRootKey(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "ov.conf")
	initial := []byte(`{"server":{"root_api_key":"stale-key"},"embedding":{"dense":{"extra_headers":{"X-Old":"1"},"input":"multimodal"}},"vlm":{"extra_headers":{"X-Old":"1"}}}`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	data, err := RenderConfig(configPath, ConfigureParams{
		EmbeddingProvider:     "openai",
		EmbeddingModel:        "text-embedding-3-large",
		EmbeddingAPIKey:       "emb-key",
		EmbeddingAPIBase:      "https://api.example.com/v1",
		EmbeddingExtraHeaders: map[string]string{"X-Embed": "1"},
		EmbeddingDimension:    flexInt(3072),
		VLMProvider:           "litellm",
		VLMModel:              "MiniMax-M2.7",
		VLMAPIKey:             "vlm-key",
		VLMAPIBase:            "https://api.example.com/v1",
		VLMExtraHeaders:       map[string]string{"X-VLM": "1"},
		RootAPIKey:            nil,
	})
	if err != nil {
		t.Fatalf("RenderConfig() error = %v", err)
	}

	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	server := cfg["server"].(map[string]any)
	if server["root_api_key"] != nil {
		t.Fatalf("root_api_key = %#v, want nil", server["root_api_key"])
	}

	dense := cfg["embedding"].(map[string]any)["dense"].(map[string]any)
	if dense["provider"] != "openai" {
		t.Fatalf("embedding provider = %#v, want openai", dense["provider"])
	}
	if _, ok := dense["input"]; ok {
		t.Fatalf("expected non-volcengine embedding input to be removed, got %#v", dense["input"])
	}
	if dense["extra_headers"].(map[string]any)["X-Embed"] != "1" {
		t.Fatalf("unexpected embedding extra_headers: %#v", dense["extra_headers"])
	}

	vlm := cfg["vlm"].(map[string]any)
	if vlm["provider"] != "litellm" {
		t.Fatalf("vlm provider = %#v, want litellm", vlm["provider"])
	}
	if vlm["extra_headers"].(map[string]any)["X-VLM"] != "1" {
		t.Fatalf("unexpected vlm extra_headers: %#v", vlm["extra_headers"])
	}
}

func TestRenderConfigPreservesExplicitRootKey(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "ov.conf")
	rootKey := "root-123"

	data, err := RenderConfig(configPath, ConfigureParams{
		EmbeddingProvider:  "volcengine",
		EmbeddingModel:     "doubao-embedding-vision-250615",
		EmbeddingAPIKey:    "emb-key",
		EmbeddingAPIBase:   "https://ark.example.com/api/v3",
		EmbeddingDimension: flexInt(1024),
		VLMProvider:        "volcengine",
		VLMModel:           "doubao-seed-2-0-pro-260215",
		VLMAPIKey:          "vlm-key",
		VLMAPIBase:         "https://ark.example.com/api/v3",
		RootAPIKey:         &rootKey,
	})
	if err != nil {
		t.Fatalf("RenderConfig() error = %v", err)
	}

	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	server := cfg["server"].(map[string]any)
	if server["root_api_key"] != rootKey {
		t.Fatalf("root_api_key = %#v, want %q", server["root_api_key"], rootKey)
	}
	dense := cfg["embedding"].(map[string]any)["dense"].(map[string]any)
	if dense["input"] != "multimodal" {
		t.Fatalf("expected volcengine embedding input multimodal, got %#v", dense["input"])
	}
}
