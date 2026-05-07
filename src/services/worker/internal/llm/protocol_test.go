package llm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sharedoutbound "arkloop/services/shared/outboundurl"
)

func TestResolveOpenAIProtocolConfig_AutoAddsFallback(t *testing.T) {
	cfg, err := ResolveOpenAIProtocolConfig("auto", map[string]any{
		"metadata": map[string]any{"source": "test"},
	})
	if err != nil {
		t.Fatalf("ResolveOpenAIProtocolConfig returned error: %v", err)
	}
	if cfg.PrimaryKind != ProtocolKindOpenAIResponses {
		t.Fatalf("unexpected primary protocol kind: %s", cfg.PrimaryKind)
	}
	if cfg.FallbackKind == nil || *cfg.FallbackKind != ProtocolKindOpenAIChatCompletions {
		t.Fatalf("unexpected fallback protocol kind: %#v", cfg.FallbackKind)
	}
	if cfg.AdvancedPayloadJSON["metadata"] == nil {
		t.Fatalf("expected advanced payload to be preserved, got %#v", cfg.AdvancedPayloadJSON)
	}
}

func TestResolveAnthropicProtocolConfig_SeparatesHeadersFromPayload(t *testing.T) {
	cfg, err := ResolveAnthropicProtocolConfig(map[string]any{
		"anthropic_version": "2023-06-01",
		"extra_headers": map[string]any{
			"anthropic-beta": "tools-2024-04-04",
		},
		"thinking": map[string]any{
			"type":          "enabled",
			"budget_tokens": 1024,
			"signature":     "ignored-by-provider",
		},
	})
	if err != nil {
		t.Fatalf("ResolveAnthropicProtocolConfig returned error: %v", err)
	}
	if cfg.Version != "2023-06-01" {
		t.Fatalf("unexpected anthropic version: %q", cfg.Version)
	}
	if cfg.ExtraHeaders["anthropic-beta"] != "tools-2024-04-04" {
		t.Fatalf("unexpected anthropic headers: %#v", cfg.ExtraHeaders)
	}
	if cfg.AdvancedPayloadJSON["thinking"] == nil {
		t.Fatalf("expected thinking payload to remain in protocol payload: %#v", cfg.AdvancedPayloadJSON)
	}
}

func TestNewGatewayFromResolvedConfig_AnthropicUsesExplicitPathBase(t *testing.T) {
	gateway, err := NewGatewayFromResolvedConfig(ResolvedGatewayConfig{
		ProtocolKind: ProtocolKindAnthropicMessages,
		Model:        "MiniMax-M2.7",
		Transport: TransportConfig{
			APIKey:  "test",
			BaseURL: "https://api.minimaxi.com/anthropic/v1",
		},
		Anthropic: &AnthropicProtocolConfig{
			Version:             defaultAnthropicVersion,
			ExtraHeaders:        map[string]string{},
			AdvancedPayloadJSON: map[string]any{},
		},
	})
	if err != nil {
		t.Fatalf("NewGatewayFromResolvedConfig returned error: %v", err)
	}

	anthropicGateway, ok := gateway.(*anthropicSDKGateway)
	if !ok {
		t.Fatalf("expected anthropicSDKGateway, got %T", gateway)
	}
	if anthropicGateway.ProtocolKind() != ProtocolKindAnthropicMessages {
		t.Fatalf("unexpected protocol kind: %s", anthropicGateway.ProtocolKind())
	}
	if anthropicGateway.transport.cfg.BaseURL != "https://api.minimaxi.com/anthropic" {
		t.Fatalf("unexpected normalized base url: %q", anthropicGateway.transport.cfg.BaseURL)
	}
	if path := anthropicGateway.transport.endpoint("/v1/messages"); path != "https://api.minimaxi.com/anthropic/v1/messages" {
		t.Fatalf("unexpected anthropic endpoint: %q", path)
	}
}

func TestProtocolHTTPClientAllowsLoopbackWhenProtectionDisabled(t *testing.T) {
	t.Setenv(sharedoutbound.ProtectionEnabledEnv, "false")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := newProtocolHTTPClient(sharedoutbound.DefaultPolicy(), time.Second)
	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("unexpected status: %s", resp.Status)
	}
}

func TestProtocolHTTPClientRejectsLoopbackWhenProtectionEnabled(t *testing.T) {
	t.Setenv(sharedoutbound.ProtectionEnabledEnv, "true")
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "false")

	client := newProtocolHTTPClient(sharedoutbound.DefaultPolicy(), time.Second)
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:1/v1/models", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	_, err = client.Do(req)
	assertOutboundDeniedReason(t, err, "insecure_scheme_denied")
}

func assertOutboundDeniedReason(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	var denied sharedoutbound.DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected DeniedError, got %T: %v", err, err)
	}
	if denied.Reason != want {
		t.Fatalf("Reason = %q, want %q", denied.Reason, want)
	}
}

func TestGeminiAPIVersionFromBaseURL(t *testing.T) {
	if got := geminiAPIVersionFromBaseURL("https://generativelanguage.googleapis.com/v1"); got != "v1" {
		t.Fatalf("unexpected version for v1 base: %q", got)
	}
	if got := geminiAPIVersionFromBaseURL("https://generativelanguage.googleapis.com/v1beta1"); got != "v1beta1" {
		t.Fatalf("unexpected version for v1beta1 base: %q", got)
	}
	if got := geminiAPIVersionFromBaseURL("https://generativelanguage.googleapis.com"); got != "" {
		t.Fatalf("unexpected version for unversioned base: %q", got)
	}
}

