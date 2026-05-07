package webfetch

import (
	"context"
	"errors"
	"testing"
	"time"

	sharedoutbound "arkloop/services/shared/outboundurl"
	"arkloop/services/worker/internal/tools"
)

func TestParseArgsNormalizesDoubleScheme(t *testing.T) {
	targetURL, maxLength, err := parseArgs(map[string]any{
		"url":        "httpshttps://example.com/a",
		"max_length": 10,
	})
	if err != nil {
		t.Fatalf("parseArgs returned error: %#v", err)
	}
	if targetURL != "https://example.com/a" {
		t.Fatalf("expected url normalized, got %q", targetURL)
	}
	if maxLength != 10 {
		t.Fatalf("expected maxLength=10, got %d", maxLength)
	}
}

func TestParseArgsUnwrapsJinaWrapper(t *testing.T) {
	targetURL, _, err := parseArgs(map[string]any{
		"url":        "https://r.jina.ai/http://example.com/a",
		"max_length": 10,
	})
	if err != nil {
		t.Fatalf("parseArgs returned error: %#v", err)
	}
	if targetURL != "http://example.com/a" {
		t.Fatalf("expected jina wrapper stripped, got %q", targetURL)
	}
}

func TestExecuteUnwrapsJinaWrapperBeforeFetch(t *testing.T) {
	provider := &captureProvider{}
	executor := &ToolExecutor{
		provider: provider,
		timeout:  2 * time.Second,
	}

	result := executor.Execute(
		context.Background(),
		"web_fetch",
		map[string]any{
			"url":        "https://r.jina.ai/https://example.com/a",
			"max_length": 10,
		},
		tools.ExecutionContext{},
		"call_1",
	)
	if result.Error != nil {
		t.Fatalf("expected success, got error: %#v", result.Error)
	}
	if provider.gotURL != "https://example.com/a" {
		t.Fatalf("expected provider to receive unwrapped url, got %q", provider.gotURL)
	}
}

func TestExecuteUnwrapsJinaWrapperBeforePolicyCheck(t *testing.T) {
	t.Setenv(sharedoutbound.ProtectionEnabledEnv, "true")

	provider := &captureProvider{}
	executor := &ToolExecutor{
		provider: provider,
		timeout:  2 * time.Second,
	}

	result := executor.Execute(
		context.Background(),
		"web_fetch",
		map[string]any{
			"url":        "https://r.jina.ai/http://localhost:1234/private",
			"max_length": 10,
		},
		tools.ExecutionContext{},
		"call_1",
	)
	if result.Error == nil {
		t.Fatalf("expected url denied error")
	}
	if result.Error.ErrorClass != errorURLDenied {
		t.Fatalf("unexpected error class: %s", result.Error.ErrorClass)
	}
	if provider.called {
		t.Fatalf("provider should not be called when url is denied")
	}
}

func TestExecuteClassifiesFetchFailures(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		errorClass string
		reason     string
		retryable  bool
	}{
		{
			name:       "timeout",
			err:        context.DeadlineExceeded,
			errorClass: errorTimeout,
			reason:     fetchFailureNetworkTimeout,
			retryable:  true,
		},
		{
			name:       "canceled",
			err:        context.Canceled,
			errorClass: errorFetchFailed,
			reason:     fetchFailureRequestCanceled,
			retryable:  false,
		},
		{
			name:       "dns",
			err:        errors.New("outbound dns resolve failed: lookup httpbin.org: no such host"),
			errorClass: errorFetchFailed,
			reason:     fetchFailureDNSFailed,
			retryable:  true,
		},
		{
			name:       "tls",
			err:        errors.New("net/http: TLS handshake timeout"),
			errorClass: errorFetchFailed,
			reason:     fetchFailureTLSFailed,
			retryable:  true,
		},
		{
			name:       "http status",
			err:        HttpError{StatusCode: 503},
			errorClass: errorFetchFailed,
			reason:     fetchFailureHTTPStatusError,
			retryable:  true,
		},
		{
			name:       "empty page",
			err:        errors.New("web_fetch response body is empty"),
			errorClass: errorFetchFailed,
			reason:     fetchFailureEmptyPage,
			retryable:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := &ToolExecutor{
				provider: errorProvider{err: tt.err},
				timeout:  2 * time.Second,
			}
			result := executor.Execute(
				context.Background(),
				"web_fetch",
				map[string]any{
					"url":        "https://example.com/a",
					"max_length": 10,
				},
				tools.ExecutionContext{},
				"call_1",
			)
			if result.Error == nil {
				t.Fatalf("expected error")
			}
			if result.Error.ErrorClass != tt.errorClass {
				t.Fatalf("unexpected error class: %s", result.Error.ErrorClass)
			}
			if got := result.Error.Details["reason"]; got != tt.reason {
				t.Fatalf("unexpected reason: %#v", got)
			}
			if got := result.Error.Details["retryable"]; got != tt.retryable {
				t.Fatalf("unexpected retryable: %#v", got)
			}
			if status := result.Error.Details["status_code"]; tt.name == "http status" && status != 503 {
				t.Fatalf("unexpected status_code: %#v", status)
			}
		})
	}
}

type captureProvider struct {
	gotURL string
	called bool
}

func (p *captureProvider) Fetch(ctx context.Context, url string, maxLength int) (Result, error) {
	_ = ctx
	_ = maxLength
	p.called = true
	p.gotURL = url
	return Result{
		URL:       url,
		Title:     "t",
		Content:   "c",
		Truncated: false,
	}, nil
}

type errorProvider struct {
	err error
}

func (p errorProvider) Fetch(ctx context.Context, url string, maxLength int) (Result, error) {
	_ = ctx
	_ = url
	_ = maxLength
	return Result{}, p.err
}
