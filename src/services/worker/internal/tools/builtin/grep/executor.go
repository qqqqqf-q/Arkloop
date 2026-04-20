package grep

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"arkloop/services/shared/objectstore"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/fileops"
)

const maxMatches = 200

// skipDirs are directory names skipped during the regex fallback walk.
var skipDirs = map[string]struct{}{
	".git":         {},
	"node_modules": {},
	"__pycache__":  {},
	".venv":        {},
	"vendor":       {},
	".idea":        {},
	".vscode":      {},
	".next":        {},
	".cache":       {},
}

type grepMatch struct {
	file    string
	line    int
	text    string
	modTime time.Time
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
	include, _ := args["include"].(string)

	contextLinesRaw, _ := args["context_lines"].(float64)
	contextLines := int(contextLinesRaw)
	if contextLines < 0 {
		contextLines = 0
	}
	if contextLines > 10 {
		contextLines = 10
	}

	backend := fileops.ResolveBackend(execCtx.RuntimeSnapshot, execCtx.WorkDir, execCtx.RunID.String(), tools.ToolOutputScopeID(execCtx.ThreadID, execCtx.RunID), resolveAccountID(execCtx), execCtx.ProfileRef, execCtx.WorkspaceRef, execCtx.ToolOutputStore)

	matches, rawOutput, truncated, err := searchFiles(ctx, backend, pattern, searchPath, include, contextLines, tools.ToolOutputScopeID(execCtx.ThreadID, execCtx.RunID))
	if err != nil {
		return errResult(fmt.Sprintf("grep failed: %s", err.Error()), started)
	}

	var matchStr string
	var count int
	if rawOutput != "" {
		matchStr = rawOutput
		for _, line := range strings.Split(rawOutput, "\n") {
			if strings.Contains(line, ":") && !strings.HasPrefix(line, "--") {
				count++
			}
		}
	} else {
		lines := make([]string, 0, len(matches))
		for _, m := range matches {
			lines = append(lines, fmt.Sprintf("%s:%d:%s", m.file, m.line, m.text))
		}
		matchStr = strings.Join(lines, "\n")
		count = len(matches)
	}

	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"matches":   matchStr,
			"count":     count,
			"truncated": truncated,
		},
		DurationMs: durationMs(started),
	}
}

func searchFiles(ctx context.Context, backend fileops.Backend, pattern, searchPath, include string, contextLines int, toolOutputScopeID string) (matches []grepMatch, rawOutput string, truncated bool, err error) {
	if localBackend, ok := backend.(*fileops.LocalBackend); ok {
		if objectPrefix, displayRoot, scoped, err := fileops.ResolveScopedToolOutputSearch(searchPath, toolOutputScopeID, localBackend.ToolOutputStore); scoped {
			if err != nil {
				return nil, "", false, err
			}
			if contextLines > 0 {
				raw, rErr := searchToolOutputObjectsRaw(ctx, localBackend.ToolOutputStore, objectPrefix, displayRoot, pattern, include, contextLines)
				return nil, raw, false, rErr
			}
			m, trunc, sErr := searchToolOutputObjects(ctx, localBackend.ToolOutputStore, objectPrefix, displayRoot, pattern, include)
			return m, "", trunc, sErr
		}
	}
	if sandboxBackend, ok := backend.(*fileops.SandboxExecBackend); ok {
		if objectPrefix, displayRoot, scoped, err := fileops.ResolveScopedToolOutputSearch(searchPath, toolOutputScopeID, sandboxBackend.ToolOutputStore()); scoped {
			if err != nil {
				return nil, "", false, err
			}
			if contextLines > 0 {
				raw, rErr := searchToolOutputObjectsRaw(ctx, sandboxBackend.ToolOutputStore(), objectPrefix, displayRoot, pattern, include, contextLines)
				return nil, raw, false, rErr
			}
			m, trunc, sErr := searchToolOutputObjects(ctx, sandboxBackend.ToolOutputStore(), objectPrefix, displayRoot, pattern, include)
			return m, "", trunc, sErr
		}
	}
	if contextLines > 0 {
		raw, rErr := searchWithRipgrepRaw(ctx, backend, pattern, searchPath, include, contextLines)
		if rErr == nil {
			return nil, raw, false, nil
		}
		localBackend, ok := backend.(*fileops.LocalBackend)
		if !ok {
			return nil, "", false, rErr
		}
		raw, rErr = searchWithContextFallback(localBackend.NormalizePath(searchPath), filepath.ToSlash(filepath.Clean(searchPath)), pattern, include, contextLines)
		if rErr != nil {
			return nil, "", false, rErr
		}
		return nil, raw, false, nil
	}

	// contextLines == 0: original structured path
	m, trunc, sErr := searchFilesStructured(ctx, backend, pattern, searchPath, include)
	return m, "", trunc, sErr
}

