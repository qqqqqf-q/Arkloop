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
	if blocked, isBlocked := tools.PlanModeWriteBlocked(execCtx.PipelineRC, started, filePath); isBlocked {
		return blocked
	}
	content, _ := args["content"].(string)

	if len(content) > maxWriteSize {
		return errResult(fmt.Sprintf("content too large (%d bytes, max %d)", len(content), maxWriteSize), started)
	}

	if err := fileops.DetectOmissionInContent(content); err != nil {
		return errResult(err.Error(), started)
	}

	backend := fileops.ResolveBackend(execCtx.RuntimeSnapshot, execCtx.WorkDir, execCtx.RunID.String(), resolveAccountID(execCtx), execCtx.ProfileRef, execCtx.WorkspaceRef)

	// read-before-overwrite check: only applies when file already exists
	runID := execCtx.RunID.String()
	trackingKey := backend.NormalizePath(filePath)
	info, statErr := backend.Stat(ctx, filePath)
	if statErr == nil {
		// file exists — must have been read first
		if e.Tracker == nil || !e.Tracker.HasBeenReadForRun(runID, trackingKey) {
			return errResult(
				fmt.Sprintf("file already exists but has not been read in this run: %s. Call read with source.kind=file_path before overwriting an existing file.", filePath),
				started,
			)
		}
		// staleness check
		lastRead := e.Tracker.LastReadTimeForRun(runID, trackingKey)
		if !lastRead.IsZero() && info.ModTime.After(lastRead) {
			return errResult(
				fmt.Sprintf("file modified since last read: %s. Re-read the file to get the latest content, then retry.", filePath),
				started,
			)
		}
	}

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
	if planMetadata, ok := tools.PlanModePlanFileMetadata(execCtx.PipelineRC, execCtx.WorkDir, filePath); ok {
		for key, value := range planMetadata {
			result[key] = value
		}
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
