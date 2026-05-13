package websearch

import (
	"context"
	"net/url"
	"strings"
	"unicode/utf8"

	"arkloop/services/worker/internal/tools"
)

type Result struct {
	QueryIndex      int
	Title           string
	URL             string
	Snippet         string
	Published       string
	SiteName        string
	Summary         string
	Text            string
	HighlightScores []float64
}

func (r Result) ToJSON() map[string]any {
	title := normalizeInlineText(r.Title, 120)
	urlText := strings.TrimSpace(r.URL)
	snippet := normalizeInlineText(r.Snippet, 240)

	payload := map[string]any{
		"query_index": r.QueryIndex,
		"title":       title,
		"url":         urlText,
	}
	if snippet != "" {
		payload["snippet"] = snippet
	}
	if published := strings.TrimSpace(r.Published); published != "" {
		payload["published"] = published
	}
	if siteName := normalizeInlineText(r.SiteName, 80); siteName != "" {
		payload["siteName"] = siteName
	}
	if summary := normalizeBlockText(r.Summary); summary != "" {
		payload["summary"] = summary
	}
	if text := normalizeBlockText(r.Text); text != "" {
		payload["text"] = text
	}
	if len(r.HighlightScores) > 0 {
		payload["highlightScores"] = append([]float64{}, r.HighlightScores...)
	}
	return payload
}

type SearchRequest struct {
	Query      string
	MaxResults int
	Args       map[string]any
}

type Provider interface {
	Search(ctx context.Context, request SearchRequest) ([]Result, error)
}

type RequestParser interface {
	ParseSearchRequests(args map[string]any) ([]SearchRequest, *tools.ExecutionError)
}

type HttpError struct {
	StatusCode int
	Body       string
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

func normalizeBlockText(value string) string {
	return strings.TrimSpace(value)
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

func titleFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if host == "" {
		return ""
	}
	return host
}
