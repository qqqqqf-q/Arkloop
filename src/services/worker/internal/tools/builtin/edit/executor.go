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

	if !e.Tracker.HasBeenRead(filePath) {
		return errResult("must read the file before editing; use read_file first", started)
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
	if count > 1 {
		return errResult(fmt.Sprintf("old_string matches %d locations; include more surrounding context to make it unique", count), started)
	}

	var newContent string
	if count == 1 {
		newContent = strings.Replace(content, oldString, newString, 1)
	} else {
		// count == 0: flexible whitespace-normalized fallback
		trimmedContent := trimLines(content)
		trimmedOld := trimLines(oldString)
		trimCount := strings.Count(trimmedContent, trimmedOld)
		if trimCount == 0 {
			return errResult("old_string not found in file; verify exact text including whitespace", started)
		}
		if trimCount > 1 {
			return errResult(fmt.Sprintf("old_string matches %d locations after whitespace normalization; include more surrounding context", trimCount), started)
		}
		newContent = replaceWithTrimmedMatch(content, oldString, newString)
	}
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

func trimLines(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.Join(lines, "\n")
}

func replaceWithTrimmedMatch(content, oldString, newString string) string {
	origLines := strings.Split(content, "\n")
	oldLines := strings.Split(oldString, "\n")

	trimmedOrigLines := make([]string, len(origLines))
	for i, l := range origLines {
		trimmedOrigLines[i] = strings.TrimSpace(l)
	}
	trimmedOldLines := make([]string, len(oldLines))
	for i, l := range oldLines {
		trimmedOldLines[i] = strings.TrimSpace(l)
	}

	start := -1
	for i := 0; i <= len(origLines)-len(oldLines); i++ {
		match := true
		for j := 0; j < len(oldLines); j++ {
			if trimmedOrigLines[i+j] != trimmedOldLines[j] {
				match = false
				break
			}
		}
		if match {
			start = i
			break
		}
	}
	if start == -1 {
		return content
	}

	result := make([]string, 0, len(origLines)-len(oldLines)+1)
	result = append(result, origLines[:start]...)
	result = append(result, newString)
	result = append(result, origLines[start+len(oldLines):]...)
	return strings.Join(result, "\n")
}
