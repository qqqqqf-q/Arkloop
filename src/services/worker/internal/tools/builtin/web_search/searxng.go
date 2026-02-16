package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type SearxngProvider struct {
	baseURL string
	client  *http.Client
}

func NewSearxngProvider(baseURL string) *SearxngProvider {
	cleaned := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return &SearxngProvider{
		baseURL: cleaned,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *SearxngProvider) Search(ctx context.Context, query string, maxResults int) ([]Result, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("query 不能为空")
	}
	if maxResults <= 0 {
		return nil, fmt.Errorf("maxResults 必须为正整数")
	}

	parsed, err := url.Parse(p.baseURL + "/search")
	if err != nil {
		return nil, err
	}
	params := parsed.Query()
	params.Set("q", query)
	params.Set("format", "json")
	parsed.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, HttpError{StatusCode: resp.StatusCode}
	}

	var parsedJSON any
	if err := json.Unmarshal(body, &parsedJSON); err != nil {
		return nil, err
	}
	root, ok := parsedJSON.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("searxng response 类型错误")
	}

	rawResults, ok := root["results"].([]any)
	if !ok {
		return nil, fmt.Errorf("searxng response 缺少 results")
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
		out = append(out, Result{
			Title:   title,
			URL:     urlText,
			Snippet: snippet,
		})
	}
	return out, nil
}