func searchToolOutputObjects(
	ctx context.Context,
	store objectstore.Store,
	objectPrefix string,
	displayRoot string,
	pattern string,
	include string,
) ([]grepMatch, bool, error) {
	if store == nil {
		return nil, false, fmt.Errorf("tool output store is unavailable")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, false, fmt.Errorf("invalid regex: %w", err)
	}
	var includeRe *regexp.Regexp
	if include != "" {
		includeRe = globToRegex(include)
	}
	objects, err := store.ListPrefix(ctx, objectPrefix)
	if err != nil {
		return nil, false, err
	}
	displayRoot = normalizeDisplayRoot(displayRoot)
	var matches []grepMatch
	for _, item := range objects {
		displayPath, ok := fileops.ToolOutputDisplayPathFromObjectKey(item.Key)
		if !ok {
			continue
		}
		if displayRoot != "" && !strings.HasPrefix(displayPath, displayRoot+"/") && displayPath != displayRoot {
			continue
		}
		if includeRe != nil && !includeRe.MatchString(filepath.Base(displayPath)) {
			continue
		}
		data, getErr := store.Get(ctx, item.Key)
		if getErr != nil {
			return nil, false, getErr
		}
		modTime := time.Time{}
		if raw := strings.TrimSpace(item.Metadata["updated_at"]); raw != "" {
			if parsed, parseErr := time.Parse(time.RFC3339Nano, raw); parseErr == nil {
				modTime = parsed
			}
		}
		fileMatches := searchInBytes(data, displayPath, re, modTime)
		matches = append(matches, fileMatches...)
		if len(matches) > maxMatches*2 {
			break
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].modTime.After(matches[j].modTime)
	})
	truncated := len(matches) > maxMatches
	if truncated {
		matches = matches[:maxMatches]
	}
	return matches, truncated, nil
}

func searchToolOutputObjectsRaw(
	ctx context.Context,
	store objectstore.Store,
	objectPrefix string,
	displayRoot string,
	pattern string,
	include string,
	contextLines int,
) (string, error) {
	if store == nil {
		return "", fmt.Errorf("tool output store is unavailable")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}
	var includeRe *regexp.Regexp
	if include != "" {
		includeRe = globToRegex(include)
	}
	objects, err := store.ListPrefix(ctx, objectPrefix)
	if err != nil {
		return "", err
	}
	displayRoot = normalizeDisplayRoot(displayRoot)
	var sb strings.Builder
	first := true
	for _, item := range objects {
		displayPath, ok := fileops.ToolOutputDisplayPathFromObjectKey(item.Key)
		if !ok {
			continue
		}
		if displayRoot != "" && !strings.HasPrefix(displayPath, displayRoot+"/") && displayPath != displayRoot {
			continue
		}
		if includeRe != nil && !includeRe.MatchString(filepath.Base(displayPath)) {
			continue
		}
		data, getErr := store.Get(ctx, item.Key)
		if getErr != nil {
			return "", getErr
		}
		fileOutput := buildContextOutputFromBytes(data, displayPath, re, contextLines)
		if fileOutput == "" {
			continue
		}
		if !first {
			sb.WriteString("\n")
		}
		first = false
		sb.WriteString(fileOutput)
	}
	return sb.String(), nil
}

func searchFilesStructured(ctx context.Context, backend fileops.Backend, pattern, searchPath, include string) ([]grepMatch, bool, error) {
	m, err := searchWithRipgrep(ctx, backend, pattern, searchPath, include)
	if err == nil {
		truncated := len(m) > maxMatches
		if truncated {
			m = m[:maxMatches]
		}
		return m, truncated, nil
	}
	localBackend, ok := backend.(*fileops.LocalBackend)
	if !ok {
		return nil, false, err
	}
	return searchWithRegex(localBackend.NormalizePath(searchPath), filepath.ToSlash(filepath.Clean(searchPath)), pattern, include)
}

func searchWithRipgrep(ctx context.Context, backend fileops.Backend, pattern, searchPath, include string) ([]grepMatch, error) {
	cmd := fmt.Sprintf("rg -H -n --max-count 1000 %s", shellQuote(pattern))
	if include != "" {
		cmd += fmt.Sprintf(" -g %s", shellQuote(include))
	}
	if searchPath != "" {
		cmd += " " + shellQuote(searchPath)
	}
	stdout, _, exitCode, err := backend.Exec(ctx, cmd)
	if err != nil {
		return nil, err
	}
	// rg exits 1 when no matches are found — not an error
	if exitCode == 1 {
		return []grepMatch{}, nil
	}
	if exitCode != 0 && stdout == "" {
		return nil, fmt.Errorf("rg exited %d", exitCode)
	}

	var matches []grepMatch
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		m, ok := parseRipgrepLine(line)
		if ok {
			matches = append(matches, m)
		}
	}
	return matches, nil
}

