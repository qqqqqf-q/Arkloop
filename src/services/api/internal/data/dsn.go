package data

import (
	"net/url"
	"strings"
)

func NormalizePostgresDSN(raw string) string {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return ""
	}

	parsed, err := url.Parse(cleaned)
	if err != nil {
		return cleaned
	}

	if parsed.Scheme == "postgresql+asyncpg" {
		parsed.Scheme = "postgresql"
		return parsed.String()
	}

	return cleaned
}
