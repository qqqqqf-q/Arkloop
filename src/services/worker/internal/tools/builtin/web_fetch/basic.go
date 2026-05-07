package webfetch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html/charset"
)

const (
	basicFetchMaxResponseBytes = 5_000_000
	basicFetchMaxAttempts      = 3
	basicFetchTimeout          = 30 * time.Second
	basicFetchUserAgent        = "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko) ArkloopWebFetch/1.0 Safari/537.36"
)

type BasicProvider struct {
	client *http.Client
}

func NewBasicProvider() *BasicProvider {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = SafeDialContext(dialer)
	return &BasicProvider{
		client: &http.Client{
			// The tool executor owns the request deadline; do not let the client
			// cut off a longer caller-provided timeout first.
			Timeout:   0,
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("web_fetch redirect limit exceeded")
				}
				return EnsureURLAllowed(req.URL.String())
			},
		},
	}
}

func (p *BasicProvider) Fetch(ctx context.Context, targetURL string, maxLength int) (Result, error) {
	if err := EnsureURLAllowed(targetURL); err != nil {
		return Result{}, err
	}

	var resp *http.Response
	for attempt := 1; attempt <= basicFetchMaxAttempts; attempt++ {
		req, err := newBasicRequest(ctx, targetURL)
		if err != nil {
			return Result{}, err
		}
		resp, err = p.client.Do(req)
		if err == nil {
			break
		}
		if attempt == basicFetchMaxAttempts || !shouldRetryBasicFetch(ctx, err) {
			return Result{}, err
		}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, HttpError{StatusCode: resp.StatusCode}
	}

	body, responseTruncated, err := readLimitedBody(resp.Body, basicFetchMaxResponseBytes)
	if err != nil {
		return Result{}, err
	}

	finalURL := targetURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	if err := EnsureURLAllowed(finalURL); err != nil {
		return Result{}, err
	}

	extracted, err := extractBasicResponse(body, resp.Header.Get("Content-Type"), finalURL)
	if err != nil {
		return Result{}, err
	}

	content, contentTruncated := truncateString(extracted.Content, maxLength)
	title, _ := truncateString(extracted.Title, 512)
	rawLength := extracted.RawLength
	if rawLength <= 0 {
		rawLength = utf8.RuneCountInString(extracted.Content)
	}

	return Result{
		URL:          finalURL,
		RequestedURL: targetURL,
		Title:        title,
		Content:      content,
		Truncated:    responseTruncated || contentTruncated,
		StatusCode:   resp.StatusCode,
		ContentType:  extracted.ContentType,
		Extractor:    extracted.Extractor,
		RawLength:    rawLength,
	}, nil
}

