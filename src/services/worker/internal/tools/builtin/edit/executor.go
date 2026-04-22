package edit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/fileops"
	"arkloop/services/worker/internal/tools/coerce"
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
	var replaceAll bool
	if raw, ok := args["replace_all"]; ok {
		v, err := coerce.Bool(raw)
		if err != nil {
			return errResult("replace_all must be a boolean", started)
		}
		replaceAll = v
	}

	backend := fileops.ResolveBackend(execCtx.RuntimeSnapshot, execCtx.WorkDir, execCtx.RunID.String(), resolveAccountID(execCtx), execCtx.ProfileRef, execCtx.WorkspaceRef)

	// create mode
	if oldString == "" {
		return e.createFile(ctx, backend, execCtx, filePath, newString, started)
	}

	// no-op check
	if oldString == newString {
		return editErrResult(errNoOp(filePath), started)
	}

	runID := execCtx.RunID.String()
	trackingKey := backend.NormalizePath(filePath)

	// read-before-edit check
	if e.Tracker == nil || !e.Tracker.HasBeenReadForRun(runID, trackingKey) {
		return editErrResult(errNotRead(filePath), started)
	}

	// stat for size + staleness
	info, statErr := backend.Stat(ctx, filePath)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return editErrResult(errFileNotFound(filePath), started)
		}
		return errResult(fmt.Sprintf("stat failed: %s", statErr.Error()), started)
	}
	if info.Size > maxEditFileSize {
		return editErrResult(errTooLarge(filePath, info.Size), started)
	}

	// staleness: file modified after last read
	// NOTE: TOCTOU race exists between this check and the subsequent read/write.
	// Acceptable risk — the window is small and a full lock would hurt throughput.
	lastRead := e.Tracker.LastReadTimeForRun(runID, trackingKey)
	if !lastRead.IsZero() && info.ModTime.After(lastRead) {
		return editErrResult(errStale(filePath), started)
	}

	data, err := backend.ReadFile(ctx, filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return editErrResult(errFileNotFound(filePath), started)
		}
		return errResult(fmt.Sprintf("read failed: %s", err.Error()), started)
	}

	// UTF-16 BOM detection
	if isUTF16BOM(data) {
		return errResult(fmt.Sprintf("file appears to be UTF-16 encoded: %s; convert to UTF-8 first", filePath), started)
	}

	content := string(data)

	// detect and normalize CRLF
	hasCRLF := strings.Contains(content, "\r\n")
	if hasCRLF {
		content = strings.ReplaceAll(content, "\r\n", "\n")
		oldString = strings.ReplaceAll(oldString, "\r\n", "\n")
		newString = strings.ReplaceAll(newString, "\r\n", "\n")
	}

	// omission placeholder check
	if omErr := DetectOmissionPlaceholders(oldString, newString); omErr != nil {
		return editErrResult(omErr, started)
	}

	// 3-layer progressive matching
	mr := match(content, oldString)
	if mr == nil {
		return editErrResult(errNotFound(filePath, content, oldString), started)
	}

	matchCount := len(mr.indices)
	if !replaceAll && matchCount > 1 {
		return editErrResult(errAmbiguous(filePath, matchCount), started)
	}

	// apply replacement(s) in reverse order to preserve offsets
	newContent := content
	var editStart, oldLen, newLen int
	for i := matchCount - 1; i >= 0; i-- {
		actual := mr.actuals[i]
		replacement := newString

		// quote style preservation for normalized matches
		if mr.strategy == "normalized" || mr.strategy == "regex" {
			replacement = preserveQuoteStyle(oldString, actual, replacement)
		}

		// indentation adjustment for non-exact matches
		if mr.strategy != "exact" {
			targetIndent := extractIndent(actual)
			repLines := strings.Split(replacement, "\n")
			repLines = applyIndentation(repLines, targetIndent)
			replacement = strings.Join(repLines, "\n")
		}

		idx := mr.indices[i]
		newContent = newContent[:idx] + replacement + newContent[idx+len(actual):]

		if i == 0 {
			editStart = idx
			oldLen = len(actual)
			newLen = len(replacement)
		}
	}

	// trailing whitespace strip (skip .md/.mdx) — only on affected lines
	// must run before CRLF restore so byte offsets (editStart/newLen) stay valid
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext != ".md" && ext != ".mdx" {
		newContent = stripTrailingWhitespaceRange(newContent, editStart, editStart+newLen)
	}

	// diff snippet before CRLF restore — offsets are LF-based
	snippet := diffSnippet(newContent, editStart, oldLen, newLen, 4)

	// restore CRLF
	if hasCRLF {
		newContent = strings.ReplaceAll(newContent, "\n", "\r\n")
	}

	if err := backend.WriteFile(ctx, filePath, []byte(newContent)); err != nil {
		return errResult(fmt.Sprintf("write failed: %s", err.Error()), started)
	}

	if e.Tracker != nil {
		e.Tracker.RecordWriteForRun(runID, trackingKey)
		e.Tracker.InvalidateReadState(runID, trackingKey)
	}

	// success response
	additions, removals := fileops.CountDiffLines(content, newContent)
	result := map[string]any{
		"file_path": filePath,
		"status":    "edited",
		"additions": additions,
		"removals":  removals,
	}
	if mr.strategy != "exact" {
		result["match_strategy"] = mr.strategy
	}
	if snippet != "" {
		result["snippet"] = snippet
	}
	return tools.ExecutionResult{
		ResultJSON: result,
		DurationMs: durationMs(started),
	}
}

func (e *Executor) createFile(ctx context.Context, backend fileops.Backend, execCtx tools.ExecutionContext, filePath, content string, started time.Time) tools.ExecutionResult {
	if _, err := backend.Stat(ctx, filePath); err == nil {
		return editErrResult(errFileExists(filePath), started)
	}

	if err := backend.WriteFile(ctx, filePath, []byte(content)); err != nil {
		return errResult(fmt.Sprintf("create failed: %s", err.Error()), started)
	}
	if e.Tracker != nil {
		e.Tracker.RecordWriteForRun(execCtx.RunID.String(), backend.NormalizePath(filePath))
		e.Tracker.InvalidateReadState(execCtx.RunID.String(), backend.NormalizePath(filePath))
	}

	lines := strings.Count(content, "\n") + 1
	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"file_path": filePath,
			"status":    "created",
			"additions": lines,
		},
		DurationMs: durationMs(started),
	}
}

func editErrResult(e *editError, started time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error: &tools.ExecutionError{
			ErrorClass: "tool.edit." + strings.ToLower(e.Code),
			Message:    e.Error(),
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

// isUTF16BOM checks for UTF-16 LE/BE byte order marks.
func isUTF16BOM(data []byte) bool {
	if len(data) < 2 {
		return false
	}
	// UTF-16 BE: FE FF, UTF-16 LE: FF FE
	return (data[0] == 0xFE && data[1] == 0xFF) || (data[0] == 0xFF && data[1] == 0xFE)
}

// stripTrailingWhitespace removes trailing spaces/tabs from each line.
func stripTrailingWhitespace(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.Join(lines, "\n")
}

// stripTrailingWhitespaceRange strips trailing whitespace only on lines
// overlapping the byte range [start, end) in s.
func stripTrailingWhitespaceRange(s string, start, end int) string {
	lines := strings.Split(s, "\n")
	off := 0
	for i, line := range lines {
		lineEnd := off + len(line)
		if off < end && lineEnd > start {
			lines[i] = strings.TrimRight(line, " \t")
		}
		off = lineEnd + 1 // +1 for \n
	}
	return strings.Join(lines, "\n")
}
