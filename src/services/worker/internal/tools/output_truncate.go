package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	// TruncateMaxChars is the character limit for output returned to the agent.
	TruncateMaxChars = 50_000

	// LargeOutputDir is the base directory for persisted large outputs.
	LargeOutputDir = "/tmp/arkloop-output"

	// MaxBytesPerRun limits total persisted output per run (200MB).
	MaxBytesPerRun = 200 * 1024 * 1024

	// MaxBytesPerFile limits a single persisted file (64MB).
	MaxBytesPerFile = 64 * 1024 * 1024
)

// TruncateResult holds the output of TruncateTail.
type TruncateResult struct {
	Text        string
	Truncated   bool
	DroppedRows int
}

// TruncateTail keeps the last maxChars characters of output, preserving UTF-8
// boundaries. When truncation occurs, a marker line is prepended.
func TruncateTail(output string, maxChars int) TruncateResult {
	if maxChars <= 0 {
		maxChars = TruncateMaxChars
	}
	if utf8.RuneCountInString(output) <= maxChars {
		return TruncateResult{Text: output}
	}

	// walk backward to find the rune boundary at maxChars from the end
	kept := tailRunes(output, maxChars)

	// count dropped lines
	dropped := strings.Count(output[:len(output)-len(kept)], "\n")

	// if we cut mid-line, drop the partial first line of 'kept' for cleanliness
	if idx := strings.IndexByte(kept, '\n'); idx >= 0 && idx < 200 {
		kept = kept[idx+1:]
		dropped++
	}

	marker := fmt.Sprintf("... [truncated %d lines] ...\n", dropped)
	return TruncateResult{
		Text:        marker + kept,
		Truncated:   true,
		DroppedRows: dropped,
	}
}

// tailRunes returns the last n runes of s as a substring.
func tailRunes(s string, n int) string {
	total := utf8.RuneCountInString(s)
	skip := total - n
	if skip <= 0 {
		return s
	}
	i := 0
	for skip > 0 {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		skip--
	}
	return s[i:]
}

// TruncateOutputField applies tail truncation to a single string field.
// Returns the (possibly truncated) string and whether truncation occurred.
func TruncateOutputField(s string, maxChars int) (string, bool) {
	r := TruncateTail(s, maxChars)
	return r.Text, r.Truncated
}

// run-level disk usage tracking
var (
	runDiskMu    sync.Mutex
	runDiskUsage = map[string]int64{}
)

func trackRunDisk(runID string, delta int64) bool {
	runDiskMu.Lock()
	defer runDiskMu.Unlock()
	current := runDiskUsage[runID]
	if current+delta > int64(MaxBytesPerRun) {
		return false
	}
	runDiskUsage[runID] = current + delta
	return true
}

// CleanupRunDisk removes the persisted output directory for a run and resets tracking.
func CleanupRunDisk(runID string) {
	dir := filepath.Join(LargeOutputDir, runID)
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		slog.Warn("cleanup_run_disk: remove failed", "run_id", runID, "error", err.Error())
	}
	runDiskMu.Lock()
	delete(runDiskUsage, runID)
	runDiskMu.Unlock()
}

// untrackRunDisk subtracts delta from the tracked disk usage for a run.
func untrackRunDisk(runID string, delta int64) {
	runDiskMu.Lock()
	defer runDiskMu.Unlock()
	runDiskUsage[runID] -= delta
	if runDiskUsage[runID] <= 0 {
		delete(runDiskUsage, runID)
	}
}

// CleanupStaleOutputDirs removes output directories older than maxAge.
// Call at worker startup as a safety net.
func CleanupStaleOutputDirs(maxAge time.Duration) {
	entries, err := os.ReadDir(LargeOutputDir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			dir := filepath.Join(LargeOutputDir, entry.Name())
			_ = os.RemoveAll(dir)
		}
	}
}

// PersistLargeOutput writes full output to /tmp/arkloop-output/{runID}/{hash}.log
// when it exceeds TruncateMaxChars. Returns the file path or "" if persistence
// was skipped (under limit, over disk quota, or write error).
func PersistLargeOutput(runID, output string) string {
	if utf8.RuneCountInString(output) <= TruncateMaxChars {
		return ""
	}
	data := []byte(output)
	if len(data) > MaxBytesPerFile {
		slog.Warn("persist_large_output: exceeds per-file limit",
			"run_id", runID, "bytes", len(data))
		return ""
	}
	if !trackRunDisk(runID, int64(len(data))) {
		slog.Warn("persist_large_output: exceeds per-run limit", "run_id", runID)
		return ""
	}

	hash := sha256.Sum256(data)
	name := hex.EncodeToString(hash[:8]) + ".log"
	dir := filepath.Join(LargeOutputDir, runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Warn("persist_large_output: mkdir failed", "run_id", runID, "error", err.Error())
		untrackRunDisk(runID, int64(len(data)))
		return ""
	}
	fp := filepath.Join(dir, name)
	// skip if identical content already persisted
	if info, err := os.Stat(fp); err == nil && info.Size() == int64(len(data)) {
		untrackRunDisk(runID, int64(len(data)))
		return fp
	}
	if err := os.WriteFile(fp, data, 0o644); err != nil {
		slog.Warn("persist_large_output: write failed", "run_id", runID, "error", err.Error())
		untrackRunDisk(runID, int64(len(data)))
		return ""
	}
	return fp
}
