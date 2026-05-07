package webfetch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	sharedoutbound "arkloop/services/shared/outboundurl"
	"arkloop/services/worker/internal/tools"
)

func TestBasicProviderExtractsReadableHTML(t *testing.T) {
	provider := newBasicProviderForTest(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("Accept"); !strings.Contains(got, "text/html") {
			t.Fatalf("Accept header missing html preference: %q", got)
		}
		body := `<!doctype html>
<html>
<head><title>Fallback title</title><meta property="og:title" content="Article title"></head>
<body>
<nav>navigation should not win</nav>
<script>alert("x")</script>
<article class="article-content">
  <h1>Article title</h1>
  <p>This is the main paragraph with <a href="/docs">docs</a>.</p>
  <aside>related links should not dominate</aside>
  <ul><li>First point</li><li>Second point</li></ul>
</article>
</body></html>`
		return textResponse(req, "text/html; charset=utf-8", body), nil
	})

	result, err := provider.Fetch(context.Background(), "https://example.com/post", 1000)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if result.Title != "Fallback title" {
		t.Fatalf("unexpected title: %q", result.Title)
	}
	if !strings.Contains(result.Content, "# Article title") {
		t.Fatalf("expected heading in content, got:\n%s", result.Content)
	}
	if !strings.Contains(result.Content, "[docs](https://example.com/docs)") {
		t.Fatalf("expected absolute markdown link, got:\n%s", result.Content)
	}
	if strings.Contains(result.Content, "alert(") || strings.Contains(result.Content, "navigation should not win") {
		t.Fatalf("expected script/nav content removed, got:\n%s", result.Content)
	}
	if result.ContentType != "text/html; charset=utf-8" {
		t.Fatalf("unexpected content type: %q", result.ContentType)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", result.StatusCode)
	}
	if result.Extractor != "html-readability" {
		t.Fatalf("unexpected extractor: %q", result.Extractor)
	}
}

func TestBasicProviderDecodesResponseCharset(t *testing.T) {
	provider := newBasicProviderForTest(func(req *http.Request) (*http.Response, error) {
		body := []byte("<html><head><title>Caf\xe9</title></head><body><main><p>Caf\xe9 prices</p></main></body></html>")
		return byteResponse(req, "text/html; charset=iso-8859-1", body), nil
	})

	result, err := provider.Fetch(context.Background(), "https://example.com/cafe", 1000)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if result.Title != "Café" {
		t.Fatalf("expected decoded title, got %q", result.Title)
	}
	if !strings.Contains(result.Content, "Café prices") {
		t.Fatalf("expected decoded content, got %q", result.Content)
	}
}

func TestBasicProviderFormatsJSON(t *testing.T) {
	provider := newBasicProviderForTest(func(req *http.Request) (*http.Response, error) {
		return textResponse(req, "application/json", `{"items":[{"name":"arkloop"}],"ok":true}`), nil
	})

	result, err := provider.Fetch(context.Background(), "https://api.example.com/data", 1000)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if result.Extractor != "json" {
		t.Fatalf("unexpected extractor: %q", result.Extractor)
	}
	if !strings.Contains(result.Content, "\n  \"items\": [") {
		t.Fatalf("expected pretty JSON, got: %s", result.Content)
	}
}

func TestBasicProviderSniffsJSONWithoutContentType(t *testing.T) {
	provider := newBasicProviderForTest(func(req *http.Request) (*http.Response, error) {
		return textResponse(req, "", `{"name":"arkloop"}`), nil
	})

	result, err := provider.Fetch(context.Background(), "https://api.example.com/data", 1000)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if result.Extractor != "json" {
		t.Fatalf("unexpected extractor: %q", result.Extractor)
	}
	if !strings.Contains(result.Content, "\n  \"name\": \"arkloop\"") {
		t.Fatalf("expected pretty JSON, got: %s", result.Content)
	}
}

func TestBasicProviderRejectsBinaryContent(t *testing.T) {
	provider := newBasicProviderForTest(func(req *http.Request) (*http.Response, error) {
		return byteResponse(req, "image/png", []byte{0x89, 'P', 'N', 'G', 0x00}), nil
	})

	_, err := provider.Fetch(context.Background(), "https://example.com/image.png", 1000)
	var contentTypeErr UnsupportedContentTypeError
	if !errors.As(err, &contentTypeErr) {
		t.Fatalf("expected UnsupportedContentTypeError, got %T: %v", err, err)
	}
}

