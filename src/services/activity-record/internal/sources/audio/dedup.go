package audio

import (
	"strings"
	"unicode"
)

// dedup trims the leading words of current that duplicate the trailing words of
// previous, removing the repetition introduced by the 2s segment overlap. The
// match is computed on a punctuation-stripped, lowercased view but the trim is
// applied to the original words so casing and punctuation are preserved.
func dedup(previous, current string) string {
	currOrig := strings.Fields(current)
	if previous == "" || len(currOrig) == 0 {
		return current
	}
	prevWords := strings.Fields(normalizeForMatch(previous))
	currWords := strings.Fields(normalizeForMatch(current))
	if len(prevWords) == 0 || len(currWords) != len(currOrig) {
		return current
	}

	maxK := len(prevWords)
	if len(currWords) < maxK {
		maxK = len(currWords)
	}
	overlap := 0
	for k := maxK; k > 0; k-- {
		if equalWords(prevWords[len(prevWords)-k:], currWords[:k]) {
			overlap = k
			break
		}
	}
	if overlap == 0 {
		return current
	}
	if overlap >= len(currOrig) {
		return ""
	}
	return strings.Join(currOrig[overlap:], " ")
}

func normalizeForMatch(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	return b.String()
}

func equalWords(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
