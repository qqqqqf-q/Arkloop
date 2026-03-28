package understandimage

import (
	"context"
	"errors"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
	read "arkloop/services/worker/internal/tools/builtin/read"
)

const (
	errorUnsupportedMedia = "tool.unsupported_media_type"
	errorTooLarge         = "tool.payload_too_large"
	errorProviderFailed   = "external_provider.failed"
)

// Legacy compatibility surface for internal imports.
// Product entrypoints now use the unified read tool.
var AgentSpec = tools.AgentToolSpec{
	Name:        "understand_image",
	Version:     "1",
	Description: "legacy alias of read for remote image URLs",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var AgentSpecMiniMax = tools.AgentToolSpec{
	Name:        ProviderNameMiniMax,
	LlmName:     "understand_image",
	Version:     "1",
	Description: "legacy minimax alias of read",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var LlmSpec = llm.ToolSpec{
	Name:        "understand_image",
	Description: strPtr("legacy alias of read for remote image URL understanding"),
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
				"description": "maximum image size in bytes (default 20971520)",
				"default":     20971520,
				"minimum":     1,
				"maximum":     20971520,
			},
			"timeout_ms": map[string]any{
				"type":        "integer",
				"description": "optional timeout override in milliseconds",
				"minimum":     1,
				"maximum":     120000,
			},
		},
		"required":             []string{"url", "prompt"},
		"additionalProperties": false,
	},
}

type ToolExecutor struct {
	delegate *read.Executor
}

func NewToolExecutor() *ToolExecutor {
	return &ToolExecutor{delegate: read.NewToolExecutor()}
}

func NewToolExecutorWithProvider(provider Provider) *ToolExecutor {
	return &ToolExecutor{delegate: read.NewToolExecutorWithProvider(providerAdapter{inner: provider})}
}

func (e *ToolExecutor) IsNotConfigured() bool {
	return e == nil || e.delegate == nil
}

func (e *ToolExecutor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	toolCallID string,
) tools.ExecutionResult {
	_ = toolName
	if e == nil || e.delegate == nil {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: "config.missing",
				Message:    "understand_image backend not configured",
				Details:    map[string]any{"group_name": GroupName},
			},
		}
	}

	translated := map[string]any{
		"source": map[string]any{
			"kind": "remote_url",
			"url":  args["url"],
		},
		"prompt": args["prompt"],
	}
	if raw, ok := args["max_bytes"]; ok {
		translated["max_bytes"] = raw
	}
	if raw, ok := args["timeout_ms"]; ok {
		translated["timeout_ms"] = raw
	}
	return e.delegate.Execute(ctx, "read", translated, execCtx, toolCallID)
}

type providerAdapter struct {
	inner Provider
}

func (p providerAdapter) DescribeImage(ctx context.Context, req read.DescribeImageRequest) (read.DescribeImageResponse, error) {
	resp, err := p.inner.DescribeImage(ctx, DescribeImageRequest{
		Prompt:    req.Prompt,
		SourceURL: req.SourceURL,
		MimeType:  req.MimeType,
		Bytes:     req.Bytes,
	})
	if err != nil {
		var providerErr ProviderError
		if errors.As(err, &providerErr) {
			return read.DescribeImageResponse{}, read.ProviderError{
				Message:    providerErr.Message,
				StatusCode: providerErr.StatusCode,
				TraceID:    providerErr.TraceID,
				Provider:   providerErr.Provider,
			}
		}
		return read.DescribeImageResponse{}, err
	}
	return read.DescribeImageResponse{
		Text:     resp.Text,
		Provider: resp.Provider,
		Model:    resp.Model,
	}, nil
}

func (p providerAdapter) Name() string {
	return p.inner.Name()
}

func strPtr(value string) *string {
	return &value
}
