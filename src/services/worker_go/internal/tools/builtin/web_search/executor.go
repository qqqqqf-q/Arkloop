package websearch

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"arkloop/services/worker_go/internal/llm"
	"arkloop/services/worker_go/internal/tools"
)

const (
	errorArgsInvalid   = "tool.args_invalid"
	errorNotConfigured = "tool.not_configured"
	errorTimeout       = "tool.timeout"
	errorSearchFailed  = "tool.search_failed"

	defaultTimeout = 10 * time.Second
	maxResultsLimit = 20
)

var AgentSpec = tools.AgentToolSpec{
	Name:        "web_search",
	Version:     "1",
	Description: "搜索互联网并返回摘要结果",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var LlmSpec = llm.ToolSpec{
	Name:        "web_search",
	Description: stringPtr("搜索互联网并返回标题/链接/摘要"),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query":      map[string]any{"type": "string", "minLength": 1},
			"max_results": map[string]any{"type": "integer", "minimum": 1, "maximum": maxResultsLimit},
		},
		"required":             []string{"query", "max_results"},
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

func (e *ToolExecutor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	_ = toolName
	started := time.Now()

	query, maxResults, argErr := parseArgs(args)
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
					Message:    "web_search 配置无效",
					Details:    map[string]any{"reason": err.Error()},
				},
				DurationMs: durationMs(started),
			}
		}
		if built == nil {
			return tools.ExecutionResult{
				Error: &tools.ExecutionError{
					ErrorClass: errorNotConfigured,
					Message:    "web_search 未配置 backend",
				},
				DurationMs: durationMs(started),
			}
		}
		provider = built
	}

	timeout := e.timeout
	if execCtx.TimeoutMs != nil && *execCtx.TimeoutMs > 0 {
		timeout = time.Duration(*execCtx.TimeoutMs) * time.Millisecond
	}

	searchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	results, err := provider.Search(searchCtx, query, maxResults)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return tools.ExecutionResult{
				Error: &tools.ExecutionError{
					ErrorClass: errorTimeout,
					Message:    "web_search 超时",
					Details:    map[string]any{"timeout_seconds": timeout.Seconds()},
				},
				DurationMs: durationMs(started),
			}
		}
		if httpErr, ok := err.(HttpError); ok {
			return tools.ExecutionResult{
				Error: &tools.ExecutionError{
					ErrorClass: errorSearchFailed,
					Message:    "web_search 请求失败",
					Details:    map[string]any{"status_code": httpErr.StatusCode},
				},
				DurationMs: durationMs(started),
			}
		}
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorSearchFailed,
				Message:    "web_search 执行失败",
				Details:    map[string]any{"reason": err.Error()},
			},
			DurationMs: durationMs(started),
		}
	}

	payload := map[string]any{
		"results": resultsToJSON(results),
	}
	return tools.ExecutionResult{
		ResultJSON: payload,
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
	case ProviderKindSearxng:
		if strings.TrimSpace(cfg.SearxngBaseURL) == "" {
			return nil, fmt.Errorf("SearXNG base_url 未配置")
		}
		return NewSearxngProvider(cfg.SearxngBaseURL), nil
	case ProviderKindTavily:
		if strings.TrimSpace(cfg.TavilyAPIKey) == "" {
			return nil, fmt.Errorf("Tavily api_key 未配置")
		}
		return NewTavilyProvider(cfg.TavilyAPIKey), nil
	case ProviderKindSerper:
		return nil, fmt.Errorf("web_search provider 未实现：serper")
	default:
		return nil, fmt.Errorf("web_search provider 未实现")
	}
}

func resultsToJSON(results []Result) []map[string]any {
	out := make([]map[string]any, 0, len(results))
	for _, item := range results {
		out = append(out, item.ToJSON())
	}
	return out
}

func parseArgs(args map[string]any) (string, int, *tools.ExecutionError) {
	unknown := []string{}
	for key := range args {
		if key != "query" && key != "max_results" {
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

	rawQuery, ok := args["query"].(string)
	if !ok || strings.TrimSpace(rawQuery) == "" {
		return "", 0, &tools.ExecutionError{
			ErrorClass: errorArgsInvalid,
			Message:    "参数 query 必须为非空字符串",
			Details:    map[string]any{"field": "query"},
		}
	}

	rawMax, ok := args["max_results"]
	maxResults, okInt := rawMax.(int)
	if !ok || !okInt {
		if floatVal, ok := rawMax.(float64); ok {
			maxResults = int(floatVal)
			okInt = floatVal == float64(maxResults)
		}
	}
	if !okInt {
		return "", 0, &tools.ExecutionError{
			ErrorClass: errorArgsInvalid,
			Message:    "参数 max_results 必须为整数",
			Details:    map[string]any{"field": "max_results"},
		}
	}
	if maxResults <= 0 || maxResults > maxResultsLimit {
		return "", 0, &tools.ExecutionError{
			ErrorClass: errorArgsInvalid,
			Message:    fmt.Sprintf("参数 max_results 必须在 1..%d 之间", maxResultsLimit),
			Details:    map[string]any{"field": "max_results", "max": maxResultsLimit},
		}
	}

	return strings.TrimSpace(rawQuery), maxResults, nil
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
