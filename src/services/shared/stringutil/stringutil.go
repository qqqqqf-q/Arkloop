package stringutil

import "strings"

// SplitCSV splits a comma-separated string, trims whitespace, and drops empty entries.
func SplitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if v := strings.TrimSpace(part); v != "" {
			result = append(result, v)
		}
	}
	return result
}
