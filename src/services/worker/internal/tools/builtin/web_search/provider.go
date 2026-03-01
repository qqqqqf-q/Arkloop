package websearch

import (
	"context"
	"strings"
	"unicode/utf8"
)

type Result struct {
	Title   string
	URL     string
	Snippet string
}

func (r Result) ToJSON() map[string]any {
	title := normalizeInlineText(r.Title, 120)
	urlText := strings.TrimSpace(r.URL)
	snippet := normalizeInlineText(r.Snippet, 240)

	payload := map[string]any{
		"title": title,
		"url":   urlText,
	}
	if snippet != "" {
		payload["snippet"] = snippet
	}
	return payload
}

type Provider interface {
	Search(ctx context.Context, query string, maxResults int) ([]Result, error)
}

type HttpError struct {
	StatusCode int
}

func (e HttpError) Error() string {
	return "http error"
}

func normalizeInlineText(value string, maxChars int) string {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return ""
	}
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	return truncateRunes(cleaned, maxChars)
}

func truncateRunes(value string, maxChars int) string {
	if maxChars <= 0 || value == "" {
		return ""
	}
	if utf8.RuneCountInString(value) <= maxChars {
		return value
	}
	out := make([]rune, 0, maxChars)
	for _, r := range value {
		if len(out) >= maxChars {
			break
		}
		out = append(out, r)
	}
	return string(out)
}
