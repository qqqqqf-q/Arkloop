package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type TavilyProvider struct {
	apiKey string
	client *http.Client
}

func NewTavilyProvider(apiKey string) *TavilyProvider {
	return &TavilyProvider{
		apiKey: strings.TrimSpace(apiKey),
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *TavilyProvider) Search(ctx context.Context, query string, maxResults int) ([]Result, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("query must not be empty")
	}
	if maxResults <= 0 {
		return nil, fmt.Errorf("maxResults must be a positive integer")
	}
	if p.apiKey == "" {
		return nil, fmt.Errorf("missing Tavily api_key")
	}

	payload := map[string]any{
		"api_key":     p.apiKey,
		"query":       query,
		"max_results": maxResults,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.tavily.com/search", bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, HttpError{StatusCode: resp.StatusCode}
	}

	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	root, ok := parsed.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tavily response type error")
	}
	rawResults, ok := root["results"].([]any)
	if !ok {
		return nil, fmt.Errorf("tavily response missing results")
	}

	out := make([]Result, 0, maxResults)
	for _, item := range rawResults {
		if len(out) >= maxResults {
			break
		}
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		title, _ := obj["title"].(string)
		urlText, _ := obj["url"].(string)
		snippet, _ := obj["content"].(string)
		title = strings.TrimSpace(title)
		urlText = strings.TrimSpace(urlText)
		snippet = strings.TrimSpace(snippet)
		if title == "" || urlText == "" {
			continue
		}
		out = append(out, Result{Title: title, URL: urlText, Snippet: snippet})
	}
	return out, nil
}
