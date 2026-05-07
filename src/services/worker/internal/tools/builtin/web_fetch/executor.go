package webfetch

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	sharedtoolmeta "arkloop/services/shared/toolmeta"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

const (
	errorArgsInvalid   = "tool.args_invalid"
	errorTimeout       = "tool.timeout"
	errorFetchFailed   = "tool.fetch_failed"
	errorURLDenied     = "tool.url_denied"
	errorNotConfigured = "config.missing"

	defaultTimeout = basicFetchTimeout
	maxLengthLimit = sharedtoolmeta.WebFetchMaxLengthLimit

	fetchFailureDNSFailed              = "dns_failed"
	fetchFailureEmptyPage              = "empty_page"
	fetchFailureHTTPStatusError        = "http_status_error"
	fetchFailureNetworkError           = "network_error"
	fetchFailureNetworkTimeout         = "network_timeout"
	fetchFailureRequestCanceled        = "request_canceled"
	fetchFailureTLSFailed              = "tls_failed"
	fetchFailureUnknown                = "unknown"
	fetchFailureUnsupportedContentType = "unsupported_content_type"
)

var AgentSpec = tools.AgentToolSpec{
	Name:        "web_fetch",
	Version:     "1",
	Description: "fetch web page content and extract body text",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: false,
}

var AgentSpecJina = tools.AgentToolSpec{
	Name:        "web_fetch.jina",
	LlmName:     "web_fetch",
	Version:     "1",
	Description: "fetch web page content and extract body text",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: false,
}

var AgentSpecFirecrawl = tools.AgentToolSpec{
	Name:        "web_fetch.firecrawl",
	LlmName:     "web_fetch",
	Version:     "1",
	Description: "fetch web page content and extract body text",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: false,
}

var AgentSpecBasic = tools.AgentToolSpec{
	Name:        "web_fetch.basic",
	LlmName:     "web_fetch",
	Version:     "1",
	Description: "fetch web page content and extract body text",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: false,
}

var LlmSpec = llm.ToolSpec{
	Name:        "web_fetch",
	Description: stringPtr(sharedtoolmeta.Must("web_fetch").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url":        map[string]any{"type": "string", "description": "URL to fetch"},
			"max_length": map[string]any{"type": "integer", "minimum": 1, "maximum": maxLengthLimit, "description": "maximum number of characters to return"},
		},
		"required":             []string{"url", "max_length"},
		"additionalProperties": false,
	},
}

type ToolExecutor struct {
	provider Provider
	timeout  time.Duration
}

func NewToolExecutor(_ any) *ToolExecutor {
	return &ToolExecutor{timeout: defaultTimeout}
}

func NewBasicExecutor(_ any) *ToolExecutor {
	return &ToolExecutor{timeout: defaultTimeout}
}

func NewFirecrawlExecutor(_ any) *ToolExecutor {
	return &ToolExecutor{timeout: defaultTimeout}
}

func NewJinaExecutor(_ any) *ToolExecutor {
	return &ToolExecutor{timeout: defaultTimeout}
}

func NewToolExecutorWithProvider(provider Provider) *ToolExecutor {
	return &ToolExecutor{
		provider: provider,
		timeout:  defaultTimeout,
	}
}

func (e *ToolExecutor) IsNotConfigured() bool {
	return e == nil || e.provider == nil
}