func searchWithRipgrepRaw(ctx context.Context, backend fileops.Backend, pattern, searchPath, include string, contextLines int) (string, error) {
	cmd := fmt.Sprintf("rg -H -n --max-count 1000 -C %d %s", contextLines, shellQuote(pattern))
	if include != "" {
		cmd += fmt.Sprintf(" -g %s", shellQuote(include))
	}
	if searchPath != "" {
		cmd += " " + shellQuote(searchPath)
	}
	stdout, _, exitCode, err := backend.Exec(ctx, cmd)
	if err != nil {
		return "", err
	}
	if exitCode == 1 {
		return "", nil
	}
	if exitCode != 0 && stdout == "" {
		return "", fmt.Errorf("rg exited %d", exitCode)
	}
	return stdout, nil
}

func searchWithContextFallback(root, displayRoot, pattern, include string, contextLines int) (string, error) {
	if root == "" {
		root = "."
	}
	displayRoot = normalizeDisplayRoot(displayRoot)
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}

	var includeRe *regexp.Regexp
	if include != "" {
		includeRe = globToRegex(include)
	}

	var sb strings.Builder
	firstFile := true

	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
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
		if info.Size() > 1024*1024 {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		if includeRe != nil && !includeRe.MatchString(filepath.Base(rel)) {
			return nil
		}

		displayPath := displayRelPath(displayRoot, rel)
		fileOutput := buildContextOutput(path, displayPath, re, contextLines)
		if fileOutput == "" {
			return nil
		}
		if !firstFile {
			sb.WriteString("\n")
		}
		firstFile = false
		sb.WriteString(fileOutput)
		return nil
	})
	if walkErr != nil && walkErr != filepath.SkipAll {
		return "", walkErr
	}

	return sb.String(), nil
}

// buildContextOutput reads a file and returns context-aware grep output for it.
// Match lines use "file:line:text", context lines use "file-line-text", blocks separated by "--".
func buildContextOutput(path string, displayPath string, re *regexp.Regexp, contextLines int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	if strings.TrimSpace(displayPath) == "" {
		displayPath = filepath.ToSlash(path)
	}

	var allLines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		allLines = append(allLines, scanner.Text())
	}

	// find matching line indices (0-based)
	type interval struct{ start, end int }
	var intervals []interval
	for i, line := range allLines {
		if re.MatchString(line) {
			s := i - contextLines
			if s < 0 {
				s = 0
			}
			e := i + contextLines
			if e >= len(allLines) {
				e = len(allLines) - 1
			}
			intervals = append(intervals, interval{s, e})
		}
	}
	if len(intervals) == 0 {
		return ""
	}

	// merge overlapping intervals
	merged := []interval{intervals[0]}
	for _, iv := range intervals[1:] {
		last := &merged[len(merged)-1]
		if iv.start <= last.end+1 {
			if iv.end > last.end {
				last.end = iv.end
			}
		} else {
			merged = append(merged, iv)
		}
	}

	// build output for matched line sets
	matchSet := map[int]bool{}
	for _, iv := range intervals {
		for i := iv.start; i <= iv.end; i++ {
			if re.MatchString(allLines[i]) {
				matchSet[i] = true
			}
		}
	}

	var sb strings.Builder
	firstBlock := true
	for _, iv := range merged {
		if !firstBlock {
			sb.WriteString("--\n")
		}
		firstBlock = false
		for i := iv.start; i <= iv.end; i++ {
			lineNum := i + 1
			if matchSet[i] {
				sb.WriteString(fmt.Sprintf("%s:%d:%s\n", displayPath, lineNum, allLines[i]))
			} else {
				sb.WriteString(fmt.Sprintf("%s-%d-%s\n", displayPath, lineNum, allLines[i]))
			}
		}
	}

	return sb.String()
}

func parseRipgrepLine(line string) (grepMatch, bool) {
	// file:line:content
	first := strings.Index(line, ":")
	if first < 0 {
		return grepMatch{}, false
	}
	rest := line[first+1:]
	second := strings.Index(rest, ":")
	if second < 0 {
		return grepMatch{}, false
	}
	file := line[:first]
	lineNum, err := strconv.Atoi(rest[:second])
	if err != nil {
		return grepMatch{}, false
	}
	text := rest[second+1:]
	return grepMatch{file: file, line: lineNum, text: text}, true
}

