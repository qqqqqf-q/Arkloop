package writefile

import (
	"context"
	"fmt"
	"time"

	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/fileops"
)

const maxWriteSize = 5 * 1024 * 1024 // 5MB

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
	content, _ := args["content"].(string)

	if len(content) > maxWriteSize {
		return errResult(fmt.Sprintf("content too large (%d bytes, max %d)", len(content), maxWriteSize), started)
	}

	backend := fileops.ResolveBackend(execCtx.RuntimeSnapshot, execCtx.WorkDir, execCtx.RunID.String(), resolveAccountID(execCtx), execCtx.ProfileRef, execCtx.WorkspaceRef)

	if err := backend.WriteFile(ctx, filePath, []byte(content)); err != nil {
		return errResult(fmt.Sprintf("write failed: %s", err.Error()), started)
	}

	if e.Tracker != nil {
		normPath := backend.NormalizePath(filePath)
		e.Tracker.RecordWriteForRun(execCtx.RunID.String(), normPath)
		e.Tracker.InvalidateReadState(execCtx.RunID.String(), normPath)
	}

	result := map[string]any{
		"file_path": filePath,
		"status":    "written",
	}

	return tools.ExecutionResult{
		ResultJSON: result,
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
