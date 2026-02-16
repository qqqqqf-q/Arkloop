package websearch

import "context"

type Result struct {
	Title   string
	URL     string
	Snippet string
}

func (r Result) ToJSON() map[string]any {
	payload := map[string]any{
		"title": r.Title,
		"url":   r.URL,
	}
	if r.Snippet != "" {
		payload["snippet"] = r.Snippet
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

