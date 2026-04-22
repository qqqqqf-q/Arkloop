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

	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/fileops"
)

const (
	defaultLimit = 200
	maxLimit     = 1000
)

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

	contextLinesRaw, hasContextLines := args["context_lines"].(float64)
	contextLines := int(contextLinesRaw)
	if contextLines < 0 {
		contextLines = 0
	}
	if contextLines > 10 {
		contextLines = 10
	}

	outputMode, _ := args["output_mode"].(string)
	if outputMode == "" {
		outputMode = "files_with_matches"
	}

	limitRaw, _ := args["limit"].(float64)
	limit := int(limitRaw)
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	offsetRaw, _ := args["offset"].(float64)
	offset := int(offsetRaw)
	if offset < 0 {
		offset = 0
	}

	backend := fileops.ResolveBackend(execCtx.RuntimeSnapshot, execCtx.WorkDir, execCtx.RunID.String(), resolveAccountID(execCtx), execCtx.ProfileRef, execCtx.WorkspaceRef)

	switch outputMode {
	case "files_with_matches":
		return executeFilesWithMatches(ctx, backend, pattern, searchPath, include, limit, offset, started)
	case "count":
		return executeCount(ctx, backend, pattern, searchPath, include, limit, offset, started)
	case "content":
		return executeContent(ctx, backend, pattern, searchPath, include, contextLines, hasContextLines, limit, offset, started)
	default:
		return errResult(fmt.Sprintf("unknown output_mode: %s", outputMode), started)
	}
}

// executeFilesWithMatches returns file paths sorted by mtime (newest first).
func executeFilesWithMatches(ctx context.Context, backend fileops.Backend, pattern, searchPath, include string, limit, offset int, started time.Time) tools.ExecutionResult {
	// use rg -l for efficiency
	cmd := fmt.Sprintf("rg -l --sortr=modified %s", shellQuote(pattern))
	if include != "" {
		cmd += fmt.Sprintf(" -g %s", shellQuote(include))
	}
	if searchPath != "" {
		cmd += " " + shellQuote(searchPath)
	}

	stdout, _, exitCode, err := backend.Exec(ctx, cmd)
	if err != nil {
		// fallback to structured search then extract unique files
		return executeFilesWithMatchesFallback(ctx, backend, pattern, searchPath, include, limit, offset, started)
	}
	if exitCode == 1 {
		return tools.ExecutionResult{
			ResultJSON: map[string]any{"matches": "", "count": 0, "truncated": false},
			DurationMs: durationMs(started),
		}
	}
	if exitCode != 0 && stdout == "" {
		// rg --sortr may not be supported, fallback
		return executeFilesWithMatchesFallback(ctx, backend, pattern, searchPath, include, limit, offset, started)
	}

	lines := splitNonEmpty(stdout)
	total := len(lines)

	// apply pagination
	if offset >= len(lines) {
		lines = nil
	} else {
		lines = lines[offset:]
	}
	truncated := len(lines) > limit
	if truncated {
		lines = lines[:limit]
	}

	result := strings.Join(lines, "\n")
	return paginatedResult(result, len(lines), truncated, total, limit, offset, started)
}

func executeFilesWithMatchesFallback(ctx context.Context, backend fileops.Backend, pattern, searchPath, include string, limit, offset int, started time.Time) tools.ExecutionResult {
	matches, _, err := searchFilesStructured(ctx, backend, pattern, searchPath, include, maxLimit)
	if err != nil {
		return errResult(fmt.Sprintf("grep failed: %s", err.Error()), started)
	}

	// deduplicate files, preserve mtime order
	seen := map[string]struct{}{}
	var files []string
	for _, m := range matches {
		if _, ok := seen[m.file]; !ok {
			seen[m.file] = struct{}{}
			files = append(files, m.file)
		}
	}

	total := len(files)
	if offset >= len(files) {
		files = nil
	} else {
		files = files[offset:]
	}
	truncated := len(files) > limit
	if truncated {
		files = files[:limit]
	}

	result := strings.Join(files, "\n")
	return paginatedResult(result, len(files), truncated, total, limit, offset, started)
}

// executeCount returns match counts per file.
func executeCount(ctx context.Context, backend fileops.Backend, pattern, searchPath, include string, limit, offset int, started time.Time) tools.ExecutionResult {
	cmd := fmt.Sprintf("rg -c --sortr=modified %s", shellQuote(pattern))
	if include != "" {
		cmd += fmt.Sprintf(" -g %s", shellQuote(include))
	}
	if searchPath != "" {
		cmd += " " + shellQuote(searchPath)
	}

	stdout, _, exitCode, err := backend.Exec(ctx, cmd)
	if err != nil {
		return executeCountFallback(ctx, backend, pattern, searchPath, include, limit, offset, started)
	}
	if exitCode == 1 {
		return tools.ExecutionResult{
			ResultJSON: map[string]any{"matches": "", "count": 0, "truncated": false},
			DurationMs: durationMs(started),
		}
	}
	if exitCode != 0 && stdout == "" {
		return executeCountFallback(ctx, backend, pattern, searchPath, include, limit, offset, started)
	}

	return buildCountResult(splitNonEmpty(stdout), limit, offset, started)
}

