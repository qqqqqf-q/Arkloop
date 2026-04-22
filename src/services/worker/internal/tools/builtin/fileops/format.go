package fileops

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

const (
	MaxReadSize      = 256 * 1024
	DefaultReadLimit = 2000
	MaxLineLength    = 2000
	MaxOutputChars   = 100 * 1024 // 100K character output cap
)

// FormatWithLineNumbers prepends right-aligned 6-char line numbers to each line.
// startLine is 1-based.
func FormatWithLineNumbers(content string, startLine int) string {
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		num := i + startLine
		out = append(out, fmt.Sprintf("%6d|%s", num, line))
	}
	return strings.Join(out, "\n")
}

// TruncateLine cuts a line at maxLen characters, appending "..." if truncated.
func TruncateLine(line string, maxLen int) string {
	if len(line) <= maxLen {
		return line
	}
	s := line[:maxLen]
	for len(s) > 0 && !utf8.RuneStart(s[len(s)-1]) {
		s = s[:len(s)-1]
	}
	if len(s) > 0 && !utf8.ValidString(s[len(s)-1:]) {
		s = s[:len(s)-1]
	}
	return s + "..."
}

// ReadLines extracts a range of lines from raw data.
// offset is 0-based (line index), limit is the max number of lines to return.
// Returns the content string, the total line count of the file, and whether
// the output was truncated (more lines exist beyond offset+limit).
func ReadLines(data []byte, offset, limit int) (content string, totalLines int, truncated bool) {
	all := strings.Split(string(data), "\n")
	totalLines = len(all)
	if offset >= totalLines {
		return "", totalLines, false
	}
	end := offset + limit
	if end > totalLines {
		end = totalLines
	}
	selected := all[offset:end]
	for i, line := range selected {
		selected[i] = TruncateLine(strings.TrimSuffix(line, "\r"), MaxLineLength)
	}
	return strings.Join(selected, "\n"), totalLines, end < totalLines
}

// ReadLinesFromFile streams a file and extracts only the requested line range.
// offset is 0-based line index, limit is max lines to return.
// Returns content, totalLines, truncated, error.
func ReadLinesFromFile(path string, offset, limit int) (string, int, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, false, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var contentLines []string
	totalLines := 0
	end := offset + limit

	for scanner.Scan() {
		line := scanner.Text()
		if totalLines >= offset && totalLines < end {
			contentLines = append(contentLines, TruncateLine(line, MaxLineLength))
		}
		totalLines++
		if totalLines >= end {
			// Continue scanning only to count total lines
			for scanner.Scan() {
				totalLines++
			}
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return "", totalLines, false, err
	}

	return strings.Join(contentLines, "\n"), totalLines, totalLines > end, nil
}

// CountDiffLines counts lines added and removed between old and new content.
func CountDiffLines(oldContent, newContent string) (additions, removals int) {
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")
	oldSet := make(map[string]int, len(oldLines))
	for _, l := range oldLines {
		oldSet[l]++
	}
	newSet := make(map[string]int, len(newLines))
	for _, l := range newLines {
		newSet[l]++
	}
	for l, count := range newSet {
		if oldCount, ok := oldSet[l]; ok {
			if count > oldCount {
				additions += count - oldCount
			}
		} else {
			additions += count
		}
	}
	for l, count := range oldSet {
		if newCount, ok := newSet[l]; ok {
			if count > newCount {
				removals += count - newCount
			}
		} else {
			removals += count
		}
	}
	return
}