func TestBasicProviderRejectsUnsafeFinalURL(t *testing.T) {
	t.Setenv(sharedoutbound.ProtectionEnabledEnv, "true")

	provider := newBasicProviderForTest(func(req *http.Request) (*http.Response, error) {
		resp := textResponse(req, "text/plain", "secret")
		finalReq, err := http.NewRequest(http.MethodGet, "http://localhost/private", nil)
		if err != nil {
			t.Fatalf("build final request: %v", err)
		}
		resp.Request = finalReq
		return resp, nil
	})

	_, err := provider.Fetch(context.Background(), "https://example.com/redirect", 1000)
	var policyErr UrlPolicyDeniedError
	if !errors.As(err, &policyErr) {
		t.Fatalf("expected UrlPolicyDeniedError, got %T: %v", err, err)
	}
	if policyErr.Reason != "localhost_denied" {
		t.Fatalf("unexpected reason: %q", policyErr.Reason)
	}
}

func TestBasicProviderTruncatesByRune(t *testing.T) {
	provider := newBasicProviderForTest(func(req *http.Request) (*http.Response, error) {
		return textResponse(req, "text/plain; charset=utf-8", "你好世界"), nil
	})

	result, err := provider.Fetch(context.Background(), "https://example.com/text", 2)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if result.Content != "你好" {
		t.Fatalf("expected rune-safe truncation, got %q", result.Content)
	}
	if !result.Truncated {
		t.Fatalf("expected truncated=true")
	}
}

func TestResultToJSONIncludesMetadataAndUntrustedMarker(t *testing.T) {
	payload := Result{
		URL:          "https://example.com/final",
		RequestedURL: "https://example.com/start",
		Title:        "Title",
		Content:      "你好",
		Truncated:    true,
		StatusCode:   http.StatusOK,
		ContentType:  "text/plain; charset=utf-8",
		Extractor:    "text",
		RawLength:    4,
	}.ToJSON()

	if payload["url"] != "https://example.com/final" {
		t.Fatalf("unexpected url: %#v", payload["url"])
	}
	if payload["requested_url"] != "https://example.com/start" || payload["final_url"] != "https://example.com/final" {
		t.Fatalf("expected requested/final url metadata, got %#v", payload)
	}
	if payload["length"] != 2 || payload["raw_length"] != 4 {
		t.Fatalf("unexpected length metadata: %#v", payload)
	}
	external, ok := payload["external_content"].(map[string]any)
	if !ok {
		t.Fatalf("expected external_content map, got %#v", payload["external_content"])
	}
	if external["source"] != "web_fetch" || external["untrusted"] != true {
		t.Fatalf("unexpected external_content: %#v", external)
	}
}

func TestExecutorMapsUnsupportedContentTypeError(t *testing.T) {
	executor := &ToolExecutor{
		provider: unsupportedContentProvider{},
		timeout:  2 * time.Second,
	}
	result := executor.Execute(
		context.Background(),
		"web_fetch",
		map[string]any{
			"url":        "https://example.com/file.pdf",
			"max_length": 10,
		},
		tools.ExecutionContext{},
		"call_1",
	)
	if result.Error == nil {
		t.Fatalf("expected error")
	}
	if result.Error.ErrorClass != errorFetchFailed {
		t.Fatalf("unexpected error class: %s", result.Error.ErrorClass)
	}
	if result.Error.Details["content_type"] != "application/pdf" {
		t.Fatalf("unexpected details: %#v", result.Error.Details)
	}
}

func TestBasicProviderSniffsHTMLWithoutContentType(t *testing.T) {
	provider := newBasicProviderForTest(func(req *http.Request) (*http.Response, error) {
		return textResponse(req, "", "<html><body><main><p>Rendered without header.</p></main></body></html>"), nil
	})

	result, err := provider.Fetch(context.Background(), "https://example.com/no-header", 1000)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if !strings.Contains(result.Content, "Rendered without header.") {
		t.Fatalf("expected html content, got %q", result.Content)
	}
	if result.Extractor != "html" && result.Extractor != "html-readability" {
		t.Fatalf("unexpected extractor: %q", result.Extractor)
	}
}

