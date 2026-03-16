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

const (
	browserSearchTimeout = 35 * time.Second
	browserSearchMaxBody = 1_000_000
)

// DesktopBrowserProvider delegates web searches to the Electron host browser
// via the local callback HTTP server started by browser-search.ts.
// The callback addr is passed through ARKLOOP_WEB_SEARCH_DESKTOP_CALLBACK_ADDR.
type DesktopBrowserProvider struct {
	callbackAddr string
	client       *http.Client
}

func NewDesktopBrowserProvider(callbackAddr string) *DesktopBrowserProvider {
	return &DesktopBrowserProvider{
		callbackAddr: strings.TrimSpace(callbackAddr),
		client:       &http.Client{Timeout: browserSearchTimeout},
	}
}

type browserSearchRequest struct {
	Query      string `json:"query"`
	MaxResults int    `json:"maxResults"`
}

type browserSearchItem struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

type browserSearchResponse struct {
	Ok      bool                `json:"ok"`
	Results []browserSearchItem `json:"results"`
	Error   string              `json:"error"`
	Captcha bool                `json:"captcha"`
}

func (p *DesktopBrowserProvider) Search(ctx context.Context, query string, maxResults int) ([]Result, error) {
	if p.callbackAddr == "" {
		return nil, fmt.Errorf("browser search callback addr not configured")
	}

	reqBody, err := json.Marshal(browserSearchRequest{
		Query:      query,
		MaxResults: maxResults,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	endpoint := fmt.Sprintf("http://%s/browser-search", p.callbackAddr)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("browser search request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, browserSearchMaxBody))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, HttpError{StatusCode: resp.StatusCode}
	}

	var result browserSearchResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if !result.Ok {
		if result.Captcha {
			return nil, fmt.Errorf("browser search blocked by CAPTCHA — please open a browser to complete the search")
		}
		return nil, fmt.Errorf("browser search failed: %s", result.Error)
	}

	out := make([]Result, 0, len(result.Results))
	for _, item := range result.Results {
		out = append(out, Result{
			Title:   strings.TrimSpace(item.Title),
			URL:     strings.TrimSpace(item.URL),
			Snippet: strings.TrimSpace(item.Snippet),
		})
	}
	return out, nil
}
