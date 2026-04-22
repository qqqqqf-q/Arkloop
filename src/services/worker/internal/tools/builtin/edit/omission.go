package edit

import (
	"fmt"
	"strings"
)

// omission placeholder phrases (lowercase, prefix-matched)
var omissionPhrases = []string{
	"rest of method",
	"rest of methods",
	"rest of code",
	"rest of file",
	"rest of function",
	"rest of class",
	"rest of",
	"unchanged code",
	"existing code",
	"previous code",
	"remaining code",
	"other methods",
	"other cases",
	"same as before",
	"keep existing",
	"no changes",
}

// comment prefixes to strip before checking
var commentPrefixes = []string{
	"<!--", "//", "#", "--", "/*", "*",
}

// DetectOmissionPlaceholders scans newString for lazy placeholder patterns
// like "// rest of code..." that don't exist in oldString.
func DetectOmissionPlaceholders(oldString, newString string) *editError {
	oldLines := strings.Split(oldString, "\n")
	oldSet := make(map[string]struct{}, len(oldLines))
	for _, l := range oldLines {
		oldSet[strings.TrimSpace(l)] = struct{}{}
	}

	for i, line := range strings.Split(newString, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// strip comment prefix
		stripped := trimmed
		for _, p := range commentPrefixes {
			if strings.HasPrefix(stripped, p) {
				stripped = strings.TrimSpace(stripped[len(p):])
				break
			}
		}

		// check for ellipsis
		dotIdx := strings.Index(stripped, "...")
		if dotIdx < 0 {
			dotIdx = strings.Index(stripped, "…") // unicode ellipsis
		}
		if dotIdx < 0 {
			continue
		}

		prefix := strings.ToLower(strings.TrimSpace(stripped[:dotIdx]))
		if !matchesOmissionPhrase(prefix) {
			continue
		}

		// allow if oldString contains the same line
		if _, ok := oldSet[trimmed]; ok {
			continue
		}

		return &editError{
			Code:    ErrCodeOmission,
			Message: fmt.Sprintf("Detected omission placeholder in new_string: '%s' at line %d. Write the complete replacement code instead of using placeholders.", trimmed, i+1),
		}
	}
	return nil
}

func matchesOmissionPhrase(text string) bool {
	for _, phrase := range omissionPhrases {
		if strings.HasPrefix(text, phrase) {
			return true
		}
	}
	return false
}