func newBasicRequest(ctx context.Context, targetURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/markdown, text/html;q=0.9, application/json;q=0.8, text/plain;q=0.7, */*;q=0.1")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("User-Agent", basicFetchUserAgent)
	return req, nil
}

func shouldRetryBasicFetch(ctx context.Context, err error) bool {
	if err == nil || (ctx != nil && ctx.Err() != nil) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	message := strings.ToLower(err.Error())
	transientFragments := []string{
		"tls handshake timeout",
		"timeout awaiting response headers",
		"connection reset",
		"connection refused",
		"i/o timeout",
		"unexpected eof",
		"eof",
		"outbound dns resolve failed",
		"no such host",
		"server misbehaving",
		"temporary failure",
		"try again",
	}
	for _, fragment := range transientFragments {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

type basicExtractedResponse struct {
	Title       string
	Content     string
	ContentType string
	Extractor   string
	RawLength   int
}

func readLimitedBody(reader io.Reader, maxBytes int64) ([]byte, bool, error) {
	if maxBytes <= 0 {
		return nil, false, errors.New("max bytes must be positive")
	}
	body, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(body)) <= maxBytes {
		return body, false, nil
	}
	return body[:maxBytes], true, nil
}

func extractBasicResponse(body []byte, contentTypeHeader string, finalURL string) (basicExtractedResponse, error) {
	contentType := normalizeResponseContentType(contentTypeHeader, body)
	mediaType := mediaTypeOnly(contentType)
	if isBinaryContentType(mediaType) || looksBinary(body) {
		return basicExtractedResponse{}, UnsupportedContentTypeError{ContentType: contentType}
	}

	text, err := decodeResponseText(body, contentType)
	if err != nil {
		return basicExtractedResponse{}, err
	}
	text = stripInvisibleUnicode(strings.TrimSpace(strings.ToValidUTF8(text, "")))
	rawLength := utf8.RuneCountInString(text)

	switch {
	case isHTMLContentType(mediaType) || looksLikeHTML(text):
		content, title, extractor := extractHTMLContent(text, finalURL)
		return basicExtractedResponse{
			Title:       title,
			Content:     content,
			ContentType: contentType,
			Extractor:   extractor,
			RawLength:   rawLength,
		}, nil
	case isJSONContentType(mediaType) || looksLikeJSON(text):
		content := formatJSONText(text)
		return basicExtractedResponse{
			Content:     content,
			ContentType: contentType,
			Extractor:   "json",
			RawLength:   utf8.RuneCountInString(content),
		}, nil
	case isMarkdownContentType(mediaType):
		return basicExtractedResponse{
			Title:       extractTitleFromMarkdown(text),
			Content:     normalizeMarkdownText(text),
			ContentType: contentType,
			Extractor:   "markdown",
			RawLength:   rawLength,
		}, nil
	default:
		content := normalizePlainText(text)
		if content == "" {
			return basicExtractedResponse{}, errors.New("web_fetch response body is empty")
		}
		return basicExtractedResponse{
			Content:     content,
			ContentType: contentType,
			Extractor:   "text",
			RawLength:   rawLength,
		}, nil
	}
}

func decodeResponseText(body []byte, contentType string) (string, error) {
	reader, err := charset.NewReader(bytes.NewReader(body), contentType)
	if err != nil {
		if utf8.Valid(body) {
			return string(body), nil
		}
		return string(bytes.ToValidUTF8(body, nil)), nil
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func normalizeResponseContentType(header string, body []byte) string {
	contentType := strings.TrimSpace(header)
	if contentType == "" {
		contentType = http.DetectContentType(body)
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(contentType))
	}
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if charsetName := strings.TrimSpace(params["charset"]); charsetName != "" {
		return mediaType + "; charset=" + strings.ToLower(charsetName)
	}
	return mediaType
}

func mediaTypeOnly(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil {
		return strings.ToLower(strings.TrimSpace(contentType))
	}
	return strings.ToLower(strings.TrimSpace(mediaType))
}

func isHTMLContentType(mediaType string) bool {
	return mediaType == "text/html" || mediaType == "application/xhtml+xml"
}

func isMarkdownContentType(mediaType string) bool {
	return mediaType == "text/markdown" || mediaType == "text/x-markdown"
}

func isJSONContentType(mediaType string) bool {
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

func isBinaryContentType(mediaType string) bool {
	if mediaType == "" {
		return false
	}
	if strings.HasPrefix(mediaType, "text/") {
		return false
	}
	if isHTMLContentType(mediaType) || isJSONContentType(mediaType) || strings.HasSuffix(mediaType, "+xml") ||
		mediaType == "application/xml" || mediaType == "application/javascript" || mediaType == "application/x-javascript" {
		return false
	}
	binaryPrefixes := []string{"image/", "audio/", "video/", "font/"}
	for _, prefix := range binaryPrefixes {
		if strings.HasPrefix(mediaType, prefix) {
			return true
		}
	}
	switch mediaType {
	case "application/octet-stream", "application/pdf", "application/zip", "application/gzip",
		"application/x-tar", "application/x-7z-compressed", "application/x-rar-compressed":
		return true
	default:
		return false
	}
}

func looksBinary(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	sample := body
	if len(sample) > 4096 {
		sample = sample[:4096]
	}
	for _, b := range sample {
		if b == 0 {
			return true
		}
	}
	return false
}

func looksLikeHTML(text string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(text))
	return strings.HasPrefix(trimmed, "<!doctype html") ||
		strings.HasPrefix(trimmed, "<html") ||
		strings.Contains(trimmed[:min(len(trimmed), 2048)], "<body") ||
		strings.Contains(trimmed[:min(len(trimmed), 2048)], "<article")
}

func looksLikeJSON(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	first := trimmed[0]
	return first == '{' || first == '['
}

func formatJSONText(text string) string {
	var parsed any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return normalizePlainText(text)
	}
	encoded, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return normalizePlainText(text)
	}
	return string(encoded)
}

func normalizeMarkdownText(text string) string {
	return normalizeBlockWhitespace(text)
}

func normalizePlainText(text string) string {
	return normalizeBlockWhitespace(text)
}

func stripInvisibleUnicode(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for _, r := range value {
		if r == '\u200b' || r == '\u200c' || r == '\u200d' || r == '\ufeff' {
			continue
		}
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func normalizeInlineWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func normalizeBlockWhitespace(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	lines := strings.Split(value, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		cleaned := normalizeInlineWhitespace(line)
		if cleaned == "" {
			if !blank && len(out) > 0 {
				out = append(out, "")
				blank = true
			}
			continue
		}
		out = append(out, cleaned)
		blank = false
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func absoluteURL(baseURL string, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if parsed.IsAbs() {
		return parsed.String()
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return raw
	}
	return base.ResolveReference(parsed).String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
