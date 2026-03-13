//go:build desktop

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DesktopConfig holds configuration specific to desktop (single-user) mode.
type DesktopConfig struct {
	ListenAddr  string
	DataDir     string
	MaxInFlight int
	SSE         SSEConfig
}

// LoadDesktopConfig reads desktop configuration from environment variables,
// falling back to sensible defaults for single-user operation.
func LoadDesktopConfig() (DesktopConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return DesktopConfig{}, fmt.Errorf("get home dir: %w", err)
	}

	cfg := DesktopConfig{
		ListenAddr:  defaultAddr,
		DataDir:     filepath.Join(home, ".arkloop"),
		MaxInFlight: defaultMaxInFlight,
		SSE:         defaultSSEConfig(),
	}

	if v := strings.TrimSpace(os.Getenv(apiGoAddrEnv)); v != "" {
		cfg.ListenAddr = v
	}
	if v := strings.TrimSpace(os.Getenv("ARKLOOP_DATA_DIR")); v != "" {
		cfg.DataDir = v
	}
	if v := strings.TrimSpace(os.Getenv(apiMaxInFlightEnv)); v != "" {
		n, err := parsePositiveInt(v)
		if err != nil {
			return DesktopConfig{}, fmt.Errorf("%s: %w", apiMaxInFlightEnv, err)
		}
		cfg.MaxInFlight = n
	}

	return cfg, nil
}
