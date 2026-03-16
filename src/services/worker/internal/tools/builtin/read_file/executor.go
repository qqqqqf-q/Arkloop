package readfile

import (
	"context"
	"fmt"
	"os"
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
	offset := intArg(args, "offset", 1)
	limit := intArg(args, "limit", fileops.DefaultReadLimit)
	if offset < 1 {
		offset = 1
	}
	if limit < 1 {
		limit = fileops.DefaultReadLimit
	}

	backend := fileops.ResolveBackend(execCtx.RuntimeSnapshot, "", execCtx.RunID.String(), resolveAccountID(execCtx), execCtx.ProfileRef, execCtx.WorkspaceRef)

	info, err := backend.Stat(ctx, filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return errResult(fmt.Sprintf("file not found: %s", filePath), started)
		}
		return errResult(fmt.Sprintf("stat failed: %s", err.Error()), started)
	}
	if info.IsDir {
		return errResult(fmt.Sprintf("path is a directory: %s", filePath), started)
	}
	if info.Size > int64(fileops.MaxReadSize) {
		return errResult(fmt.Sprintf("file too large (%d bytes, max %d)", info.Size, fileops.MaxReadSize), started)
	}

	data, err := backend.ReadFile(ctx, filePath)
	if err != nil {
		return errResult(fmt.Sprintf("read failed: %s", err.Error()), started)
	}

	// offset is 1-based for the user, convert to 0-based index
	content, totalLines, truncated := fileops.ReadLines(data, offset-1, limit)
	numbered := fileops.FormatWithLineNumbers(content, offset)

	e.Tracker.RecordRead(filePath)

	result := numbered
	if truncated {
		result += fmt.Sprintf("\n\n(showing lines %d-%d of %d; use offset to read further)", offset, offset+limit-1, totalLines)
	}

	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"content":     result,
			"file_path":   filePath,
			"total_lines": totalLines,
			"truncated":   truncated,
		},
		DurationMs: durationMs(started),
	}
}

func intArg(args map[string]any, key string, defaultVal int) int {
	v, ok := args[key]
	if !ok {
		return defaultVal
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return defaultVal
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
