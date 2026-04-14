package nowledge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"arkloop/services/worker/internal/memory"
)

const (
	envBaseURL          = "ARKLOOP_NOWLEDGE_BASE_URL"
	envAPIKey           = "ARKLOOP_NOWLEDGE_API_KEY"
	envRequestTimeoutMs = "ARKLOOP_NOWLEDGE_REQUEST_TIMEOUT_MS"
	defaultTimeoutMs    = 30_000
	defaultLocalBaseURL = "http://127.0.0.1:14242"
	localConfigRelPath  = ".nowledge-mem/config.json"
)

type Config struct {
	BaseURL          string
	APIKey           string
	RequestTimeoutMs int
}

func (c Config) Enabled() bool {
	return strings.TrimSpace(c.BaseURL) != ""
}

func (c Config) resolvedTimeoutMs() int {
	if c.RequestTimeoutMs > 0 {
		return c.RequestTimeoutMs
	}
	return defaultTimeoutMs
}

func LoadConfigFromEnv() Config {
	return Config{
		BaseURL:          strings.TrimSpace(os.Getenv(envBaseURL)),
		APIKey:           strings.TrimSpace(os.Getenv(envAPIKey)),
		RequestTimeoutMs: parseTimeoutMs(os.Getenv(envRequestTimeoutMs)),
	}
}

func LoadLocalConfigFile() (Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}
	path := filepath.Join(homeDir, localConfigRelPath)
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var payload struct {
		APIURL string `json:"apiUrl"`
		APIKey string `json:"apiKey"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Config{}, err
	}
	return Config{
		BaseURL: strings.TrimSpace(payload.APIURL),
		APIKey:  strings.TrimSpace(payload.APIKey),
	}, nil
}

func ResolveDesktopConfig(explicit Config) Config {
	resolved := explicit
	localCfg, err := LoadLocalConfigFile()
	if err == nil {
		if strings.TrimSpace(resolved.BaseURL) == "" {
			resolved.BaseURL = localCfg.BaseURL
		}
		if strings.TrimSpace(resolved.APIKey) == "" {
			resolved.APIKey = localCfg.APIKey
		}
	}
	if strings.TrimSpace(resolved.BaseURL) == "" {
		resolved.BaseURL = defaultLocalBaseURL
	}
	return resolved
}

func NewProvider(cfg Config) memory.MemoryProvider {
	if !cfg.Enabled() {
		return nil
	}
	return NewClient(cfg)
}

func parseTimeoutMs(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return 0
	}
	return value
}
