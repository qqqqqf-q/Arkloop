package edit

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// matchResult holds the outcome of a match attempt.
type matchResult struct {
	strategy string // "exact", "normalized", "regex"
	// indices[i] is the byte offset in content where the i-th match starts.
	indices []int
	// For normalized/regex matches, the actual substrings found in the original content.
	actuals []string
}

// match runs 4-layer progressive matching against content for oldString.
// Returns nil if no match at any layer.
func match(content, oldString string) *matchResult {
	// Layer 1: exact match
	if r := matchExact(content, oldString); r != nil {
		return r
	}
	// Layer 2: normalized match (quote normalization + whitespace normalization)
	if r := matchNormalized(content, oldString); r != nil {
		return r
	}
	// Layer 3: regex token match
	if r := matchRegex(content, oldString); r != nil {
		return r
	}
	// Layer 4: fuzzy Levenshtein match
	if r := matchFuzzy(content, oldString); r != nil {
		return r
	}
	return nil
}

// --- Layer 1: Exact ---

func matchExact(content, oldString string) *matchResult {
	indices := allIndices(content, oldString)
	if len(indices) == 0 {
		return nil
	}
	actuals := make([]string, len(indices))
	for i := range indices {
		actuals[i] = oldString
	}
	return &matchResult{strategy: "exact", indices: indices, actuals: actuals}
}

// --- Layer 2: Normalized ---
// - Quote normalization: smart quotes -> straight quotes
// - Whitespace normalization: per-line trim comparison, preserving original indentation

func matchNormalized(content, oldString string) *matchResult {
	// Try quote normalization first
	normContent, mapping := normalizeQuotesWithMapping(content)
	normOld := normalizeQuotes(oldString)

	if normContent != content || normOld != oldString {
		indices := allIndices(normContent, normOld)
		if len(indices) > 0 {
			actuals := make([]string, len(indices))
			origIndices := make([]int, len(indices))
			for i, normIdx := range indices {
				normEnd := normIdx + len(normOld)
				origStart := mapping[normIdx]
				origEnd := mapping[normEnd]
				origIndices[i] = origStart
				actuals[i] = content[origStart:origEnd]
			}
			return &matchResult{strategy: "normalized", indices: origIndices, actuals: actuals}
		}
	}

	// Whitespace normalization: line-by-line trim comparison
	return matchWhitespaceNormalized(content, oldString)
}

func matchWhitespaceNormalized(content, oldString string) *matchResult {
	contentLines := strings.Split(content, "\n")
	oldLines := strings.Split(oldString, "\n")
	if len(oldLines) == 0 {
		return nil
	}

	trimmedOld := make([]string, len(oldLines))
	for i, l := range oldLines {
		trimmedOld[i] = strings.TrimSpace(l)
	}

	var matches []int
	for i := 0; i <= len(contentLines)-len(oldLines); i++ {
		found := true
		for j := 0; j < len(oldLines); j++ {
			if strings.TrimSpace(contentLines[i+j]) != trimmedOld[j] {
				found = false
				break
			}
		}
		if found {
			// calculate byte offset
			offset := 0
			for k := 0; k < i; k++ {
				offset += len(contentLines[k]) + 1 // +1 for \n
			}
			matches = append(matches, offset)
		}
	}

	if len(matches) == 0 {
		return nil
	}

	actuals := make([]string, len(matches))
	for i, idx := range matches {
		// reconstruct the actual matched text from content lines
		lineIdx := countNewlines(content[:idx])
		var parts []string
		for j := 0; j < len(oldLines); j++ {
			if lineIdx+j < len(contentLines) {
				parts = append(parts, contentLines[lineIdx+j])
			}
		}
		actuals[i] = strings.Join(parts, "\n")
	}
	return &matchResult{strategy: "normalized", indices: matches, actuals: actuals}
}

// --- Layer 3: Regex token match ---
// Tokenize by delimiters, insert \s* between tokens, build a multiline regex.

func matchRegex(content, oldString string) *matchResult {
	tokens := tokenize(oldString)
	if len(tokens) == 0 {
		return nil
	}

	// too many tokens → skip to avoid pathological regex
	if len(tokens) > 500 {
		return nil
	}

	escaped := make([]string, len(tokens))
	for i, t := range tokens {
		escaped[i] = regexp.QuoteMeta(t)
	}
	// allow inline whitespace + at most one newline (with indentation) between tokens;
	// \s* would match unlimited newlines, causing cross-line false positives
	pattern := `(?m)([ \t]*)` + strings.Join(escaped, `[ \t]*(?:\n[ \t]*)?`)

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}

	locs := re.FindAllStringIndex(content, -1)
	if len(locs) == 0 {
		return nil
	}

	indices := make([]int, len(locs))
	actuals := make([]string, len(locs))
	for i, loc := range locs {
		indices[i] = loc[0]
		actuals[i] = content[loc[0]:loc[1]]
	}
	return &matchResult{strategy: "regex", indices: indices, actuals: actuals}
}

var delimiterSet = map[rune]bool{
	'(': true, ')': true, ':': true, '[': true, ']': true,
	'{': true, '}': true, '<': true, '>': true, '=': true,
}

