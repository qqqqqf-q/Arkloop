package edit

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/fileops"
)

type Executor struct {
	Tracker *fileops.FileTracker
}

func (e *Executor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	toolCallID string,
) tools.ExecutionResult {
	started := time.Now()

	filePath, _ := args["file_path"].(string)
	if filePath == "" {
		return errResult("file_path is required", started)
	}
	oldString, _ := args["old_string"].(string)
	newString, _ := args["new_string"].(string)

	backend := fileops.ResolveBackend(execCtx.RuntimeSnapshot, execCtx.WorkDir, execCtx.RunID.String(), resolveAccountID(execCtx), execCtx.ProfileRef, execCtx.WorkspaceRef)

	// old_string empty -> create new file
	if oldString == "" {
		return e.createFile(ctx, backend, filePath, newString, started)
	}

	data, err := backend.ReadFile(ctx, filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return errResult(fmt.Sprintf("file not found: %s", filePath), started)
		}
		return errResult(fmt.Sprintf("read failed: %s", err.Error()), started)
	}
	content := string(data)

	count := strings.Count(content, oldString)
	if count == 0 {
		return errResult("old_string not found in file; verify exact text including whitespace", started)
	}
	if count > 1 {
		return errResult(fmt.Sprintf("old_string matches %d locations; include more surrounding context to make it unique", count), started)
	}

	newContent := strings.Replace(content, oldString, newString, 1)
	if err := backend.WriteFile(ctx, filePath, []byte(newContent)); err != nil {
		return errResult(fmt.Sprintf("write failed: %s", err.Error()), started)
	}

	e.Tracker.RecordWrite(filePath)

	additions, removals := fileops.CountDiffLines(content, newContent)
	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"file_path":  filePath,
			"status":     "edited",
			"additions":  additions,
			"removals":   removals,
		},
		DurationMs: durationMs(started),
	}
}

func (e *Executor) createFile(ctx context.Context, backend fileops.Backend, filePath, content string, started time.Time) tools.ExecutionResult {
	if _, err := backend.Stat(ctx, filePath); err == nil {
		return errResult("file already exists; use old_string to make targeted edits instead of creating", started)
	}

	if err := backend.WriteFile(ctx, filePath, []byte(content)); err != nil {
		return errResult(fmt.Sprintf("create failed: %s", err.Error()), started)
	}
	e.Tracker.RecordWrite(filePath)

	lines := strings.Count(content, "\n") + 1
	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"file_path":  filePath,
			"status":     "created",
			"additions":  lines,
		},
		DurationMs: durationMs(started),
	}
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
