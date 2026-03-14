package webfetch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	sharedoutbound "arkloop/services/shared/outboundurl"
)

const defaultFirecrawlBaseURL = "https://api.firecrawl.dev"

type FirecrawlProvider struct {
	apiKey     string
	baseURL    string
	client     *http.Client
	baseURLErr error
}

func NewFirecrawlProvider(apiKey string, baseURL string) *FirecrawlProvider {
	cleanedKey := strings.TrimSpace(apiKey)
	trimmedBase := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmedBase == "" {
		trimmedBase = defaultFirecrawlBaseURL
	}
	normalizedBaseURL, baseURLErr := sharedoutbound.DefaultPolicy().NormalizeInternalBaseURL(trimmedBase)
	if baseURLErr == nil {
		trimmedBase = normalizedBaseURL
	}

	return &FirecrawlProvider{
		apiKey:     cleanedKey,
		baseURL:    trimmedBase,
		client:     sharedoutbound.DefaultPolicy().NewInternalHTTPClient(60 * time.Second),
		baseURLErr: baseURLErr,
	}
}

func (p *FirecrawlProvider) Fetch(ctx context.Context, targetURL string, maxLength int) (Result, error) {
	if p.baseURLErr != nil {
		return Result{}, p.baseURLErr
	}
	payload := map[string]any{
		"url":             targetURL,
		"formats":         []string{"markdown"},
		"onlyMainContent": true,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Result{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/scrape", bytes.NewReader(encoded))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
		req.Header.Set("x-api-key", p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Result{}, HttpError{StatusCode: resp.StatusCode}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5_000_000))
	if err != nil {
		return Result{}, err
	}

	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Result{}, err
	}
	root, ok := parsed.(map[string]any)
	if !ok {
		return Result{}, fmt.Errorf("firecrawl response must be a JSON object")
	}
	if success, ok := root["success"].(bool); ok && !success {
		return Result{}, fmt.Errorf("firecrawl response success=false")
	}
	rawData, ok := root["data"].(map[string]any)
	if !ok {
		return Result{}, fmt.Errorf("firecrawl response data must be a JSON object")
	}

	content := ""
	if markdown, ok := rawData["markdown"].(string); ok && strings.TrimSpace(markdown) != "" {
		content = markdown
	} else if text, ok := rawData["content"].(string); ok && strings.TrimSpace(text) != "" {
		content = text
	}

	title := ""
	if meta, ok := rawData["metadata"].(map[string]any); ok {
		if metaTitle, ok := meta["title"].(string); ok && strings.TrimSpace(metaTitle) != "" {
			title = metaTitle
		}
	}
	if title == "" {
		if rawTitle, ok := rawData["title"].(string); ok && strings.TrimSpace(rawTitle) != "" {
			title = rawTitle
		}
	}

	finalURL := targetURL
	if rawURL, ok := rawData["url"].(string); ok && strings.TrimSpace(rawURL) != "" {
		finalURL = rawURL
	}

	truncated := false
	if len(content) > maxLength {
		content = content[:maxLength]
		truncated = true
	}
	if len(title) > 512 {
		title = title[:512]
	}

	return Result{
		URL:       strings.TrimSpace(finalURL),
		Title:     strings.TrimSpace(title),
		Content:   strings.TrimSpace(content),
		Truncated: truncated,
	}, nil
}