func TestBasicProviderFallsBackToStructuredHTMLData(t *testing.T) {
	provider := newBasicProviderForTest(func(req *http.Request) (*http.Response, error) {
		body := `<!doctype html>
<html><head>
<script type="application/ld+json">{
  "@context": "https://schema.org",
  "@type": "Article",
  "headline": "Structured fallback title",
  "description": "Structured fallback description",
  "articleBody": "Structured body survived when rendered HTML was empty."
}</script>
<script>window.secret = "must not leak"</script>
</head><body><main hidden>Invisible rendered body</main></body></html>`
		return textResponse(req, "text/html; charset=utf-8", body), nil
	})

	result, err := provider.Fetch(context.Background(), "https://example.com/rendered-empty", 1000)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if result.Extractor != "html-jsonld" {
		t.Fatalf("unexpected extractor: %q", result.Extractor)
	}
	if result.Title != "Structured fallback title" {
		t.Fatalf("unexpected title: %q", result.Title)
	}
	if !strings.Contains(result.Content, "Structured body survived") {
		t.Fatalf("expected structured body, got %q", result.Content)
	}
	if strings.Contains(result.Content, "must not leak") || strings.Contains(result.Content, "Invisible rendered body") {
		t.Fatalf("unsafe or hidden content leaked: %q", result.Content)
	}
}

func TestBasicProviderFallsBackToHTMLMetadata(t *testing.T) {
	provider := newBasicProviderForTest(func(req *http.Request) (*http.Response, error) {
		body := `<!doctype html>
<html><head>
<title>Metadata title</title>
<meta name="description" content="Metadata description survived empty rendered HTML.">
</head><body><script>window.secret = "must not leak"</script></body></html>`
		return textResponse(req, "text/html; charset=utf-8", body), nil
	})

	result, err := provider.Fetch(context.Background(), "https://example.com/metadata", 1000)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if result.Extractor != "html-metadata" {
		t.Fatalf("unexpected extractor: %q", result.Extractor)
	}
	if result.Content != "Metadata description survived empty rendered HTML." {
		t.Fatalf("unexpected content: %q", result.Content)
	}
	if strings.Contains(result.Content, "must not leak") {
		t.Fatalf("script content leaked: %q", result.Content)
	}
}

func TestBasicProviderRetriesTransientRequestError(t *testing.T) {
	attempts := 0
	provider := newBasicProviderForTest(func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts < basicFetchMaxAttempts {
			return nil, errors.New("remote error: TLS handshake timeout")
		}
		return textResponse(req, "text/plain; charset=utf-8", "retry eventually succeeded"), nil
	})

	result, err := provider.Fetch(context.Background(), "https://example.com/retry", 1000)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if attempts != basicFetchMaxAttempts {
		t.Fatalf("expected %d attempts, got %d", basicFetchMaxAttempts, attempts)
	}
	if result.Content != "retry eventually succeeded" {
		t.Fatalf("unexpected content: %q", result.Content)
	}
}

func TestBasicProviderDoesNotRetryHTTPStatus(t *testing.T) {
	attempts := 0
	provider := newBasicProviderForTest(func(req *http.Request) (*http.Response, error) {
		attempts++
		resp := textResponse(req, "text/plain; charset=utf-8", "server error")
		resp.StatusCode = http.StatusInternalServerError
		return resp, nil
	})

	_, err := provider.Fetch(context.Background(), "https://example.com/http-error", 1000)
	var httpErr HttpError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected HttpError, got %T: %v", err, err)
	}
	if attempts != 1 {
		t.Fatalf("expected one attempt, got %d", attempts)
	}
}

func TestDefaultTimeoutIsThirtySeconds(t *testing.T) {
	if defaultTimeout != 30*time.Second {
		t.Fatalf("unexpected default timeout: %s", defaultTimeout)
	}
}

type unsupportedContentProvider struct{}

func (unsupportedContentProvider) Fetch(ctx context.Context, url string, maxLength int) (Result, error) {
	_ = ctx
	_ = url
	_ = maxLength
	return Result{}, UnsupportedContentTypeError{ContentType: "application/pdf"}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func newBasicProviderForTest(fn roundTripFunc) *BasicProvider {
	return &BasicProvider{
		client: &http.Client{Transport: fn},
	}
}

func textResponse(req *http.Request, contentType string, body string) *http.Response {
	return byteResponse(req, contentType, []byte(body))
}

func byteResponse(req *http.Request, contentType string, body []byte) *http.Response {
	header := http.Header{}
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    req,
	}
}