func TestWithStreamIdleTimeoutResetsOnActivity(t *testing.T) {
	ctx, stop, markActivity := withStreamIdleTimeout(context.Background(), 25*time.Millisecond)
	defer stop()

	time.Sleep(10 * time.Millisecond)
	markActivity()
	time.Sleep(10 * time.Millisecond)
	markActivity()

	select {
	case <-ctx.Done():
		t.Fatalf("stream timer should stay alive after activity, got %v", context.Cause(ctx))
	default:
	}

	time.Sleep(35 * time.Millisecond)
	if !errors.Is(context.Cause(ctx), errStreamIdleTimeout) {
		t.Fatalf("expected idle timeout cause, got %v", context.Cause(ctx))
	}
}

func TestForEachSSEDataOnlyTimesOutWhenSilent(t *testing.T) {
	ctx, stop, markActivity := withStreamIdleTimeout(context.Background(), 20*time.Millisecond)
	defer stop()

	reader := &timedChunkReader{
		ctx: ctx,
		steps: []timedChunkStep{
			{delay: 5 * time.Millisecond, data: "data: first\n"},
			{delay: 5 * time.Millisecond, data: "\n"},
			{delay: 30 * time.Millisecond, data: "data: second\n"},
		},
	}

	var got []string
	err := forEachSSEData(ctx, reader, markActivity, func(data string) error {
		got = append(got, data)
		return nil
	})
	if !errors.Is(err, errStreamIdleTimeout) {
		t.Fatalf("expected idle timeout, got %v", err)
	}
	if len(got) != 1 || got[0] != "first" {
		t.Fatalf("unexpected streamed data before timeout: %#v", got)
	}
}

func TestProviderResponseCaptureKeepsBoundedTail(t *testing.T) {
	capture := newProviderResponseCapture()
	capture.setResponse(&http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": {"text/event-stream"},
			"X-Request-Id": {"req_tail_1"},
		},
	})

	body := &providerResponseTailBody{
		body:    io.NopCloser(strings.NewReader("prefix-" + strings.Repeat("x", providerResponseTailMaxBytes) + "tail")),
		capture: capture,
	}
	if _, err := io.Copy(io.Discard, body); err != nil {
		t.Fatalf("copy body: %v", err)
	}
	if err := body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}

	details := capture.details()
	if details["status_code"] != http.StatusOK || details["content_type"] != "text/event-stream" || details["provider_request_id"] != "req_tail_1" {
		t.Fatalf("missing response metadata: %#v", details)
	}
	tail, _ := details["provider_response_tail"].(string)
	if !strings.HasSuffix(tail, "tail") || strings.Contains(tail, "prefix-") || len([]byte(tail)) > providerResponseTailMaxBytes {
		t.Fatalf("unexpected bounded tail: %q", tail)
	}
	if details["provider_response_tail_truncated"] != true {
		t.Fatalf("expected truncated tail flag: %#v", details)
	}
}

func TestSSEKeepaliveFilterDropsCommentOnlyBlocks(t *testing.T) {
	capture := newProviderResponseCapture()
	activityCount := 0
	rawBody := ": PROCESSING\n\ndata: {\"ok\":true}\n\n"
	body := &providerResponseTailBody{
		body:         io.NopCloser(strings.NewReader(rawBody)),
		capture:      capture,
		markActivity: func() { activityCount++ },
	}
	filtered := newSSEKeepaliveFilterReadCloser(body)

	out, err := io.ReadAll(filtered)
	if err != nil {
		t.Fatalf("read filtered body: %v", err)
	}
	if string(out) != "data: {\"ok\":true}\n\n" {
		t.Fatalf("unexpected filtered body: %q", out)
	}
	tail, _ := capture.details()["provider_response_tail"].(string)
	if !strings.Contains(tail, ": PROCESSING") || !strings.Contains(tail, `{"ok":true}`) {
		t.Fatalf("raw tail was not captured: %#v", capture.details())
	}
	if activityCount == 0 {
		t.Fatalf("expected raw keepalive bytes to mark stream activity")
	}
}

func TestMergeContextErrorDetailsIncludesCause(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errStreamIdleTimeout)

	details := mergeContextErrorDetails(map[string]any{}, context.Canceled, ctx)
	if details["context_canceled"] != true || details["context_done"] != true || details["stream_idle_timeout"] != true {
		t.Fatalf("missing context diagnostics: %#v", details)
	}
	if details["context_error"] != context.Canceled.Error() || details["context_cause"] != errStreamIdleTimeout.Error() {
		t.Fatalf("unexpected context diagnostics: %#v", details)
	}
}

type timedChunkStep struct {
	delay time.Duration
	data  string
}

type timedChunkReader struct {
	ctx    context.Context
	steps  []timedChunkStep
	index  int
	offset int
}

func (r *timedChunkReader) Read(p []byte) (int, error) {
	if r.index < len(r.steps) {
		step := r.steps[r.index]
		if r.offset == 0 && step.delay > 0 {
			timer := time.NewTimer(step.delay)
			select {
			case <-r.ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return 0, streamContextError(r.ctx, context.Cause(r.ctx))
			case <-timer.C:
			}
		}
	}
	if r.index >= len(r.steps) {
		return 0, io.EOF
	}
	step := r.steps[r.index]
	n := copy(p, step.data[r.offset:])
	r.offset += n
	if r.offset >= len(step.data) {
		r.index++
		r.offset = 0
	}
	return n, nil
}
