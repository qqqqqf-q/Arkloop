package webfetch

import (
	"context"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var titleRegex = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

type BasicProvider struct {
	client *http.Client
}

func NewBasicProvider() *BasicProvider {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return &BasicProvider{
		client: &http.Client{
			Timeout: 20 * time.Second,
			Transport: &http.Transport{
				DialContext: SafeDialContext(dialer),
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return http.ErrUseLastResponse
				}
				return EnsureURLAllowed(req.URL.String())
			},
		},
	}
}

func (p *BasicProvider) Fetch(ctx context.Context, targetURL string, maxLength int) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("User-Agent", "arkloop-web-fetch/1.0")

	resp, err := p.client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, HttpError{StatusCode: resp.StatusCode}
	}

	limit := int64(maxLength)
	if limit <= 0 {
		limit = 1
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	truncated := int64(len(body)) > limit
	if truncated {
		body = body[:limit]
	}

	text := string(body)
	title := extractTitle(text)
	content := stripHTML(text)
	if len(content) > maxLength {
		content = content[:maxLength]
		truncated = true
	}
	if len(title) > 512 {
		title = title[:512]
	}

	finalURL := targetURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	return Result{
		URL:       finalURL,
		Title:     title,
		Content:   strings.TrimSpace(content),
		Truncated: truncated,
	}, nil
}

func extractTitle(html string) string {
	matches := titleRegex.FindStringSubmatch(html)
	if len(matches) < 2 {
		return ""
	}
	return strings.TrimSpace(stripHTML(matches[1]))
}

func stripHTML(html string) string {
	out := regexp.MustCompile(`(?s)<[^>]+>`).ReplaceAllString(html, " ")
	out = strings.ReplaceAll(out, "\u00a0", " ")
	out = strings.Join(strings.Fields(out), " ")
	return out
}
