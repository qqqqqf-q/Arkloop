package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"arkloop/services/worker/internal/llm"
)

const resultSummarizerTimeout = 30 * time.Second
const resultSummarizerTemperature = 0.2

type ResultSummarizerConfig struct {
	Prompt    string
	MaxTokens int
}

// ResultSummarizer calls an LLM to summarize oversized tool output.
type ResultSummarizer struct {
	gateway   llm.Gateway
	model     string
	threshold int
	config    ResultSummarizerConfig
}

func NewResultSummarizer(gateway llm.Gateway, model string, threshold int, config ResultSummarizerConfig) *ResultSummarizer {
	if config.MaxTokens <= 0 {
		config.MaxTokens = 512
	}
	return &ResultSummarizer{
		gateway:   gateway,
		model:     model,
		threshold: threshold,
		config:    config,
	}
}

// Summarize replaces result.ResultJSON with an LLM-generated summary when its
// marshaled size exceeds the threshold. On failure, the original (Layer-1-truncated)
// result is returned unchanged.
func (s *ResultSummarizer) Summarize(ctx context.Context, toolName string, result ExecutionResult) ExecutionResult {
	raw, err := json.Marshal(result.ResultJSON)
	if err != nil || len(raw) <= s.threshold {
		return result
	}
	originalBytes := len(raw)

	ctx, cancel := context.WithTimeout(ctx, resultSummarizerTimeout)
	defer cancel()

	req := llm.Request{
		Model: s.model,
		Messages: []llm.Message{
			{Role: "system", Content: []llm.TextPart{{Text: buildSummarizerSystemPrompt(s.config.Prompt)}}},
			{Role: "user", Content: []llm.TextPart{{Text: buildSummarizerUserPrompt(toolName, string(raw))}}},
		},
		Temperature:     floatPtr(resultSummarizerTemperature),
		MaxOutputTokens: intPtr(s.config.MaxTokens),
		ReasoningMode:   "disabled",
	}

	var chunks []string
	sentinel := fmt.Errorf("done")

	streamErr := s.gateway.Stream(ctx, req, func(ev llm.StreamEvent) error {
		switch typed := ev.(type) {
		case llm.StreamMessageDelta:
			if typed.Channel != nil && *typed.Channel == "thinking" {
				return nil
			}
			if typed.ContentDelta != "" {
				chunks = append(chunks, typed.ContentDelta)
			}
		case llm.StreamRunCompleted, llm.StreamRunFailed:
			return sentinel
		}
		return nil
	})

	if streamErr != nil && streamErr != sentinel {
		slog.Warn("result_summarizer: llm call failed", "tool", toolName, "err", streamErr.Error())
		return result
	}

	summary := strings.TrimSpace(strings.Join(chunks, ""))
	if summary == "" {
		return result
	}

	return ExecutionResult{
		ResultJSON: map[string]any{
			"summary":         summary,
			"_summarized":     true,
			"_original_bytes": originalBytes,
		},
		Error:      result.Error,
		DurationMs: result.DurationMs,
		Usage:      result.Usage,
		Events:     result.Events,
	}
}

func buildSummarizerSystemPrompt(prompt string) string {
	base := "You are a tool output compressor.\n" +
		"Return only a concise plain-text summary.\n" +
		"Preserve numbers, file paths, identifiers, status codes, key outputs, and exact error messages.\n" +
		"Remove repetitive logs, boilerplate, and formatting noise."
	if strings.TrimSpace(prompt) != "" {
		return base + "\n" + strings.TrimSpace(prompt)
	}
	return base
}

func buildSummarizerUserPrompt(toolName, resultJSON string) string {
	return "Tool: " + toolName + "\nOutput:\n" + resultJSON
}

func intPtr(v int) *int {
	return &v
}

func floatPtr(v float64) *float64 {
	return &v
}
