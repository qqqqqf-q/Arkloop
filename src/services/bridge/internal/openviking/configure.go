package openviking

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// ConfigureParams holds the parameters for configuring OpenViking.
type ConfigureParams struct {
	// Embedding config
	EmbeddingProvider  string `json:"embedding_provider"`
	EmbeddingModel     string `json:"embedding_model"`
	EmbeddingAPIKey    string `json:"embedding_api_key"`
	EmbeddingAPIBase   string `json:"embedding_api_base"`
	EmbeddingDimension flexInt `json:"embedding_dimension"`

	// VLM config
	VLMProvider string `json:"vlm_provider"`
	VLMModel    string `json:"vlm_model"`
	VLMAPIKey   string `json:"vlm_api_key"`
	VLMAPIBase  string `json:"vlm_api_base"`

	// Server config (optional overrides)
	RootAPIKey string `json:"root_api_key,omitempty"`
}

// flexInt unmarshals from both JSON numbers and strings (e.g. 1024 or "1024").
type flexInt int

func (fi *flexInt) UnmarshalJSON(b []byte) error {
	var n int
	if err := json.Unmarshal(b, &n); err == nil {
		*fi = flexInt(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		v, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("cannot parse %q as int: %w", s, err)
		}
		*fi = flexInt(v)
		return nil
	}
	return fmt.Errorf("embedding_dimension must be a number or numeric string")
}

// defaultConfig returns the base ov.conf structure with sensible defaults for
// fields that are not managed by ConfigureParams (storage, log, server).
func defaultConfig() map[string]any {
	return map[string]any{
		"server": map[string]any{
			"host":         "0.0.0.0",
			"port":         19010,
			"root_api_key": nil,
			"cors_origins": []any{"*"},
		},
		"storage": map[string]any{
			"vectordb": map[string]any{
				"name":    "context",
				"backend": "local",
				"path":    "/app/data",
			},
			"agfs": map[string]any{
				"port":      1833,
				"log_level": "warn",
				"backend":   "local",
				"path":      "/app/data",
			},
		},
		"embedding": map[string]any{
			"dense": map[string]any{},
		},
		"vlm": map[string]any{},
		"log": map[string]any{
			"level":    "INFO",
			"format":   "%(asctime)s - %(name)s - %(levelname)s - %(message)s",
			"output":   "stdout",
			"rotation": false,
		},
	}
}

// RenderConfig produces the full ov.conf JSON from the given parameters.
// If configPath points to an existing file it is loaded first so that
// non-managed sections (storage, log, etc.) are preserved.
func RenderConfig(configPath string, params ConfigureParams) ([]byte, error) {
	cfg := defaultConfig()

	// Try to load existing config to preserve user customisations.
	if data, err := os.ReadFile(configPath); err == nil {
		var existing map[string]any
		if jsonErr := json.Unmarshal(data, &existing); jsonErr == nil {
			cfg = existing
		}
	}

	// Ensure nested maps exist before writing into them.
	ensureMap(cfg, "server")
	ensureMap(cfg, "embedding")
	embeddingMap := cfg["embedding"].(map[string]any)
	ensureMap(embeddingMap, "dense")
	ensureMap(cfg, "vlm")

	// --- Embedding ---
	dim := int(params.EmbeddingDimension)
	if dim == 0 {
		dim = 1024
	}

	dense := embeddingMap["dense"].(map[string]any)
	dense["provider"] = params.EmbeddingProvider
	dense["model"] = params.EmbeddingModel
	dense["api_key"] = params.EmbeddingAPIKey
	dense["api_base"] = params.EmbeddingAPIBase
	dense["dimension"] = dim
	if params.EmbeddingProvider == "volcengine" {
		dense["input"] = "multimodal"
	}

	// --- VLM ---
	vlm := cfg["vlm"].(map[string]any)
	vlm["provider"] = params.VLMProvider
	vlm["model"] = params.VLMModel
	vlm["api_key"] = params.VLMAPIKey
	vlm["api_base"] = params.VLMAPIBase
	vlm["temperature"] = 0.0
	vlm["max_retries"] = 2

	// --- Server ---
	if params.RootAPIKey != "" {
		srv := cfg["server"].(map[string]any)
		srv["root_api_key"] = params.RootAPIKey
	}

	return json.MarshalIndent(cfg, "", "  ")
}

// WriteConfig writes data to configPath atomically (write tmp then rename).
func WriteConfig(configPath string, data []byte) error {
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	tmp := configPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}

	if err := os.Rename(tmp, configPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename config: %w", err)
	}
	return nil
}

// WaitHealthy polls the OpenViking health endpoint until it returns 200 or
// the context deadline / timeout is exceeded.
func WaitHealthy(ctx context.Context, baseURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("openviking health check timed out after %s", timeout)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
		if err != nil {
			return fmt.Errorf("create health request: %w", err)
		}

		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// ensureMap guarantees that cfg[key] is a map[string]any, creating it if absent.
func ensureMap(cfg map[string]any, key string) {
	v, ok := cfg[key]
	if !ok {
		cfg[key] = map[string]any{}
		return
	}
	if _, isMap := v.(map[string]any); !isMap {
		cfg[key] = map[string]any{}
	}
}
