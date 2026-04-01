package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// metadataFields are preserved verbatim during compression.
var metadataFields = map[string]struct{}{
	"error":       {},
	"exit_code":   {},
	"status":      {},
	"running":     {},
	"timed_out":   {},
	"cwd":         {},
	"session_id":  {},
	"session_ref": {},
	"duration_ms": {},
	"share_scope": {},
}

var (
	rtkOnce sync.Once
	rtkPath string
)

// resolvedRTKPath returns the path to the rtk binary, or "" if not found.
// Priority: /usr/local/bin/rtk (container/server) → ~/.arkloop/bin/rtk (desktop) → PATH.
func resolvedRTKPath() string {
	rtkOnce.Do(func() {
		// Container / server installs RTK here.
		if _, err := os.Stat("/usr/local/bin/rtk"); err == nil {
			rtkPath = "/usr/local/bin/rtk"
			return
		}
		// Desktop: user installs RTK to ~/.arkloop/bin/rtk.
		if home, err := os.UserHomeDir(); err == nil {
			candidate := filepath.Join(home, ".arkloop", "bin", desktopRTKBinaryName())
			if _, err := os.Stat(candidate); err == nil {
				rtkPath = candidate
				return
			}
		}
		// Fallback: anywhere on PATH.
		if p, err := exec.LookPath("rtk"); err == nil {
			rtkPath = p
		}
	})
	return rtkPath
}

func desktopRTKBinaryName() string {
	if runtime.GOOS == "windows" {
		return "rtk.exe"
	}
	return "rtk"
}

// rtkCompressText pipes text through `rtk compress`. Returns compressed text
// on success, or ("", false) if RTK is unavailable or fails.
func rtkCompressText(text string) (string, bool) {
	bin := resolvedRTKPath()
	if bin == "" {
		return "", false
	}
	cmd := exec.Command(bin, "compress")
	cmd.Stdin = strings.NewReader(text)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", false
	}
	result := out.String()
	if result == "" {
		return "", false
	}
	return result, true
}

// CompressResult applies compression to result.ResultJSON when its marshaled
// size exceeds limit bytes. Prefers RTK semantic compression; falls back to
// head/tail truncation. Returns a new ExecutionResult; original is never mutated.
func CompressResult(toolName string, result ExecutionResult, limit int) ExecutionResult {
	if result.ResultJSON == nil {
		return result
	}
	raw, err := json.Marshal(result.ResultJSON)
	if err != nil || len(raw) <= limit {
		return result
	}
	originalBytes := len(raw)
	compressed := compressMap(result.ResultJSON, limit)
	compressed["_compressed"] = true
	compressed["_original_bytes"] = originalBytes
	return ExecutionResult{
		ResultJSON: compressed,
		Error:      result.Error,
		DurationMs: result.DurationMs,
		Usage:      result.Usage,
		Events:     result.Events,
	}
}

func compressMap(m map[string]any, limit int) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if _, keep := metadataFields[k]; keep {
			out[k] = v
		}
	}
	for k, v := range m {
		if _, keep := metadataFields[k]; keep {
			continue
		}
		out[k] = compressValue(v, limit/2)
	}
	return out
}

func compressValue(v any, budget int) any {
	if budget <= 0 {
		budget = 512
	}
	switch typed := v.(type) {
	case string:
		return compressString(typed, budget)
	case []any:
		return truncateArray(typed, budget)
	case map[string]any:
		return compressMap(typed, budget)
	default:
		return v
	}
}

// compressString applies head/tail truncation when the string exceeds budget.
func compressString(s string, budget int) string {
	return truncateString(s, budget)
}

// truncateString keeps head/tail of a string, inserting a marker in the middle.
func truncateString(s string, budget int) string {
	if len(s) <= budget {
		return s
	}
	keep := budget / 4
	if keep < 32 {
		keep = 32
	}
	if keep*2 >= len(s) {
		// budget too close to string length; just trim the end
		return s[:budget] + fmt.Sprintf("\n...[%d bytes truncated]", len(s)-budget)
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= 4 {
		head := s[:keep]
		tail := s[len(s)-keep:]
		return head + fmt.Sprintf("\n...[%d bytes truncated]...\n", len(s)-keep*2) + tail
	}
	headLines := keep / 80
	if headLines < 2 {
		headLines = 2
	}
	tailLines := 2
	if len(lines) <= headLines+tailLines {
		return s
	}
	omitted := len(lines) - headLines - tailLines
	head := strings.Join(lines[:headLines], "\n")
	tail := strings.Join(lines[len(lines)-tailLines:], "\n")
	return head + fmt.Sprintf("\n...[%d lines truncated]...\n", omitted) + tail
}

// truncateArray keeps first N and last 2 elements.
func truncateArray(arr []any, budget int) any {
	perItem := budget / 4
	if perItem < 1 {
		perItem = 1
	}
	maxItems := budget / max(perItem, 1)
	if maxItems < 3 {
		maxItems = 3
	}
	if len(arr) <= maxItems {
		return arr
	}
	headCount := maxItems - 2
	truncated := len(arr) - headCount - 2
	out := make([]any, 0, headCount+1+2)
	for _, item := range arr[:headCount] {
		out = append(out, compressValue(item, perItem))
	}
	out = append(out, map[string]any{"_truncated": truncated})
	for _, item := range arr[len(arr)-2:] {
		out = append(out, compressValue(item, perItem))
	}
	return out
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
