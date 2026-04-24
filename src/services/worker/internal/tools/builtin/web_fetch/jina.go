package webfetch

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultJinaBaseURL = "https://r.jina.ai"

type JinaProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewJinaProvider creates a Jina provider. apiKey may be empty for anonymous
// free-tier access (rate-limited but requires no account).
func NewJinaProvider(apiKey string) (*JinaProvider, error) {
	return &JinaProvider{
		apiKey:  strings.TrimSpace(apiKey),
		baseURL: defaultJinaBaseURL,
		client:  &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (p *JinaProvider) Fetch(ctx context.Context, targetURL string, maxLength int) (Result, error) {
	cleanedURL := strings.TrimSpace(targetURL)
	reqURL := strings.TrimRight(p.baseURL, "/") + "/" + cleanedURL

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return Result{}, err
	}
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, HttpError{StatusCode: resp.StatusCode}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5_000_000))
	if err != nil {
		return Result{}, err
	}

	content := strings.TrimSpace(string(body))
	title := extractTitleFromMarkdown(content)

	truncated := false
	if len(content) > maxLength {
		content = content[:maxLength]
		truncated = true
	}
	if len(title) > 512 {
		title = title[:512]
	}

	return Result{
		URL:       cleanedURL,
		Title:     title,
		Content:   content,
		Truncated: truncated,
	}, nil
}

func extractTitleFromMarkdown(text string) string {
	for _, line := range strings.Split(text, "\n") {
		stripped := strings.TrimSpace(line)
		if stripped == "" {
			continue
		}
		if strings.HasPrefix(stripped, "# ") {
			return strings.TrimSpace(stripped[2:])
		}
		lowered := strings.ToLower(stripped)
		if strings.HasPrefix(lowered, "title:") {
			return strings.TrimSpace(strings.SplitN(stripped, ":", 2)[1])
		}
		return ""
	}
	return ""
}
