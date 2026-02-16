package webfetch

import "context"

type Result struct {
	Title   string
	Content string
}

func (r Result) ToJSON() map[string]any {
	payload := map[string]any{}
	if r.Title != "" {
		payload["title"] = r.Title
	}
	payload["content"] = r.Content
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

