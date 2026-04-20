package glob

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"arkloop/services/shared/objectstore"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/fileops"
)

const maxResults = 1000

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

	backend := fileops.ResolveBackend(execCtx.RuntimeSnapshot, execCtx.WorkDir, execCtx.RunID.String(), tools.ToolOutputScopeID(execCtx.ThreadID, execCtx.RunID), resolveAccountID(execCtx), execCtx.ProfileRef, execCtx.WorkspaceRef, execCtx.ToolOutputStore)

	matches, truncated, err := globFiles(ctx, backend, pattern, searchPath, tools.ToolOutputScopeID(execCtx.ThreadID, execCtx.RunID))
	if err != nil {
		return errResult(fmt.Sprintf("glob failed: %s", err.Error()), started)
	}

	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"files":     matches,
			"count":     len(matches),
			"truncated": truncated,
		},
		DurationMs: durationMs(started),
	}
}

func globFiles(ctx context.Context, backend fileops.Backend, pattern, searchPath, toolOutputScopeID string) ([]string, bool, error) {
	if localBackend, ok := backend.(*fileops.LocalBackend); ok {
		if objectPrefix, displayRoot, scoped, err := fileops.ResolveScopedToolOutputSearch(searchPath, toolOutputScopeID, localBackend.ToolOutputStore); scoped {
			if err != nil {
				return nil, false, err
			}
			return globToolOutputObjects(ctx, localBackend.ToolOutputStore, objectPrefix, displayRoot, pattern)
		}
	}
	if sandboxBackend, ok := backend.(*fileops.SandboxExecBackend); ok {
		if objectPrefix, displayRoot, scoped, err := fileops.ResolveScopedToolOutputSearch(searchPath, toolOutputScopeID, sandboxBackend.ToolOutputStore()); scoped {
			if err != nil {
				return nil, false, err
			}
			return globToolOutputObjects(ctx, sandboxBackend.ToolOutputStore(), objectPrefix, displayRoot, pattern)
		}
	}
	// ripgrep fast path
	matches, err := globWithRipgrep(ctx, backend, pattern, searchPath)
	if err == nil {
		truncated := len(matches) > maxResults
		if truncated {
			matches = matches[:maxResults]
		}
		return matches, truncated, nil
	}

	localBackend, ok := backend.(*fileops.LocalBackend)
	if !ok {
		return nil, false, err
	}

	// fallback: pure Go walk only when the backend itself is local.
	root := localBackend.NormalizePath(searchPath)
	displayRoot := filepath.ToSlash(filepath.Clean(searchPath))
	return globWalk(root, displayRoot, pattern)
}

func globToolOutputObjects(ctx context.Context, store interface {
	ListPrefix(ctx context.Context, prefix string) ([]objectstore.ObjectInfo, error)
}, objectPrefix, displayRoot, pattern string) ([]string, bool, error) {
	objects, err := store.ListPrefix(ctx, objectPrefix)
	if err != nil {
		return nil, false, err
	}
	prefix := strings.TrimSuffix(filepath.ToSlash(displayRoot), "/")
	if prefix != "" {
		prefix += "/"
	}
	if !strings.Contains(pattern, "/") && !strings.HasPrefix(pattern, "**/") {
		pattern = "**/" + pattern
	}
	matches := make([]string, 0, len(objects))
	for _, item := range objects {
		displayPath, ok := fileops.ToolOutputDisplayPathFromObjectKey(item.Key)
		if !ok {
			continue
		}
		if prefix != "" && !strings.HasPrefix(displayPath, prefix) {
			continue
		}
		rel := strings.TrimPrefix(displayPath, prefix)
		matched, matchErr := filepath.Match(pattern, rel)
		if matchErr != nil {
			if suffix, ok := strings.CutPrefix(pattern, "**/"); ok {
				matched, _ = filepath.Match(suffix, filepath.Base(rel))
			}
		}
		if matched {
			matches = append(matches, displayPath)
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		return len(matches[i]) < len(matches[j])
	})
	truncated := len(matches) > maxResults
	if truncated {
		matches = matches[:maxResults]
	}
	return matches, truncated, nil
}

func globWithRipgrep(ctx context.Context, backend fileops.Backend, pattern, searchPath string) ([]string, error) {
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
	var matches []string
	for _, path := range strings.Split(stdout, "\n") {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if isHiddenPath(path) {
			continue
		}
		matches = append(matches, path)
	}
	sort.Slice(matches, func(i, j int) bool {
		return len(matches[i]) < len(matches[j])
	})
	return matches, nil
}

func globWalk(root, displayRoot, pattern string) ([]string, bool, error) {
	if root == "" {
		root = "."
	}
	displayRoot = strings.TrimSpace(filepath.ToSlash(filepath.Clean(displayRoot)))
	if displayRoot == "" || displayRoot == "." {
		displayRoot = ""
	}
	// Prepend **/ if pattern doesn't already have a directory component
	if !strings.Contains(pattern, "/") && !strings.HasPrefix(pattern, "**/") {
		pattern = "**/" + pattern
	}

	var matches []string
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
			display := filepath.ToSlash(filepath.Clean(rel))
			if displayRoot != "" {
				display = filepath.ToSlash(filepath.Join(displayRoot, rel))
			}
			matches = append(matches, display)
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	sort.Slice(matches, func(i, j int) bool {
		return len(matches[i]) < len(matches[j])
	})
	truncated := len(matches) > maxResults
	if truncated {
		matches = matches[:maxResults]
	}
	return matches, truncated, nil
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
