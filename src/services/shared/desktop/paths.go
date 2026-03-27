//go:build desktop

package desktop

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolveDataDir returns the desktop data directory, preferring an explicit
// value, then ARKLOOP_DATA_DIR, then ~/.arkloop.
func ResolveDataDir(explicit string) (string, error) {
	if value := strings.TrimSpace(explicit); value != "" {
		return value, nil
	}
	if value := strings.TrimSpace(os.Getenv("ARKLOOP_DATA_DIR")); value != "" {
		return value, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".arkloop"), nil
}

// StorageRoot returns the shared desktop object-store root for a data dir.
func StorageRoot(dataDir string) string {
	return filepath.Join(strings.TrimSpace(dataDir), "storage")
}

func MCPRoot(dataDir string) string {
	return filepath.Join(strings.TrimSpace(dataDir), "mcp")
}

func MCPServersPath(dataDir string) string {
	return filepath.Join(MCPRoot(dataDir), "servers.json")
}
