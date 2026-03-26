package understandimage

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	sharedoutbound "arkloop/services/shared/outboundurl"
	sharedtoolmeta "arkloop/services/shared/toolmeta"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

const (
	errorArgsInvalid      = "tool.args_invalid"
	errorFetchFailed      = "tool.fetch_failed"
	errorNotConfigured    = "config.missing"
	errorProviderFailed   = "external_provider.failed"
	errorTimeout          = "tool.timeout"
	errorTooLarge         = "tool.payload_too_large"
	errorUnsupportedMedia = "tool.unsupported_media_type"
	errorURLDenied        = "tool.url_denied"
	defaultTimeout        = 30 * time.Second
	defaultMaxBytes       = 20 * 1024 * 1024
	maxTimeoutMs          = 120000
)

var AgentSpec = tools.AgentToolSpec{
	Name:        "understand_image",
	Version:     "1",
	Description: "fetch a remote image and return a textual understanding result",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: false,
}

var AgentSpecMiniMax = tools.AgentToolSpec{
	Name:        ProviderNameMiniMax,
	LlmName:     "understand_image",
	Version:     "1",
	Description: "fetch a remote image and return a textual understanding result",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: false,
}

var LlmSpec = llm.ToolSpec{
	Name:        "understand_image",
	Description: stringPtr(sharedtoolmeta.Must("understand_image").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "remote http/https image URL",
			},
			"prompt": map[string]any{
				"type":        "string",
				"description": "what to analyze in the image",
			},
			"max_bytes": map[string]any{
				"type":        "integer",
				"description": fmt.Sprintf("maximum image size in bytes (default %d)", defaultMaxBytes),
				"default":     defaultMaxBytes,
				"minimum":     1,
				"maximum":     defaultMaxBytes,
			},
			"timeout_ms": map[string]any{
				"type":        "integer",
				"description": "optional timeout override in milliseconds",
				"minimum":     1,
				"maximum":     maxTimeoutMs,
			},
		},
		"required":             []string{"url", "prompt"},
		"additionalProperties": false,
	},
}

type ToolExecutor struct {
	provider Provider
	timeout  time.Duration
}

func NewToolExecutor() *ToolExecutor {
	return &ToolExecutor{timeout: defaultTimeout}
}

func NewToolExecutorWithProvider(provider Provider) *ToolExecutor {
	return &ToolExecutor{provider: provider, timeout: defaultTimeout}
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

	targetURL, prompt, maxBytes, timeoutOverride, argErr := parseArgs(args)
	if argErr != nil {
		return tools.ExecutionResult{Error: argErr, DurationMs: durationMs(started)}
	}
	if e == nil || e.provider == nil {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorNotConfigured,
				Message:    "understand_image backend not configured",
				Details:    map[string]any{"group_name": GroupName},
			},
			DurationMs: durationMs(started),
		}
	}

	if err := sharedoutbound.DefaultPolicy().ValidateRequestURL(targetURL); err != nil {
		return tools.ExecutionResult{
			Error:      executionErrorFromFetchError(err),
			DurationMs: durationMs(started),
		}
	}

	timeout := resolveTimeout(e.timeout, execCtx.TimeoutMs, timeoutOverride)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	image, err := fetchRemoteImage(runCtx, targetURL, maxBytes)
	if err != nil {
		return tools.ExecutionResult{
			Error:      executionErrorFromFetchError(err),
			DurationMs: durationMs(started),
		}
	}

	description, err := e.provider.DescribeImage(runCtx, DescribeImageRequest{
		Prompt:    prompt,
		SourceURL: image.FinalURL,
		MimeType:  image.MimeType,
		Bytes:     image.Bytes,
	})
	if err != nil {
		return tools.ExecutionResult{
			Error:      executionErrorFromProviderError(err),
			DurationMs: durationMs(started),
		}
	}

	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"text":       description.Text,
			"provider":   description.Provider,
			"model":      description.Model,
			"source_url": image.FinalURL,
			"mime_type":  image.MimeType,
			"bytes":      len(image.Bytes),
		},
		DurationMs: durationMs(started),
	}
}

