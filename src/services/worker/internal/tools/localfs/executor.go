//go:build desktop

package localfs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"arkloop/services/worker/internal/tools"
)

const (
	errorArgsInvalid     = "tool.args_invalid"
	errorPathDenied      = "tool.path_denied"
	errorFileNotFound    = "tool.file_not_found"
	errorFileReadFailed  = "tool.file_read_failed"
	errorFileWriteFailed = "tool.file_write_failed"
	maxReadBytes         = 256 * 1024
	envWorkspace         = "ARKLOOP_LOCAL_SHELL_WORKSPACE"
)

// Executor implements tools.Executor for workspace-scoped file operations.
type Executor struct {
	workspaceRoot string
}

func NewExecutor() *Executor {
	root := os.Getenv(envWorkspace)
	if root == "" {
		if wd, err := os.Getwd(); err == nil {
			root = wd
		} else {
			root = os.TempDir()
		}
	}
	return &Executor{workspaceRoot: root}
}

func (e *Executor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	toolCallID string,
) tools.ExecutionResult {
	started := time.Now()

	switch toolName {
	case "file_read":
		return e.executeRead(args, started)
	case "file_write":
		return e.executeWrite(args, started)
	default:
		return errResult(errorArgsInvalid, fmt.Sprintf("unknown localfs tool: %s", toolName), started)
	}
}

func (e *Executor) executeRead(args map[string]any, started time.Time) tools.ExecutionResult {
	relPath := readStringArg(args, "path")
	if strings.TrimSpace(relPath) == "" {
		return errResult(errorArgsInvalid, "parameter path is required", started)
	}

	absPath, err := e.resolvePath(relPath)
	if err != nil {
		return errResult(errorPathDenied, err.Error(), started)
	}

	slog.Info("localfs: file_read", "path", absPath)

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return errResult(errorFileNotFound, fmt.Sprintf("file not found: %s", relPath), started)
		}
		return errResult(errorFileReadFailed, err.Error(), started)
	}
	if info.IsDir() {
		return e.readDirectory(absPath, relPath, started)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return errResult(errorFileReadFailed, err.Error(), started)
	}

	content := string(data)
	truncated := false
	if len(data) > maxReadBytes {
		content = string(data[:maxReadBytes])
		truncated = true
	}

	offset := readIntArg(args, "offset")
	limit := readIntArg(args, "limit")
	if offset > 0 || limit > 0 {
		content, truncated = applyLineRange(content, offset, limit)
	}

	resultJSON := map[string]any{
		"path":      relPath,
		"content":   content,
		"size":      info.Size(),
		"truncated": truncated,
	}
	return tools.ExecutionResult{ResultJSON: resultJSON, DurationMs: durationMs(started)}
}

func (e *Executor) readDirectory(absPath, relPath string, started time.Time) tools.ExecutionResult {
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return errResult(errorFileReadFailed, err.Error(), started)
	}

	items := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		item := map[string]any{
			"name":  entry.Name(),
			"is_dir": entry.IsDir(),
		}
		if info, err := entry.Info(); err == nil {
			item["size"] = info.Size()
		}
		items = append(items, item)
	}

	resultJSON := map[string]any{
		"path":    relPath,
		"is_dir":  true,
		"entries": items,
		"count":   len(items),
	}
	return tools.ExecutionResult{ResultJSON: resultJSON, DurationMs: durationMs(started)}
}

func (e *Executor) executeWrite(args map[string]any, started time.Time) tools.ExecutionResult {
	relPath := readStringArg(args, "path")
	if strings.TrimSpace(relPath) == "" {
		return errResult(errorArgsInvalid, "parameter path is required", started)
	}
	content := readStringArg(args, "content")

	absPath, err := e.resolvePath(relPath)
	if err != nil {
		return errResult(errorPathDenied, err.Error(), started)
	}

	slog.Info("localfs: file_write", "path", absPath, "size", len(content))

	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errResult(errorFileWriteFailed, fmt.Sprintf("create directory failed: %s", err.Error()), started)
	}

	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return errResult(errorFileWriteFailed, err.Error(), started)
	}

	resultJSON := map[string]any{
		"path":          relPath,
		"bytes_written": len(content),
		"success":       true,
	}
	return tools.ExecutionResult{ResultJSON: resultJSON, DurationMs: durationMs(started)}
}

// resolvePath validates the path is within the workspace and returns the absolute path.
func (e *Executor) resolvePath(relPath string) (string, error) {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return "", fmt.Errorf("path is empty")
	}

	// If absolute, check it's under workspace
	if filepath.IsAbs(relPath) {
		cleaned := filepath.Clean(relPath)
		wsClean := filepath.Clean(e.workspaceRoot)
		if !strings.HasPrefix(cleaned, wsClean+string(filepath.Separator)) && cleaned != wsClean {
			return "", fmt.Errorf("path %q is outside the workspace", relPath)
		}
		return cleaned, nil
	}

	joined := filepath.Join(e.workspaceRoot, relPath)
	cleaned := filepath.Clean(joined)
	wsClean := filepath.Clean(e.workspaceRoot)

	if !strings.HasPrefix(cleaned, wsClean+string(filepath.Separator)) && cleaned != wsClean {
		return "", fmt.Errorf("path %q resolves outside the workspace (path traversal blocked)", relPath)
	}

	return cleaned, nil
}

func applyLineRange(content string, offset, limit int) (string, bool) {
	lines := strings.Split(content, "\n")
	total := len(lines)

	start := 0
	if offset > 0 {
		start = offset - 1
	}
	if start > total {
		start = total
	}

	end := total
	if limit > 0 {
		end = start + limit
	}
	if end > total {
		end = total
	}

	truncated := end < total || start > 0
	return strings.Join(lines[start:end], "\n"), truncated
}

func errResult(errorClass, message string, started time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error:      &tools.ExecutionError{ErrorClass: errorClass, Message: message},
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

func readStringArg(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

func readIntArg(args map[string]any, key string) int {
	v, ok := args[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		parsed, err := n.Int64()
		if err == nil {
			return int(parsed)
		}
	}
	return 0
}