func (e *ToolExecutor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	_ = toolName
	started := time.Now()

	targetURL, maxLength, argErr := parseArgs(args)
	if argErr != nil {
		return tools.ExecutionResult{
			Error:      argErr,
			DurationMs: durationMs(started),
		}
	}

	provider := e.provider
	if provider == nil {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorNotConfigured,
				Message:    "web_fetch backend not configured",
			},
			DurationMs: durationMs(started),
		}
	}

	if err := EnsureURLAllowed(targetURL); err != nil {
		denied, ok := err.(UrlPolicyDeniedError)
		details := map[string]any{"reason": "unknown"}
		if ok {
			details = map[string]any{"reason": denied.Reason}
			for key, value := range denied.Details {
				details[key] = value
			}
		}
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorURLDenied,
				Message:    "web_fetch URL denied by security policy",
				Details:    details,
			},
			DurationMs: durationMs(started),
		}
	}

	timeout := e.timeout
	if execCtx.TimeoutMs != nil && *execCtx.TimeoutMs > 0 {
		timeout = time.Duration(*execCtx.TimeoutMs) * time.Millisecond
	}
	fetchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := provider.Fetch(fetchCtx, targetURL, maxLength)
	if err != nil {
		var denied UrlPolicyDeniedError
		if errors.As(err, &denied) {
			details := map[string]any{"reason": denied.Reason}
			for key, value := range denied.Details {
				details[key] = value
			}
			return tools.ExecutionResult{
				Error: &tools.ExecutionError{
					ErrorClass: errorURLDenied,
					Message:    "web_fetch URL denied by security policy",
					Details:    details,
				},
				DurationMs: durationMs(started),
			}
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return tools.ExecutionResult{
				Error: &tools.ExecutionError{
					ErrorClass: errorTimeout,
					Message:    "web_fetch timed out",
					Details: map[string]any{
						"reason":          fetchFailureNetworkTimeout,
						"retryable":       true,
						"timeout_seconds": timeout.Seconds(),
					},
				},
				DurationMs: durationMs(started),
			}
		}
		if errors.Is(err, context.Canceled) {
			return tools.ExecutionResult{
				Error: &tools.ExecutionError{
					ErrorClass: errorFetchFailed,
					Message:    "web_fetch request canceled",
					Details: map[string]any{
						"reason":    fetchFailureRequestCanceled,
						"retryable": false,
					},
				},
				DurationMs: durationMs(started),
			}
		}
		var httpErr HttpError
		if errors.As(err, &httpErr) {
			return tools.ExecutionResult{
				Error: &tools.ExecutionError{
					ErrorClass: errorFetchFailed,
					Message:    "web_fetch request failed",
					Details: map[string]any{
						"reason":      fetchFailureHTTPStatusError,
						"retryable":   isRetryableHTTPStatus(httpErr.StatusCode),
						"status_code": httpErr.StatusCode,
					},
				},
				DurationMs: durationMs(started),
			}
		}
		var contentTypeErr UnsupportedContentTypeError
		if errors.As(err, &contentTypeErr) {
			return tools.ExecutionResult{
				Error: &tools.ExecutionError{
					ErrorClass: errorFetchFailed,
					Message:    "web_fetch content type is not readable text",
					Details: map[string]any{
						"reason":       fetchFailureUnsupportedContentType,
						"retryable":    false,
						"content_type": contentTypeErr.ContentType,
					},
				},
				DurationMs: durationMs(started),
			}
		}
		failure := classifyFetchFailure(err)
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorFetchFailed,
				Message:    "web_fetch execution failed",
				Details: map[string]any{
					"reason":    failure.reason,
					"retryable": failure.retryable,
					"error":     err.Error(),
				},
			},
			DurationMs: durationMs(started),
		}
	}

	return tools.ExecutionResult{
		ResultJSON: result.ToJSON(),
		DurationMs: durationMs(started),
	}
}

type fetchFailure struct {
	reason    string
	retryable bool
}

func classifyFetchFailure(err error) fetchFailure {
	if err == nil {
		return fetchFailure{reason: fetchFailureUnknown}
	}
	if errors.Is(err, context.Canceled) {
		return fetchFailure{reason: fetchFailureRequestCanceled}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fetchFailure{reason: fetchFailureNetworkTimeout, retryable: true}
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return fetchFailure{reason: fetchFailureDNSFailed, retryable: dnsErr.IsTimeout || dnsErr.IsTemporary}
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return fetchFailure{reason: fetchFailureNetworkTimeout, retryable: true}
		}
	}

	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "outbound dns resolve failed"),
		strings.Contains(message, "no such host"),
		strings.Contains(message, "server misbehaving"):
		return fetchFailure{reason: fetchFailureDNSFailed, retryable: true}
	case strings.Contains(message, "tls handshake timeout"),
		strings.Contains(message, "tls:"),
		strings.Contains(message, "certificate"),
		strings.Contains(message, "x509:"):
		return fetchFailure{reason: fetchFailureTLSFailed, retryable: strings.Contains(message, "timeout")}
	case strings.Contains(message, "timeout"),
		strings.Contains(message, "deadline exceeded"),
		strings.Contains(message, "i/o timeout"):
		return fetchFailure{reason: fetchFailureNetworkTimeout, retryable: true}
	case strings.Contains(message, "connection reset"),
		strings.Contains(message, "connection refused"),
		strings.Contains(message, "unexpected eof"),
		strings.Contains(message, "eof"),
		strings.Contains(message, "temporary failure"),
		strings.Contains(message, "try again"):
		return fetchFailure{reason: fetchFailureNetworkError, retryable: true}
	case strings.Contains(message, "response body is empty"),
		strings.Contains(message, "html-empty"):
		return fetchFailure{reason: fetchFailureEmptyPage}
	default:
		return fetchFailure{reason: fetchFailureUnknown}
	}
}

