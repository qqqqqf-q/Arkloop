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

	backend := fileops.ResolveBackend(execCtx.RuntimeSnapshot, "", execCtx.RunID.String(), resolveAccountID(execCtx), execCtx.ProfileRef, execCtx.WorkspaceRef)

	matches, truncated, err := searchFiles(ctx, backend, pattern, searchPath, include)
	if err != nil {
		return errResult(fmt.Sprintf("grep failed: %s", err.Error()), started)
	}

	lines := make([]string, 0, len(matches))
	for _, m := range matches {
		lines = append(lines, fmt.Sprintf("%s:%d:%s", m.file, m.line, m.text))
	}

	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"matches":   strings.Join(lines, "\n"),
			"count":     len(matches),
			"truncated": truncated,
		},
		DurationMs: durationMs(started),
	}
}

func searchFiles(ctx context.Context, backend fileops.Backend, pattern, searchPath, include string) ([]grepMatch, bool, error) {
	matches, err := searchWithRipgrep(ctx, backend, pattern, searchPath, include)
	if err == nil {
		truncated := len(matches) > maxMatches
		if truncated {
			matches = matches[:maxMatches]
		}
		return matches, truncated, nil
	}

	return searchWithRegex(searchPath, pattern, include)
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

func searchWithRegex(root, pattern, include string) ([]grepMatch, bool, error) {
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

		fileMatches := searchInFile(path, re, info.ModTime())
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

func searchInFile(path string, re *regexp.Regexp, modTime time.Time) []grepMatch {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var matches []grepMatch
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		text := scanner.Text()
		if re.MatchString(text) {
			matches = append(matches, grepMatch{
				file:    path,
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
