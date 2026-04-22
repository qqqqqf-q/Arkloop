package glob

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/fileops"
)

const maxResults = 1000

// fileEntry holds a matched file path with optional metadata.
type fileEntry struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Mtime string `json:"mtime"` // RFC3339
}

// skipDirs are directory names skipped during glob fallback walk.
var skipDirs = map[string]struct{}{
	".git":         {},
	"node_modules": {},
	"__pycache__":  {},
	".venv":        {},
	"vendor":       {},
	".idea":        {},
	".vscode":      {},
	"dist":         {},
	"build":        {},
	".next":        {},
	".cache":       {},
}

type Executor struct{}

func (e *Executor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	toolCallID string,
) tools.ExecutionResult {
	started := time.Now()

	pattern, _ := args["pattern"].(string)
	if pattern == "" {
		return errResult("pattern is required", started)
	}
	searchPath, _ := args["path"].(string)

	backend := fileops.ResolveBackend(execCtx.RuntimeSnapshot, execCtx.WorkDir, execCtx.RunID.String(), resolveAccountID(execCtx), execCtx.ProfileRef, execCtx.WorkspaceRef)

	entries, truncated, err := globFiles(ctx, backend, pattern, searchPath)
	if err != nil {
		return errResult(fmt.Sprintf("glob failed: %s", err.Error()), started)
	}

	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"files":     entries,
			"count":     len(entries),
			"truncated": truncated,
		},
		DurationMs: durationMs(started),
	}
}

func globFiles(ctx context.Context, backend fileops.Backend, pattern, searchPath string) ([]fileEntry, bool, error) {
	// ripgrep fast path
	entries, err := globWithRipgrep(ctx, backend, pattern, searchPath)
	if err == nil {
		truncated := len(entries) > maxResults
		if truncated {
			entries = entries[:maxResults]
		}
		return entries, truncated, nil
	}

	localBackend, ok := backend.(*fileops.LocalBackend)
	if !ok {
		return nil, false, err
	}
	if _, resolveErr := localBackend.ResolvePath(searchPath); resolveErr != nil {
		return nil, false, resolveErr
	}

	// fallback: pure Go walk only when the backend itself is local.
	return globWalk(localBackend.NormalizePath(searchPath), pattern)
}

func globWithRipgrep(ctx context.Context, backend fileops.Backend, pattern, searchPath string) ([]fileEntry, error) {
	// Avoid --null: PTY sessions may corrupt NUL bytes in the output stream.
	cmd := fmt.Sprintf("rg --files --glob %s", shellQuote(pattern))
	if searchPath != "" {
		cmd += " " + shellQuote(searchPath)
	}
	stdout, _, exitCode, err := backend.Exec(ctx, cmd)
	if err != nil {
		return nil, err
	}
	// rg exits 1 when no files match — not an error
	if exitCode == 1 {
		return nil, nil
	}
	if exitCode != 0 && stdout == "" {
		return nil, fmt.Errorf("rg exited %d", exitCode)
	}
	var entries []fileEntry
	for _, path := range strings.Split(stdout, "\n") {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if isHiddenPath(path) {
			continue
		}
		e := fileEntry{Path: path}
		if info, statErr := os.Stat(path); statErr == nil {
			e.Size = info.Size()
			e.Mtime = info.ModTime().UTC().Format(time.RFC3339)
		}
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Mtime > entries[j].Mtime
	})
	return entries, nil
}

func globWalk(root, pattern string) ([]fileEntry, bool, error) {
	if root == "" {
		root = "."
	}
	// Prepend **/ if pattern doesn't already have a directory component
	if !strings.Contains(pattern, "/") && !strings.HasPrefix(pattern, "**/") {
		pattern = "**/" + pattern
	}

	var entries []fileEntry
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if strings.HasPrefix(base, ".") && base != "." {
				return filepath.SkipDir
			}
			if _, skip := skipDirs[base]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		matched, matchErr := filepath.Match(pattern, rel)
		if matchErr != nil {
			// try simple suffix match for ** patterns
			if suffix, ok := strings.CutPrefix(pattern, "**/"); ok {
				matched, _ = filepath.Match(suffix, filepath.Base(rel))
			}
		}
		if matched {
			e := fileEntry{Path: filepath.ToSlash(filepath.Clean(rel))}
			e.Size = info.Size()
			e.Mtime = info.ModTime().UTC().Format(time.RFC3339)
			entries = append(entries, e)
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Mtime > entries[j].Mtime
	})
	truncated := len(entries) > maxResults
	if truncated {
		entries = entries[:maxResults]
	}
	return entries, truncated, nil
}

func isHiddenPath(path string) bool {
	for _, segment := range strings.Split(filepath.ToSlash(path), "/") {
		if strings.HasPrefix(segment, ".") && segment != "." && segment != ".." {
			return true
		}
	}
	return false
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func errResult(message string, started time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error: &tools.ExecutionError{
			ErrorClass: "tool.file_error",
			Message:    message,
		},
		DurationMs: durationMs(started),
	}
}

func durationMs(started time.Time) int {
	ms := int(time.Since(started) / time.Millisecond)
	if ms < 0 {
		return 0
	}
	return ms
}

func resolveAccountID(execCtx tools.ExecutionContext) string {
	if execCtx.AccountID == nil {
		return ""
	}
	return execCtx.AccountID.String()
}
