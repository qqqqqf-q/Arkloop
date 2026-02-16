package webfetch

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

const (
	errorArgsInvalid   = "tool.args_invalid"
	errorTimeout       = "tool.timeout"
	errorFetchFailed   = "tool.fetch_failed"
	errorURLDenied     = "tool.url_denied"
	errorNotConfigured = "tool.not_configured"

	defaultTimeout = 15 * time.Second
	maxLengthLimit = 200000
)

var AgentSpec = tools.AgentToolSpec{
	Name:        "web_fetch",
	Version:     "1",
	Description: "抓取网页内容并提取正文",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: false,
}

var LlmSpec = llm.ToolSpec{
	Name:        "web_fetch",
	Description: stringPtr("抓取网页内容，返回 title/content（纯文本）"),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url":        map[string]any{"type": "string", "minLength": 1},
			"max_length": map[string]any{"type": "integer", "minimum": 1, "maximum": maxLengthLimit},
		},
		"required":             []string{"url", "max_length"},
		"additionalProperties": false,
	},
}

type ToolExecutor struct {
	provider Provider
	timeout  time.Duration
}

func NewToolExecutor() *ToolExecutor {
	return &ToolExecutor{
		timeout: defaultTimeout,
	}
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
		built, err := providerFromEnv()
		if err != nil {
			return tools.ExecutionResult{
				Error: &tools.ExecutionError{
					ErrorClass: errorNotConfigured,
					Message:    "web_fetch 配置无效",
					Details:    map[string]any{"reason": err.Error()},
				},
				DurationMs: durationMs(started),
			}
		}
		if built == nil {
			provider = NewBasicProvider()
		} else {
			provider = built
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
				Message:    "web_fetch URL 被安全策略拒绝",
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
		if errors.Is(err, context.DeadlineExceeded) {
			return tools.ExecutionResult{
				Error: &tools.ExecutionError{
					ErrorClass: errorTimeout,
					Message:    "web_fetch 超时",
					Details:    map[string]any{"timeout_seconds": timeout.Seconds()},
				},
				DurationMs: durationMs(started),
			}
		}
		if httpErr, ok := err.(HttpError); ok {
			return tools.ExecutionResult{
				Error: &tools.ExecutionError{
					ErrorClass: errorFetchFailed,
					Message:    "web_fetch 请求失败",
					Details:    map[string]any{"status_code": httpErr.StatusCode},
				},
				DurationMs: durationMs(started),
			}
		}
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorFetchFailed,
				Message:    "web_fetch 执行失败",
				Details:    map[string]any{"reason": err.Error()},
			},
			DurationMs: durationMs(started),
		}
	}

	return tools.ExecutionResult{
		ResultJSON: result.ToJSON(),
		DurationMs: durationMs(started),
	}
}

func providerFromEnv() (Provider, error) {
	cfg, err := ConfigFromEnv(false)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, nil
	}

	switch cfg.ProviderKind {
	case ProviderKindBasic:
		return NewBasicProvider(), nil
	case ProviderKindFirecrawl:
		return NewFirecrawlProvider(cfg.FirecrawlAPIKey, cfg.FirecrawlBaseURL), nil
	case ProviderKindJina:
		return NewJinaProvider(cfg.JinaAPIKey)
	default:
		return nil, fmt.Errorf("web_fetch provider 未实现")
	}
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
			Message:    "工具参数不支持额外字段",
			Details:    map[string]any{"unknown_fields": unknown},
		}
	}

	rawURL, ok := args["url"].(string)
	if !ok || strings.TrimSpace(rawURL) == "" {
		return "", 0, &tools.ExecutionError{
			ErrorClass: errorArgsInvalid,
			Message:    "参数 url 必须为非空字符串",
			Details:    map[string]any{"field": "url"},
		}
	}

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
			Message:    "参数 max_length 必须为整数",
			Details:    map[string]any{"field": "max_length"},
		}
	}
	if maxLength <= 0 || maxLength > maxLengthLimit {
		return "", 0, &tools.ExecutionError{
			ErrorClass: errorArgsInvalid,
			Message:    fmt.Sprintf("参数 max_length 必须在 1..%d 之间", maxLengthLimit),
			Details:    map[string]any{"field": "max_length", "max": maxLengthLimit},
		}
	}
	return strings.TrimSpace(rawURL), maxLength, nil
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
