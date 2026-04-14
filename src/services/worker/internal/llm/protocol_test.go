package llm

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
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

	anthropicGateway, ok := gateway.(*AnthropicGateway)
	if !ok {
		t.Fatalf("expected AnthropicGateway, got %T", gateway)
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