func executeCountFallback(ctx context.Context, backend fileops.Backend, pattern, searchPath, include string, limit, offset int, started time.Time) tools.ExecutionResult {
	matches, _, err := searchFilesStructured(ctx, backend, pattern, searchPath, include, maxLimit)
	if err != nil {
		return errResult(fmt.Sprintf("grep count failed: %s", err.Error()), started)
	}

	// aggregate counts per file, preserve mtime order
	type fileCount struct {
		file  string
		count int
	}
	seen := map[string]*fileCount{}
	var ordered []*fileCount
	for _, m := range matches {
		fc, ok := seen[m.file]
		if !ok {
			fc = &fileCount{file: m.file}
			seen[m.file] = fc
			ordered = append(ordered, fc)
		}
		fc.count++
	}

	lines := make([]string, len(ordered))
	for i, fc := range ordered {
		lines[i] = fmt.Sprintf("%s:%d", fc.file, fc.count)
	}
	return buildCountResult(lines, limit, offset, started)
}

func buildCountResult(lines []string, limit, offset int, started time.Time) tools.ExecutionResult {
	total := len(lines)
	if offset >= len(lines) {
		lines = nil
	} else {
		lines = lines[offset:]
	}
	truncated := len(lines) > limit
	if truncated {
		lines = lines[:limit]
	}

	totalMatches := 0
	for _, line := range lines {
		if idx := strings.LastIndex(line, ":"); idx >= 0 {
			if n, e := strconv.Atoi(line[idx+1:]); e == nil {
				totalMatches += n
			}
		}
	}

	result := strings.Join(lines, "\n")
	res := map[string]any{
		"matches":       result,
		"count":         len(lines),
		"total_matches": totalMatches,
		"truncated":     truncated,
		"total":         total,
		"offset":        offset,
		"limit":         limit,
	}
	if truncated {
		nextOffset := offset + limit
		res["pagination_hint"] = fmt.Sprintf("[搜索结果] 显示第 %d-%d 条，共约 %d 条。使用 offset=%d 获取更多", offset+1, offset+len(lines), total, nextOffset)
	}
	return tools.ExecutionResult{
		ResultJSON: res,
		DurationMs: durationMs(started),
	}
}

// executeContent returns matching lines with optional context.
func executeContent(ctx context.Context, backend fileops.Backend, pattern, searchPath, include string, contextLines int, hasContextLines bool, limit, offset int, started time.Time) tools.ExecutionResult {
	// first do a quick structured search to count matches for auto-context
	if !hasContextLines {
		quickMatches, _, _ := searchFilesStructured(ctx, backend, pattern, searchPath, include, 20)
		contextLines = autoContextLines(len(quickMatches))
	}

	if contextLines > 0 {
		raw, rErr := searchWithRipgrepRaw(ctx, backend, pattern, searchPath, include, contextLines)
		if rErr == nil {
			return paginateByBlocks(raw, limit, offset, started)
		}

		// fallback to local context search
		localBackend, ok := backend.(*fileops.LocalBackend)
		if !ok {
			return errResult(fmt.Sprintf("grep failed: %s", rErr.Error()), started)
		}
		if _, resolveErr := localBackend.ResolvePath(searchPath); resolveErr != nil {
			return errResult(fmt.Sprintf("grep failed: %s", resolveErr.Error()), started)
		}
		raw, rErr = searchWithContextFallback(localBackend.NormalizePath(searchPath), pattern, include, contextLines)
		if rErr != nil {
			return errResult(fmt.Sprintf("grep failed: %s", rErr.Error()), started)
		}
		return paginateByBlocks(raw, limit, offset, started)
	}

	// contextLines == 0: structured path
	matches, truncated, sErr := searchFilesStructured(ctx, backend, pattern, searchPath, include, limit+offset)
	if sErr != nil {
		return errResult(fmt.Sprintf("grep failed: %s", sErr.Error()), started)
	}

	total := len(matches)
	if offset >= len(matches) {
		matches = nil
	} else {
		matches = matches[offset:]
	}
	if len(matches) > limit {
		truncated = true
		matches = matches[:limit]
	}

	lines := make([]string, 0, len(matches))
	for _, m := range matches {
		lines = append(lines, fmt.Sprintf("%s:%d:%s", m.file, m.line, m.text))
	}
	result := strings.Join(lines, "\n")
	return paginatedResult(result, len(matches), truncated, total, limit, offset, started)
}

// autoContextLines determines context lines based on match count (Gemini-style).
// 1 match → 30 lines, 2-3 → 10, 4-10 → 3, >10 → 0.
func autoContextLines(matchCount int) int {
	switch {
	case matchCount == 1:
		return 30
	case matchCount <= 3:
		return 10
	case matchCount <= 10:
		return 3
	default:
		return 0
	}
}