func tokenize(s string) []string {
	// split by delimiters and whitespace
	var tokens []string
	var current strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}
		if delimiterSet[r] {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			tokens = append(tokens, string(r))
			continue
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// --- Quote normalization ---

// normalizeQuotes converts smart/curly quotes to straight ASCII quotes.
func normalizeQuotes(s string) string {
	r := strings.NewReplacer(
		"\u2018", "'",
		"\u2019", "'",
		"\u201c", `"`,
		"\u201d", `"`,
	)
	return r.Replace(s)
}

// normalizeQuotesWithMapping normalizes quotes and returns a mapping from
// normalized byte offset to original byte offset. Each entry maps the start
// of a normalized byte position to the corresponding original byte position.
func normalizeQuotesWithMapping(s string) (string, []int) {
	var buf strings.Builder
	buf.Grow(len(s))
	// mapping[i] = original byte offset for normalized byte i
	mapping := make([]int, 0, len(s)+1)
	origOff := 0
	for origOff < len(s) {
		r, size := utf8.DecodeRuneInString(s[origOff:])
		var replacement string
		switch r {
		case '\u2018', '\u2019':
			replacement = "'"
		case '\u201c', '\u201d':
			replacement = `"`
		default:
			replacement = s[origOff : origOff+size]
		}
		for i := 0; i < len(replacement); i++ {
			mapping = append(mapping, origOff)
		}
		buf.WriteString(replacement)
		origOff += size
	}
	mapping = append(mapping, origOff) // sentinel for end-of-string
	return buf.String(), mapping
}

// preserveQuoteStyle re-applies the original file's curly quote style to newString.
func preserveQuoteStyle(oldString, actualOldString, newString string) string {
	if oldString == actualOldString {
		return newString
	}
	hasDouble := strings.ContainsAny(actualOldString, "“”")
	hasSingle := strings.ContainsAny(actualOldString, "‘’")
	if !hasDouble && !hasSingle {
		return newString
	}

	result := newString
	if hasDouble {
		result = applyCurlyDoubleQuotes(result)
	}
	if hasSingle {
		result = applyCurlySingleQuotes(result)
	}
	return result
}

func applyCurlyDoubleQuotes(s string) string {
	runes := []rune(s)
	var sb strings.Builder
	sb.Grow(len(s))
	for i, r := range runes {
		if r == '"' {
			if isOpeningContext(runes, i) {
				sb.WriteRune('“')
			} else {
				sb.WriteRune('”')
			}
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func applyCurlySingleQuotes(s string) string {
	runes := []rune(s)
	var sb strings.Builder
	sb.Grow(len(s))
	for i, r := range runes {
		if r == '\'' {
			prev := rune(0)
			next := rune(0)
			if i > 0 {
				prev = runes[i-1]
			}
			if i < len(runes)-1 {
				next = runes[i+1]
			}
			if unicode.IsLetter(prev) && unicode.IsLetter(next) {
				sb.WriteRune('’') // apostrophe in contraction
			} else if isOpeningContext(runes, i) {
				sb.WriteRune('‘')
			} else {
				sb.WriteRune('’')
			}
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func isOpeningContext(runes []rune, i int) bool {
	if i == 0 {
		return true
	}
	prev := runes[i-1]
	return unicode.IsSpace(prev) || prev == '(' || prev == '[' || prev == '{' ||
		prev == '—' || prev == '–'
}

// --- Helpers ---

func allIndices(content, sub string) []int {
	var indices []int
	start := 0
	for {
		idx := strings.Index(content[start:], sub)
		if idx < 0 {
			break
		}
		indices = append(indices, start+idx)
		start += idx + len(sub)
	}
	return indices
}

func countNewlines(s string) int {
	n := 0
	for _, c := range s {
		if c == '\n' {
			n++
		}
	}
	return n
}

// applyIndentation re-indents replacement lines relative to target indentation.
func applyIndentation(lines []string, targetIndent string) []string {
	if len(lines) == 0 {
		return lines
	}
	// detect reference indent from first line
	refIndent := extractIndent(lines[0])
	result := make([]string, len(lines))
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			result[i] = ""
			continue
		}
		if strings.HasPrefix(line, refIndent) {
			result[i] = targetIndent + line[len(refIndent):]
		} else {
			result[i] = targetIndent + strings.TrimLeft(line, " \t")
		}
	}
	return result
}

func extractIndent(line string) string {
	for i, r := range line {
		if r != ' ' && r != '\t' {
			return line[:i]
		}
	}
	return line
}

// diffSnippet generates a ±contextLines snippet around the edit for the success response.
func diffSnippet(newContent string, editStart, oldLen, newLen, contextLines int) string {
	lines := strings.Split(newContent, "\n")
	contentLen := len(newContent)
	if editStart > contentLen {
		editStart = contentLen
	}
	editEnd := editStart + newLen
	if editEnd > contentLen {
		editEnd = contentLen
	}
	// find the line number where the edit starts
	editLine := strings.Count(newContent[:editStart], "\n")
	// find the line number where the edit ends
	editEndLine := editLine + strings.Count(newContent[editStart:editEnd], "\n")

	start := editLine - contextLines
	if start < 0 {
		start = 0
	}
	end := editEndLine + contextLines + 1
	if end > len(lines) {
		end = len(lines)
	}

	var sb strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&sb, "%6d|%s\n", i+1, lines[i])
	}
	return sb.String()
}