func searchWithRegex(root, displayRoot, pattern, include string) ([]grepMatch, bool, error) {
	if root == "" {
		root = "."
	}
	displayRoot = normalizeDisplayRoot(displayRoot)
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, false, fmt.Errorf("invalid regex: %w", err)
	}

	var includeRe *regexp.Regexp
	if include != "" {
		includeRe = globToRegex(include)
	}

	var matches []grepMatch
	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
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
		if info.Size() > 1024*1024 {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		if includeRe != nil && !includeRe.MatchString(filepath.Base(rel)) {
			return nil
		}

		fileMatches := searchInFile(path, displayRelPath(displayRoot, rel), re, info.ModTime())
		matches = append(matches, fileMatches...)
		if len(matches) > maxMatches*2 {
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil && walkErr != filepath.SkipAll {
		return nil, false, walkErr
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].modTime.After(matches[j].modTime)
	})
	truncated := len(matches) > maxMatches
	if truncated {
		matches = matches[:maxMatches]
	}
	return matches, truncated, nil
}

func normalizeDisplayRoot(displayRoot string) string {
	displayRoot = strings.TrimSpace(filepath.ToSlash(filepath.Clean(displayRoot)))
	if displayRoot == "" || displayRoot == "." {
		return ""
	}
	return displayRoot
}

func displayRelPath(displayRoot, rel string) string {
	rel = filepath.ToSlash(filepath.Clean(rel))
	if displayRoot == "" {
		return rel
	}
	return filepath.ToSlash(filepath.Join(displayRoot, rel))
}

func searchInFile(path string, displayPath string, re *regexp.Regexp, modTime time.Time) []grepMatch {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	if strings.TrimSpace(displayPath) == "" {
		displayPath = filepath.ToSlash(path)
	}

	var matches []grepMatch
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		text := scanner.Text()
		if re.MatchString(text) {
			matches = append(matches, grepMatch{
				file:    displayPath,
				line:    lineNum,
				text:    text,
				modTime: modTime,
			})
			if len(matches) >= 20 {
				break
			}
		}
	}
	return matches
}

func searchInBytes(data []byte, displayPath string, re *regexp.Regexp, modTime time.Time) []grepMatch {
	if strings.TrimSpace(displayPath) == "" {
		return nil
	}
	var matches []grepMatch
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		text := scanner.Text()
		if re.MatchString(text) {
			matches = append(matches, grepMatch{
				file:    displayPath,
				line:    lineNum,
				text:    text,
				modTime: modTime,
			})
			if len(matches) >= 20 {
				break
			}
		}
	}
	return matches
}

func buildContextOutputFromBytes(data []byte, displayPath string, re *regexp.Regexp, contextLines int) string {
	if strings.TrimSpace(displayPath) == "" {
		return ""
	}
	var allLines []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		allLines = append(allLines, scanner.Text())
	}
	type interval struct{ start, end int }
	var intervals []interval
	for i, line := range allLines {
		if re.MatchString(line) {
			s := i - contextLines
			if s < 0 {
				s = 0
			}
			e := i + contextLines
			if e >= len(allLines) {
				e = len(allLines) - 1
			}
			intervals = append(intervals, interval{s, e})
		}
	}
	if len(intervals) == 0 {
		return ""
	}
	merged := []interval{intervals[0]}
	for _, iv := range intervals[1:] {
		last := &merged[len(merged)-1]
		if iv.start <= last.end+1 {
			if iv.end > last.end {
				last.end = iv.end
			}
		} else {
			merged = append(merged, iv)
		}
	}
	matchSet := map[int]bool{}
	for _, iv := range intervals {
		for i := iv.start; i <= iv.end; i++ {
			if re.MatchString(allLines[i]) {
				matchSet[i] = true
			}
		}
	}
	var sb strings.Builder
	firstBlock := true
	for _, iv := range merged {
		if !firstBlock {
			sb.WriteString("--\n")
		}
		firstBlock = false
		for i := iv.start; i <= iv.end; i++ {
			lineNum := i + 1
			if matchSet[i] {
				sb.WriteString(fmt.Sprintf("%s:%d:%s\n", displayPath, lineNum, allLines[i]))
			} else {
				sb.WriteString(fmt.Sprintf("%s-%d-%s\n", displayPath, lineNum, allLines[i]))
			}
		}
	}
	return sb.String()
}

func globToRegex(pattern string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		case '.':
			b.WriteString("\\.")
		default:
			b.WriteByte(pattern[i])
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil
	}
	return re
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
