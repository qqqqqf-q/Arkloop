package app

import (
	"os"
	"path/filepath"
	"strings"
)

func defaultPluginDataRoot() string {
	if value := strings.TrimSpace(os.Getenv("ARKLOOP_PLUGIN_DATA_ROOT")); value != "" {
		return value
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".arkloop", "plugins")
	}
	return filepath.Join(os.TempDir(), "arkloop", "plugins")
}
