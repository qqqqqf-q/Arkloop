package edit

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Error codes returned by the edit tool. Each maps to a specific failure mode
// with actionable guidance for the agent.
const (
	ErrCodeNoOp       = "EDIT_NO_OP"        // old_string == new_string
	ErrCodeNotFound   = "EDIT_NOT_FOUND"     // old_string not found in file
	ErrCodeAmbiguous  = "EDIT_AMBIGUOUS"     // old_string matches multiple locations
	ErrCodeFileExists = "EDIT_FILE_EXISTS"   // create mode but file exists
	ErrCodeNotRead    = "EDIT_NOT_READ"      // file not read before edit
	ErrCodeStale      = "EDIT_STALE"         // file modified since last read
	ErrCodeTooLarge   = "EDIT_TOO_LARGE"     // file exceeds size limit
	ErrCodeFileNotFnd = "EDIT_FILE_NOT_FOUND" // file does not exist
	ErrCodeOmission   = "EDIT_OMISSION_DETECTED" // placeholder in new_string
)

const maxEditFileSize = 5 * 1024 * 1024 // 5 MB

type editError struct {
	Code    string
	Message string
	Hint    string
}

func (e *editError) Error() string {
	if e.Hint != "" {
		return fmt.Sprintf("[%s] %s\n%s", e.Code, e.Message, e.Hint)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func errNoOp(filePath string) *editError {
	return &editError{
		Code:    ErrCodeNoOp,
		Message: fmt.Sprintf("old_string and new_string are identical in %s", filePath),
		Hint:    "No changes needed. If you intended a different edit, verify old_string and new_string differ.",
	}
}

func errFileNotFound(filePath string) *editError {
	return &editError{
		Code:    ErrCodeFileNotFnd,
		Message: fmt.Sprintf("file not found: %s", filePath),
		Hint:    "Check the file path. To create a new file, set old_string to empty.",
	}
}

func errFileExists(filePath string) *editError {
	return &editError{
		Code:    ErrCodeFileExists,
		Message: fmt.Sprintf("file already exists: %s", filePath),
		Hint:    "Use old_string to make targeted edits instead of creating.",
	}
}

func errNotRead(filePath string) *editError {
	return &editError{
		Code:    ErrCodeNotRead,
		Message: fmt.Sprintf("file not read before editing: %s", filePath),
		Hint:    "Call read with source.kind=file_path first, then retry the edit.",
	}
}

func errStale(filePath string) *editError {
	return &editError{
		Code:    ErrCodeStale,
		Message: fmt.Sprintf("file modified since last read: %s", filePath),
		Hint:    "Re-read the file to get the latest content, then retry.",
	}
}

func errTooLarge(filePath string, size int64) *editError {
	return &editError{
		Code:    ErrCodeTooLarge,
		Message: fmt.Sprintf("file too large: %s (%d bytes, max %d)", filePath, size, maxEditFileSize),
		Hint:    "Use exec_command with sed/awk for files larger than 5 MB.",
	}
}

func errAmbiguous(filePath string, count int) *editError {
	return &editError{
		Code:    ErrCodeAmbiguous,
		Message: fmt.Sprintf("old_string matches %d locations in %s", count, filePath),
		Hint:    "Include more surrounding context (3-5 lines before/after) to make old_string unique, or set replace_all=true to replace all occurrences.",
	}
}

// snippetMatch holds a closest-match result with line location.
type snippetMatch struct {
	startLine int    // 1-based inclusive
	endLine   int    // 1-based inclusive
	text      string // snippet with line numbers
}

// errNotFound builds EDIT_NOT_FOUND with structured guidance for self-correction.
func errNotFound(filePath, content, oldString string) *editError {
	totalLines := strings.Count(content, "\n") + 1
	m := findClosestMatch(content, oldString, 4)
	if m == nil {
		return &editError{
			Code:    ErrCodeNotFound,
			Message: fmt.Sprintf("old_string not found in %s", filePath),
			Hint: fmt.Sprintf(
				"No close match found. The file has %d lines. Re-read the file to verify the content you want to edit.",
				totalLines,
			),
		}
	}
	return &editError{
		Code:    ErrCodeNotFound,
		Message: fmt.Sprintf("old_string not found in %s", filePath),
		Hint: fmt.Sprintf(
			"The closest match was found at lines %d-%d. Re-read those lines and adjust your old_string.\n\n%s",
			m.startLine, m.endLine, m.text,
		),
	}
}

// findClosestMatch finds the region with the smallest edit distance to needle
// and returns line range + snippet with ±contextLines of surrounding context.
func findClosestMatch(content, needle string, contextLines int) *snippetMatch {
	if content == "" || needle == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	needleLines := strings.Split(needle, "\n")
	needleLen := len(needleLines)
	if needleLen == 0 || len(lines) == 0 {
		return nil
	}

	// limit search for performance
	if len(lines) > 10000 || needleLen > 500 {
		return nil
	}

	bestDist := -1
	bestStart := 0
	trimNeedle := trimAllLines(needleLines)

	for i := 0; i <= len(lines)-needleLen; i++ {
		window := trimAllLines(lines[i : i+needleLen])
		d := levenshteinLines(window, trimNeedle)
		if bestDist < 0 || d < bestDist {
			bestDist = d
			bestStart = i
			if d == 0 {
				break
			}
		}
	}

	// only show if reasonably close (< 40% of needle length)
	if bestDist < 0 || bestDist > utf8.RuneCountInString(strings.Join(needleLines, "\n"))*4/10 {
		return nil
	}

	matchStart := bestStart            // 0-based
	matchEnd := bestStart + needleLen - 1 // 0-based inclusive

	start := matchStart - contextLines
	if start < 0 {
		start = 0
	}
	end := matchEnd + contextLines + 1
	if end > len(lines) {
		end = len(lines)
	}

	var sb strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&sb, "%6d|%s\n", i+1, lines[i])
	}
	return &snippetMatch{
		startLine: matchStart + 1,
		endLine:   matchEnd + 1,
		text:      sb.String(),
	}
}

func trimAllLines(lines []string) string {
	parts := make([]string, len(lines))
	for i, l := range lines {
		parts[i] = strings.TrimSpace(l)
	}
	return strings.Join(parts, "\n")
}

// levenshteinLines computes a simple edit distance between two multiline strings.
func levenshteinLines(a, b string) int {
	if a == b {
		return 0
	}
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)

	// cap computation for large strings
	if la*lb > 4_000_000 {
		diff := la - lb
		if diff < 0 {
			diff = -diff
		}
		return diff
	}

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			curr[j] = min3(del, ins, sub)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