func isRetryableHTTPStatus(statusCode int) bool {
	return statusCode == 408 || statusCode == 429 || statusCode == 502 || statusCode == 503 || statusCode == 504
}

func parseArgs(args map[string]any) (string, int, *tools.ExecutionError) {
	unknown := []string{}
	for key := range args {
		if key != "url" && key != "max_length" {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return "", 0, &tools.ExecutionError{
			ErrorClass: errorArgsInvalid,
			Message:    "tool arguments do not allow extra fields",
			Details:    map[string]any{"unknown_fields": unknown},
		}
	}

	rawURL, ok := args["url"].(string)
	if !ok || strings.TrimSpace(rawURL) == "" {
		return "", 0, &tools.ExecutionError{
			ErrorClass: errorArgsInvalid,
			Message:    "parameter url must be a non-empty string",
			Details:    map[string]any{"field": "url"},
		}
	}
	targetURL := normalizeTargetURL(rawURL)

	rawMax, ok := args["max_length"]
	maxLength, okInt := rawMax.(int)
	if !ok || !okInt {
		if floatVal, ok := rawMax.(float64); ok {
			maxLength = int(floatVal)
			okInt = floatVal == float64(maxLength)
		}
	}
	if !okInt {
		return "", 0, &tools.ExecutionError{
			ErrorClass: errorArgsInvalid,
			Message:    "parameter max_length must be an integer",
			Details:    map[string]any{"field": "max_length"},
		}
	}
	if maxLength <= 0 || maxLength > maxLengthLimit {
		return "", 0, &tools.ExecutionError{
			ErrorClass: errorArgsInvalid,
			Message:    fmt.Sprintf("parameter max_length must be in range 1..%d", maxLengthLimit),
			Details:    map[string]any{"field": "max_length", "max": maxLengthLimit},
		}
	}
	return targetURL, maxLength, nil
}

func normalizeTargetURL(raw string) string {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return ""
	}

	cleaned = fixDuplicatedScheme(cleaned)
	cleaned = unwrapJinaWrapper(cleaned)
	cleaned = fixDuplicatedScheme(cleaned)
	return strings.TrimSpace(cleaned)
}

func fixDuplicatedScheme(raw string) string {
	if strings.HasPrefix(raw, "httpshttps://") {
		return "https://" + strings.TrimPrefix(raw, "httpshttps://")
	}
	if strings.HasPrefix(raw, "httphttp://") {
		return "http://" + strings.TrimPrefix(raw, "httphttp://")
	}
	return raw
}

func unwrapJinaWrapper(raw string) string {
	trimmed := strings.TrimSpace(raw)
	for {
		stripped := false
		for _, prefix := range []string{"https://r.jina.ai/", "http://r.jina.ai/"} {
			if strings.HasPrefix(trimmed, prefix) {
				trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
				stripped = true
			}
		}
		if !stripped {
			break
		}
	}
	return trimmed
}

func stringPtr(value string) *string {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}

func durationMs(started time.Time) int {
	elapsed := time.Since(started)
	millis := int(elapsed / time.Millisecond)
	if millis < 0 {
		return 0
	}
	return millis
}

// jinaWithBasicFallback tries Jina (anonymous free tier) first and silently
// falls back to the basic HTTP provider on any non-policy, non-timeout error.
type jinaWithBasicFallback struct {
	jina  Provider
	basic Provider
}

func (p *jinaWithBasicFallback) Fetch(ctx context.Context, url string, maxLength int) (Result, error) {
	result, err := p.jina.Fetch(ctx, url, maxLength)
	if err == nil {
		return result, nil
	}
	// Do not fall back for URL policy violations or context cancellation —
	// those errors would be the same with basic.
	var policyErr UrlPolicyDeniedError
	if errors.As(err, &policyErr) {
		return Result{}, err
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return Result{}, err
	}
	return p.basic.Fetch(ctx, url, maxLength)
}
