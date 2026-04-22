package readfile

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

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

	backend := fileops.ResolveBackend(execCtx.RuntimeSnapshot, execCtx.WorkDir, execCtx.RunID.String(), resolveAccountID(execCtx), execCtx.ProfileRef, execCtx.WorkspaceRef)

	info, err := backend.Stat(ctx, filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fileNotFoundResult(filePath, execCtx.WorkDir, started)
		}
		return errResult(fmt.Sprintf("stat failed: %s", err.Error()), started)
	}
	if info.IsDir {
		return errResult(fmt.Sprintf("path is a directory: %s", filePath), started)
	}

	normPath := backend.NormalizePath(filePath)
	runID := execCtx.RunID.String()
	mtimeNano := info.ModTime.UnixNano()

	// dedup: check before reading to skip file IO when content unchanged
	if e.Tracker != nil {
		estimatedEnd := offset + limit - 1
		if e.Tracker.CheckReadDedup(runID, normPath, mtimeNano, offset, estimatedEnd) {
			return tools.ExecutionResult{
				ResultJSON: map[string]any{
					"file_path": filePath,
					"status":    "file_unchanged",
					"message":   "文件自上次读取后未发生变化",
				},
				DurationMs: durationMs(started),
			}
		}
	}

	var content string
	var totalLines int
	var truncated bool

	if info.Size > int64(fileops.MaxReadSize) {
		if local, ok := backend.(*fileops.LocalBackend); ok {
			resolved, err := local.ResolvePath(filePath)
			if err != nil {
				return errResult(fmt.Sprintf("resolve path failed: %s", err.Error()), started)
			}
			content, totalLines, truncated, err = fileops.ReadLinesFromFile(resolved, offset-1, limit)
			if err != nil {
				return errResult(fmt.Sprintf("read failed: %s", err.Error()), started)
			}
		} else {
			data, err := backend.ReadFile(ctx, filePath)
			if err != nil {
				return errResult(fmt.Sprintf("read failed: %s", err.Error()), started)
			}
			content, totalLines, truncated = fileops.ReadLines(data, offset-1, limit)
		}
	} else {
		data, err := backend.ReadFile(ctx, filePath)
		if err != nil {
			return errResult(fmt.Sprintf("read failed: %s", err.Error()), started)
		}
		content, totalLines, truncated = fileops.ReadLines(data, offset-1, limit)
	}

	actualEnd := offset + limit - 1
	if actualEnd > totalLines {
		actualEnd = totalLines
	}

	numbered := fileops.FormatWithLineNumbers(content, offset)

	// 100K character cap
	if len(numbered) > fileops.MaxOutputChars {
		numbered = truncateUTF8(numbered, fileops.MaxOutputChars)
		truncated = true
		// recalculate actualEnd based on lines actually present after truncation
		actualEnd = offset + strings.Count(numbered, "\n")
	}

	if e.Tracker != nil {
		e.Tracker.RecordReadForRun(runID, normPath)
		e.Tracker.RecordReadState(runID, normPath, mtimeNano, offset, actualEnd)
	}

	result := numbered
	if truncated {
		nextOffset := actualEnd + 1
		result += fmt.Sprintf("\n\n[文件已截断] 显示第 %d-%d 行，共 %d 行。使用 offset=%d 继续读取。",
			offset, actualEnd, totalLines, nextOffset)
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

func fileNotFoundResult(filePath, workDir string, started time.Time) tools.ExecutionResult {
	msg := fmt.Sprintf("file not found: %s", filePath)
	suggestions := fileops.SuggestSimilarPaths(filePath, workDir)
	if len(suggestions) > 0 {
		msg += "\n\n相似路径建议:\n"
		for _, s := range suggestions {
			msg += "  - " + s + "\n"
		}
	}
	return tools.ExecutionResult{
		Error: &tools.ExecutionError{
			ErrorClass: "tool.file_error",
			Message:    msg,
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

func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	s = s[:maxBytes]
	for len(s) > 0 && !utf8.RuneStart(s[len(s)-1]) {
		s = s[:len(s)-1]
	}
	if len(s) > 0 && !utf8.ValidString(s[len(s)-1:]) {
		s = s[:len(s)-1]
	}
	return s
}

func resolveAccountID(execCtx tools.ExecutionContext) string {
	if execCtx.AccountID == nil {
		return ""
	}
	return execCtx.AccountID.String()
}
