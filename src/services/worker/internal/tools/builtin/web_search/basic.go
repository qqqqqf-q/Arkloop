package websearch

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	sharedoutbound "arkloop/services/shared/outboundurl"
)

const (
	basicSearchEndpointEnv = "ARKLOOP_DESKTOP_BROWSER_SEARCH_URL"
	basicSearchTokenEnv    = "ARKLOOP_DESKTOP_TOKEN"
	basicSearchMaxCount    = 50
)

type BasicProvider struct {
	endpointURL string
	token       string
	client      *http.Client
}

type basicSearchResponse struct {
	Results []basicSearchResult `json:"results"`
}

type basicSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

func NewBasicProvider() *BasicProvider {
	return NewBasicProviderWithEndpoint(
		strings.TrimSpace(os.Getenv(basicSearchEndpointEnv)),
		strings.TrimSpace(os.Getenv(basicSearchTokenEnv)),
	)
}

func NewBasicProviderWithEndpoint(endpointURL, token string) *BasicProvider {
	return &BasicProvider{
		endpointURL: strings.TrimRight(strings.TrimSpace(endpointURL), "/"),
		token:       strings.TrimSpace(token),
		client:      sharedoutbound.DefaultPolicy().NewInternalHTTPClient(15 * time.Second),
	}
}

func (p *BasicProvider) Search(ctx context.Context, query string, maxResults int) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query must not be empty")
	}
	if maxResults <= 0 {
		return nil, fmt.Errorf("maxResults must be a positive integer")
	}
	if strings.TrimSpace(p.endpointURL) == "" {
		return nil, fmt.Errorf("desktop browser search endpoint not configured")
	}

	reqURL, err := p.searchURL(query, maxResults)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, HttpError{StatusCode: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1_000_000))
	if err != nil {
		return nil, err
	}

	var payload basicSearchResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode desktop browser search response: %w", err)
	}
	results := normalizeBasicEndpointResults(payload.Results, maxResults)
	if len(results) == 0 {
		return nil, fmt.Errorf("search returned no usable results")
	}
	return results, nil
}

func (p *BasicProvider) searchURL(query string, maxResults int) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(p.endpointURL))
	if err != nil {
		return "", err
	}
	if !parsed.IsAbs() || strings.TrimSpace(parsed.Hostname()) == "" {
		return "", fmt.Errorf("invalid desktop browser search endpoint")
	}
	q := parsed.Query()
	q.Set("q", query)
	count := maxResults
	if count > basicSearchMaxCount {
		count = basicSearchMaxCount
	}
	q.Set("max_results", strconv.Itoa(count))
	parsed.RawQuery = q.Encode()
	return parsed.String(), nil
}

func normalizeBasicEndpointResults(items []basicSearchResult, maxResults int) []Result {
	if maxResults <= 0 {
		return nil
	}
	out := make([]Result, 0, min(len(items), maxResults))
	seen := map[string]struct{}{}
	for _, item := range items {
		if len(out) >= maxResults {
			break
		}
		cleanURL := cleanBasicResultURL(item.URL)
		if cleanURL == "" {
			continue
		}
		key := normalizeURL(cleanURL)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		title := normalizeInlineText(item.Title, 160)
		if title == "" {
			title = titleFromURL(cleanURL)
		}
		if title == "" {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, Result{
			Title:   title,
			URL:     cleanURL,
			Snippet: normalizeInlineText(item.Snippet, 320),
		})
	}
	return out
}

func cleanBasicResultURL(raw string) string {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return ""
	}
	parsed, err := url.Parse(cleaned)
	if err != nil {
		return ""
	}
	if decoded := decodeBingRedirectURL(parsed); decoded != "" {
		return decoded
	}
	if !isPublicSearchResultURL(parsed) || isBingHost(parsed.Hostname()) {
		return ""
	}
	parsed.Fragment = ""
	parsed.RawFragment = ""
	return parsed.String()
}

func decodeBingRedirectURL(parsed *url.URL) string {
	if parsed == nil || !isBingHost(parsed.Hostname()) {
		return ""
	}
	raw := strings.TrimSpace(parsed.Query().Get("u"))
	if raw == "" {
		return ""
	}
	candidate := raw
	if strings.HasPrefix(candidate, "a1") {
		if decoded := decodeBase64URL(candidate[2:]); decoded != "" {
			candidate = decoded
		}
	}
	target, err := url.Parse(strings.TrimSpace(candidate))
	if err != nil || !isPublicSearchResultURL(target) || isBingHost(target.Hostname()) {
		return ""
	}
	target.Fragment = ""
	target.RawFragment = ""
	return target.String()
}

func decodeBase64URL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	encodings := []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	}
	for _, enc := range encodings {
		decoded, err := enc.DecodeString(value)
		if err == nil {
			return strings.TrimSpace(string(decoded))
		}
	}
	return ""
}

func isPublicSearchResultURL(parsed *url.URL) bool {
	if parsed == nil || parsed.User != nil {
		return false
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme != "http" && scheme != "https" {
		return false
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return false
	}
	policy := sharedoutbound.DefaultPolicy()
	if !policy.ProtectionEnabled {
		return true
	}
	lowered := strings.ToLower(strings.Trim(host, "."))
	if lowered == "localhost" || strings.HasSuffix(lowered, ".localhost") {
		return false
	}
	if ip := sharedoutbound.ParseIP(host); ip.IsValid() {
		return policy.EnsureIPAllowed(ip) == nil
	}
	return true
}

func isBingHost(host string) bool {
	lowered := strings.ToLower(strings.Trim(strings.TrimSpace(host), "."))
	return lowered == "bing.com" || strings.HasSuffix(lowered, ".bing.com")
}
