package edit

import "strings"

// matchFuzzy performs weighted Levenshtein fuzzy matching as a 4th-layer fallback.
// Whitespace-only differences are penalized at 10% of character differences.
func matchFuzzy(content, oldString string) *matchResult {
	sourceLines := strings.Split(content, "\n")
	searchLines := strings.Split(oldString, "\n")
	searchBytes := []byte(oldString)

	// complexity guard
	if int64(len(sourceLines))*int64(len(searchBytes))*int64(len(searchBytes)) > 4e8 {
		return nil
	}

	if len(searchLines) == 0 || len(searchLines) > len(sourceLines) {
		return nil
	}

	const threshold = 0.1

	bestScore := threshold + 1
	bestStart := -1

	for i := 0; i <= len(sourceLines)-len(searchLines); i++ {
		candidate := joinLines(sourceLines, i, i+len(searchLines))

		if !lengthHeuristicOK(candidate, oldString, 0.3) {
			continue
		}

		score := weightedScore(candidate, oldString)
		if score <= threshold && score < bestScore {
			bestScore = score
			bestStart = i
		}
	}

	if bestStart < 0 {
		return nil
	}

	offset := byteOffsetOfLine(sourceLines, bestStart)
	actual := joinLines(sourceLines, bestStart, bestStart+len(searchLines))
	return &matchResult{strategy: "fuzzy", indices: []int{offset}, actuals: []string{actual}}
}

// weightedScore computes the weighted edit distance score.
// d_norm + (d_raw - d_norm) * 0.1, normalized by search length.
func weightedScore(candidate, search string) float64 {
	dRaw := levenshtein(candidate, search)
	dNorm := levenshtein(stripWhitespace(candidate), stripWhitespace(search))
	weighted := float64(dNorm) + float64(dRaw-dNorm)*0.1
	return weighted / float64(len([]byte(search)))
}

// lengthHeuristicOK returns true if candidate length is within threshold of search length.
func lengthHeuristicOK(candidate, search string, threshold float64) bool {
	cl, sl := len(candidate), len(search)
	diff := cl - sl
	if diff < 0 {
		diff = -diff
	}
	return float64(diff)/float64(sl) <= threshold
}

func joinLines(lines []string, start, end int) string {
	return strings.Join(lines[start:end], "\n")
}

func byteOffsetOfLine(lines []string, lineIdx int) int {
	offset := 0
	for i := 0; i < lineIdx; i++ {
		offset += len(lines[i]) + 1
	}
	return offset
}

func stripWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// levenshtein computes the edit distance between two strings using DP.
func levenshtein(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	ra := []rune(a)
	rb := []rune(b)
	m, n := len(ra), len(rb)

	// single-row DP
	prev := make([]int, n+1)
	for j := 0; j <= n; j++ {
		prev[j] = j
	}

	for i := 1; i <= m; i++ {
		curr := make([]int, n+1)
		curr[0] = i
		for j := 1; j <= n; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev = curr
	}
	return prev[n]
}

