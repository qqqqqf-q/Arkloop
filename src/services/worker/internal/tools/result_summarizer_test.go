package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"arkloop/services/worker/internal/llm"
)

type mockGateway struct {
	response string
	err      error
}

func (m *mockGateway) Stream(_ context.Context, _ llm.Request, yield func(llm.StreamEvent) error) error {
	if m.err != nil {
		return m.err
	}
	if err := yield(llm.StreamMessageDelta{ContentDelta: m.response}); err != nil {
		return err
	}
	return yield(llm.StreamRunCompleted{})
}

func TestResultSummarizer_BelowThreshold(t *testing.T) {
	gw := &mockGateway{response: "summary text"}
	s := NewResultSummarizer(gw, "test-model", 10000, ResultSummarizerConfig{Prompt: "compress", MaxTokens: 32})
	result := ExecutionResult{
		ResultJSON: map[string]any{"output": "small"},
	}
	got := s.Summarize(context.Background(), "test_tool", result)
	if _, ok := got.ResultJSON["_summarized"]; ok {
		t.Fatal("should not summarize when below threshold")
	}
}

func TestResultSummarizer_AboveThreshold(t *testing.T) {
	gw := &mockGateway{response: "key info: result was 42"}
	s := NewResultSummarizer(gw, "test-model", 10, ResultSummarizerConfig{Prompt: "compress", MaxTokens: 32})
	result := ExecutionResult{
		ResultJSON: map[string]any{"output": strings.Repeat("x", 1000)},
	}
	got := s.Summarize(context.Background(), "test_tool", result)
	if got.ResultJSON["_summarized"] != true {
		t.Fatal("expected _summarized flag")
	}
	if got.ResultJSON["summary"] != "key info: result was 42" {
		t.Fatalf("unexpected summary: %v", got.ResultJSON["summary"])
	}
	origBytes, ok := got.ResultJSON["_original_bytes"].(int)
	if !ok || origBytes <= 0 {
		t.Fatal("_original_bytes should be a positive int")
	}
}

func TestResultSummarizer_GatewayError_Fallback(t *testing.T) {
	gw := &mockGateway{err: fmt.Errorf("connection refused")}
	s := NewResultSummarizer(gw, "test-model", 10, ResultSummarizerConfig{Prompt: "compress", MaxTokens: 32})
	original := map[string]any{"output": strings.Repeat("y", 1000)}
	result := ExecutionResult{ResultJSON: original}
	got := s.Summarize(context.Background(), "test_tool", result)
	// Should fall back to original result
	if _, ok := got.ResultJSON["_summarized"]; ok {
		t.Fatal("should not set _summarized on gateway error")
	}
	if got.ResultJSON["output"] == nil {
		t.Fatal("original output should be preserved on fallback")
	}
}

func TestResultSummarizer_EmptySummary_Fallback(t *testing.T) {
	gw := &mockGateway{response: "   "}
	s := NewResultSummarizer(gw, "test-model", 10, ResultSummarizerConfig{Prompt: "compress", MaxTokens: 32})
	original := map[string]any{"output": strings.Repeat("z", 200)}
	result := ExecutionResult{ResultJSON: original}
	got := s.Summarize(context.Background(), "test_tool", result)
	if _, ok := got.ResultJSON["_summarized"]; ok {
		t.Fatal("should not set _summarized when summary is empty")
	}
}

func TestResultSummarizer_PreservesMetaFields(t *testing.T) {
	gw := &mockGateway{response: "summary"}
	s := NewResultSummarizer(gw, "test-model", 10, ResultSummarizerConfig{Prompt: "compress", MaxTokens: 32})
	result := ExecutionResult{
		ResultJSON: map[string]any{"output": strings.Repeat("a", 500)},
		DurationMs: 123,
	}
	got := s.Summarize(context.Background(), "test_tool", result)
	if got.DurationMs != 123 {
		t.Fatal("DurationMs should be preserved")
	}
}

func TestResultSummarizer_DefaultsMaxTokens(t *testing.T) {
	gw := &mockGateway{response: "summary"}
	s := NewResultSummarizer(gw, "test-model", 10, ResultSummarizerConfig{Prompt: "compress"})
	if s.config.MaxTokens != 512 {
		t.Fatalf("expected default max tokens 512, got %d", s.config.MaxTokens)
	}
}
