package tools

import (
	"log/slog"
	"os"
	"path/filepath"
)

// CleanupPersistedToolOutputs removes the .tool-outputs/{run_id} directory
// created by PersistLargeResult. It is safe to call when the directory does
// not exist. Callers must ensure runID is a trusted identifier (e.g. a UUID).
func CleanupPersistedToolOutputs(workDir, runID string) {
	if workDir == "" || runID == "" {
		return
	}
	if !validToolCallIDRe.MatchString(runID) {
		slog.Warn("tool outputs cleanup skipped: invalid run_id", "run_id", runID)
		return
	}
	dir := filepath.Join(workDir, ".tool-outputs", runID)
	if err := os.RemoveAll(dir); err != nil {
		slog.Warn("tool outputs cleanup failed", "run_id", runID, "path", dir, "error", err.Error())
	}
}