// paginateByBlocks splits ripgrep context output by "--" separator into match blocks,
// then paginates by block count (not line count).
func paginateByBlocks(raw string, limit, offset int, started time.Time) tools.ExecutionResult {
	raw = strings.TrimRight(raw, "\n")
	if raw == "" {
		return paginatedResult("", 0, false, 0, limit, offset, started)
	}

	blocks := strings.Split(raw, "\n--\n")
	total := len(blocks)

	if offset >= total {
		return paginatedResult("", 0, false, total, limit, offset, started)
	}
	blocks = blocks[offset:]
	truncated := len(blocks) > limit
	if truncated {
		blocks = blocks[:limit]
	}

	result := strings.Join(blocks, "\n--\n")
	return paginatedResult(result, len(blocks), truncated, total, limit, offset, started)
}

func paginatedResult(matchStr string, count int, truncated bool, total, limit, offset int, started time.Time) tools.ExecutionResult {
	result := map[string]any{
		"matches":   matchStr,
		"count":     count,
		"truncated": truncated,
	}
	if truncated || offset > 0 {
		result["total"] = total
		result["offset"] = offset
		result["limit"] = limit
	}
	if truncated {
		nextOffset := offset + limit
		result["pagination_hint"] = fmt.Sprintf("[搜索结果] 显示第 %d-%d 条，共约 %d 条。使用 offset=%d 获取更多", offset+1, offset+count, total, nextOffset)
	}
	return tools.ExecutionResult{
		ResultJSON: result,
		DurationMs: durationMs(started),
	}
}

func splitNonEmpty(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func searchFilesStructured(ctx context.Context, backend fileops.Backend, pattern, searchPath, include string, maxMatches int) ([]grepMatch, bool, error) {
	m, err := searchWithRipgrep(ctx, backend, pattern, searchPath, include)
	if err == nil {
		// sort by mtime desc
		sortMatchesByMtime(m)
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
	if _, resolveErr := localBackend.ResolvePath(searchPath); resolveErr != nil {
		return nil, false, resolveErr
	}
	return searchWithRegex(localBackend.NormalizePath(searchPath), pattern, include, maxMatches)
}

func sortMatchesByMtime(matches []grepMatch) {
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].modTime.After(matches[j].modTime)
	})
}

func searchWithRipgrep(ctx context.Context, backend fileops.Backend, pattern, searchPath, include string) ([]grepMatch, error) {
	cmd := fmt.Sprintf("rg -H -n -a --max-count 1000 --sortr=modified %s", shellQuote(pattern))
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
	if exitCode == 1 {
		return []grepMatch{}, nil
	}
	if exitCode != 0 && stdout == "" {
		return nil, fmt.Errorf("rg exited %d", exitCode)
	}

	// cache mtime per file
	mtimeCache := map[string]time.Time{}
	statFile := func(f string) time.Time {
		if t, ok := mtimeCache[f]; ok {
			return t
		}
		info, err := os.Stat(f)
		if err != nil {
			mtimeCache[f] = time.Time{}
			return time.Time{}
		}
		mtimeCache[f] = info.ModTime()
		return info.ModTime()
	}

	var matches []grepMatch
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		m, ok := parseRipgrepLine(line)
		if ok {
			m.modTime = statFile(m.file)
			matches = append(matches, m)
		}
	}
	return matches, nil
}

func searchWithRipgrepRaw(ctx context.Context, backend fileops.Backend, pattern, searchPath, include string, contextLines int) (string, error) {
	cmd := fmt.Sprintf("rg -H -n -a --max-count 1000 --sortr=modified -C %d %s", contextLines, shellQuote(pattern))
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

func searchWithContextFallback(root, pattern, include string, contextLines int) (string, error) {
	if root == "" {
		root = "."
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}

	var includeRe *regexp.Regexp
	if include != "" {
		includeRe = globToRegex(include)
	}

	type fileResult struct {
		output string
		mtime  time.Time
	}
	var results []fileResult

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
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		if includeRe != nil && !includeRe.MatchString(filepath.Base(rel)) {
			return nil
		}

		fileOutput := buildContextOutput(path, filepath.ToSlash(filepath.Clean(rel)), re, contextLines)
		if fileOutput == "" {
			return nil
		}
		results = append(results, fileResult{output: fileOutput, mtime: info.ModTime()})
		return nil
	})
	if walkErr != nil && walkErr != filepath.SkipAll {
		return "", walkErr
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].mtime.After(results[j].mtime)
	})

	var sb strings.Builder
	for i, r := range results {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(r.output)
	}

	return sb.String(), nil
}

// buildContextOutput reads a file and returns context-aware grep output.
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

	type interval struct{ start, end int }
	var intervals []interval
	matchSet := map[int]bool{}
	for i, line := range allLines {
		if re.MatchString(line) {
			matchSet[i] = true
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

	// merge overlapping/adjacent intervals
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

func searchWithRegex(root, pattern, include string, maxMatches int) ([]grepMatch, bool, error) {
	if root == "" {
		root = "."
	}
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
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		if includeRe != nil && !includeRe.MatchString(filepath.Base(rel)) {
			return nil
		}

		fileMatches := searchInFile(path, filepath.ToSlash(filepath.Clean(rel)), re, info.ModTime())
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
