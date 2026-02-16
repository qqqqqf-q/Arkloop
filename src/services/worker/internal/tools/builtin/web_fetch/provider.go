package webfetch

import "context"

type Result struct {
	URL       string
	Title     string
	Content   string
	Truncated bool
}

func (r Result) ToJSON() map[string]any {
	payload := map[string]any{
		"url":       r.URL,
		"title":     r.Title,
		"content":   r.Content,
		"truncated": r.Truncated,
	}
	return payload
}

type Provider interface {
	Fetch(ctx context.Context, url string, maxLength int) (Result, error)
}

type HttpError struct {
	StatusCode int
}

func (e HttpError) Error() string {
	return "http error"
}
