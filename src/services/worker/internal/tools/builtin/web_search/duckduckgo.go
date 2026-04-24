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
	"unicode/utf8"
)

// Instant Answer JSON；结果密度低于商业搜索 API，无命中时返回空列表属正常。
const defaultDuckduckgoAPI = "https://api.duckduckgo.com"

type DuckduckgoProvider struct {
	baseURL string
	client  *http.Client
}

func NewDuckduckgoProvider() *DuckduckgoProvider {
	return NewDuckduckgoProviderWithBaseURL(defaultDuckduckgoAPI)
}

// NewDuckduckgoProviderWithBaseURL 用于测试将请求发到 httptest.Server。
func NewDuckduckgoProviderWithBaseURL(baseURL string) *DuckduckgoProvider {
	u := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return &DuckduckgoProvider{
		baseURL: u,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

type ddgTopic struct {
	Text     string     `json:"Text"`
	FirstURL string     `json:"FirstURL"`
	Topics   []ddgTopic `json:"Topics"`
}

type ddgResponse struct {
	Heading          string     `json:"Heading"`
	Abstract         string     `json:"Abstract"`
	AbstractText     string     `json:"AbstractText"`
	AbstractURL      string     `json:"AbstractURL"`
	Definition       string     `json:"Definition"`
	DefinitionSource string     `json:"DefinitionSource"`
	DefinitionURL    string     `json:"DefinitionURL"`
	Results          []ddgTopic `json:"Results"`
	RelatedTopics    []ddgTopic `json:"RelatedTopics"`
}

func (p *DuckduckgoProvider) Search(ctx context.Context, query string, maxResults int) ([]Result, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("query must not be empty")
	}
	if maxResults <= 0 {
		return nil, fmt.Errorf("maxResults must be a positive integer")
	}

	parsed, err := p.fetchInstantJSON(ctx, query)
	if err != nil {
		return nil, err
	}
	out := collectDuckduckgoResults(parsed, maxResults)
	if len(out) == 0 {
		if alt := shortenQueryForDDGFallback(query); alt != query {
			parsed2, err2 := p.fetchInstantJSON(ctx, alt)
			if err2 == nil {
				out = collectDuckduckgoResults(parsed2, maxResults)
			}
		}
	}
	return out, nil
}

func (p *DuckduckgoProvider) fetchInstantJSON(ctx context.Context, query string) (ddgResponse, error) {
	params := url.Values{}
	params.Set("q", query)
	params.Set("format", "json")
	params.Set("no_html", "1")
	params.Set("no_redirect", "1")

	reqURL := p.baseURL + "/?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return ddgResponse{}, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return ddgResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return ddgResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ddgResponse{}, HttpError{StatusCode: resp.StatusCode}
	}

	var parsed ddgResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ddgResponse{}, err
	}
	return parsed, nil
}

// Instant Answer 对长句、新闻式问法经常零命中；缩成核心词再请求一次，提高 Wikipedia / 站内链命中概率。
func shortenQueryForDDGFallback(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return q
	}
	fields := strings.Fields(q)
	if len(fields) > 5 {
		return strings.Join(fields[:5], " ")
	}
	if utf8.RuneCountInString(q) > 36 {
		r := []rune(q)
		return strings.TrimSpace(string(r[:36]))
	}
	return q
}

func collectDuckduckgoResults(parsed ddgResponse, maxResults int) []Result {
	if maxResults <= 0 {
		return nil
	}
	var out []Result
	seen := map[string]struct{}{}

	tryAdd := func(title, link, snippet string) {
		if len(out) >= maxResults {
			return
		}
		u := strings.TrimSpace(link)
		if u == "" {
			return
		}
		if _, dup := seen[u]; dup {
			return
		}
		seen[u] = struct{}{}
		title = strings.TrimSpace(title)
		if title == "" {
			title = titleFromURL(u)
		}
		snippet = strings.TrimSpace(snippet)
		out = append(out, Result{Title: title, URL: u, Snippet: snippet})
	}

	summary := firstNonEmpty(parsed.AbstractText, parsed.Abstract)
	urlMain := strings.TrimSpace(parsed.AbstractURL)
	titleMain := strings.TrimSpace(parsed.Heading)
	if urlMain != "" {
		if titleMain == "" {
			titleMain = titleFromURL(urlMain)
		}
		tryAdd(titleMain, urlMain, summary)
	}

	if defU := strings.TrimSpace(parsed.DefinitionURL); defU != "" {
		defTitle := strings.TrimSpace(parsed.DefinitionSource)
		if defTitle == "" {
			defTitle = "Definition"
		}
		tryAdd(defTitle, defU, parsed.Definition)
	}

	var walk func(topics []ddgTopic)
	walk = func(topics []ddgTopic) {
		for _, t := range topics {
			if len(out) >= maxResults {
				return
			}
			if len(t.Topics) > 0 {
				walk(t.Topics)
				continue
			}
			u := strings.TrimSpace(t.FirstURL)
			if u == "" {
				continue
			}
			text := strings.TrimSpace(t.Text)
			title := text
			if i := strings.LastIndex(text, " - "); i > 0 {
				title = strings.TrimSpace(text[:i])
			}
			if title == "" {
				title = titleFromURL(u)
			}
			snippet := ""
			if i := strings.LastIndex(text, " - "); i > 0 && i+3 < len(text) {
				snippet = strings.TrimSpace(text[i+3:])
			}
			tryAdd(title, u, snippet)
		}
	}

	walk(parsed.Results)
	walk(parsed.RelatedTopics)
	return out
}

func firstNonEmpty(a, b string) string {
	a = strings.TrimSpace(a)
	if a != "" {
		return a
	}
	return strings.TrimSpace(b)
}

func titleFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if host == "" {
		return ""
	}
	return host
}
