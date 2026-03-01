package webfetch

import (
	"context"
	"testing"
	"time"

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