func parseArgs(args map[string]any) (string, string, int, *int, *tools.ExecutionError) {
	unknown := make([]string, 0)
	for key := range args {
		if key != "url" && key != "prompt" && key != "max_bytes" && key != "timeout_ms" {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return "", "", 0, nil, &tools.ExecutionError{
			ErrorClass: errorArgsInvalid,
			Message:    "tool arguments do not allow extra fields",
			Details:    map[string]any{"unknown_fields": unknown},
		}
	}

	targetURL, ok := args["url"].(string)
	if !ok || strings.TrimSpace(targetURL) == "" {
		return "", "", 0, nil, requiredStringArgError("url")
	}
	prompt, ok := args["prompt"].(string)
	if !ok || strings.TrimSpace(prompt) == "" {
		return "", "", 0, nil, requiredStringArgError("prompt")
	}

	maxBytes := defaultMaxBytes
	if raw, exists := args["max_bytes"]; exists {
		value, ok := intArg(raw)
		if !ok || value <= 0 || value > defaultMaxBytes {
			return "", "", 0, nil, &tools.ExecutionError{
				ErrorClass: errorArgsInvalid,
				Message:    fmt.Sprintf("parameter max_bytes must be in range 1..%d", defaultMaxBytes),
				Details:    map[string]any{"field": "max_bytes", "max": defaultMaxBytes},
			}
		}
		maxBytes = value
	}

	var timeoutOverride *int
	if raw, exists := args["timeout_ms"]; exists {
		value, ok := intArg(raw)
		if !ok || value <= 0 || value > maxTimeoutMs {
			return "", "", 0, nil, &tools.ExecutionError{
				ErrorClass: errorArgsInvalid,
				Message:    fmt.Sprintf("parameter timeout_ms must be in range 1..%d", maxTimeoutMs),
				Details:    map[string]any{"field": "timeout_ms", "max": maxTimeoutMs},
			}
		}
		timeoutOverride = &value
	}

	return strings.TrimSpace(targetURL), strings.TrimSpace(prompt), maxBytes, timeoutOverride, nil
}

func requiredStringArgError(field string) *tools.ExecutionError {
	return &tools.ExecutionError{
		ErrorClass: errorArgsInvalid,
		Message:    fmt.Sprintf("parameter %s must be a non-empty string", field),
		Details:    map[string]any{"field": field},
	}
}

func intArg(raw any) (int, bool) {
	switch value := raw.(type) {
	case int:
		return value, true
	case float64:
		if value != float64(int(value)) {
			return 0, false
		}
		return int(value), true
	default:
		return 0, false
	}
}

func resolveTimeout(base time.Duration, inherited *int, override *int) time.Duration {
	timeout := base
	if inherited != nil && *inherited > 0 {
		timeout = time.Duration(*inherited) * time.Millisecond
	}
	if override != nil && *override > 0 {
		overrideDuration := time.Duration(*override) * time.Millisecond
		if timeout <= 0 || overrideDuration < timeout {
			timeout = overrideDuration
		}
	}
	return timeout
}

func executionErrorFromFetchError(err error) *tools.ExecutionError {
	if err == nil {
		return nil
	}
	var denied sharedoutbound.DeniedError
	if errors.As(err, &denied) {
		details := map[string]any{"reason": denied.Reason}
		for key, value := range denied.Details {
			details[key] = value
		}
		return &tools.ExecutionError{
			ErrorClass: errorURLDenied,
			Message:    "understand_image URL denied by security policy",
			Details:    details,
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &tools.ExecutionError{
			ErrorClass: errorTimeout,
			Message:    "understand_image timed out",
		}
	}
	var tooLarge imageTooLargeError
	if errors.As(err, &tooLarge) {
		return &tools.ExecutionError{
			ErrorClass: errorTooLarge,
			Message:    "image exceeds max_bytes limit",
			Details:    map[string]any{"max_bytes": tooLarge.MaxBytes},
		}
	}
	var unsupported unsupportedMediaTypeError
	if errors.As(err, &unsupported) {
		return &tools.ExecutionError{
			ErrorClass: errorUnsupportedMedia,
			Message:    "URL did not resolve to a supported image",
			Details:    map[string]any{"mime_type": unsupported.DetectedMimeType},
		}
	}
	var statusErr httpStatusError
	if errors.As(err, &statusErr) {
		return &tools.ExecutionError{
			ErrorClass: errorFetchFailed,
			Message:    "understand_image request failed",
			Details:    map[string]any{"status_code": statusErr.StatusCode},
		}
	}
	return &tools.ExecutionError{
		ErrorClass: errorFetchFailed,
		Message:    "understand_image fetch failed",
		Details:    map[string]any{"reason": err.Error()},
	}
}

func executionErrorFromProviderError(err error) *tools.ExecutionError {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &tools.ExecutionError{
			ErrorClass: errorTimeout,
			Message:    "understand_image timed out",
		}
	}
	details := map[string]any{"provider": "minimax"}
	var providerErr ProviderError
	if errors.As(err, &providerErr) {
		if providerErr.StatusCode > 0 {
			details["status_code"] = providerErr.StatusCode
		}
		if strings.TrimSpace(providerErr.TraceID) != "" {
			details["trace_id"] = providerErr.TraceID
		}
	}
	details["reason"] = err.Error()
	return &tools.ExecutionError{
		ErrorClass: errorProviderFailed,
		Message:    "image understanding provider failed",
		Details:    details,
	}
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
	if elapsed < 0 {
		return 0
	}
	return int(elapsed / time.Millisecond)
}
