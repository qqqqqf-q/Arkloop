package websearch

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

const (
	errorArgsInvalid   = "tool.args_invalid"
	errorNotConfigured = "tool.not_configured"
	errorTimeout       = "tool.timeout"
	errorSearchFailed  = "tool.search_failed"

	defaultTimeout    = 10 * time.Second
	defaultMaxResults = 5
	maxResultsLimit   = 20
	maxQueriesLimit   = 5
)

var AgentSpec = tools.AgentToolSpec{
	Name:        "web_search",
	Version:     "1",
	Description: "search the internet and return summary results",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var AgentSpecSearxng = tools.AgentToolSpec{
	Name:        "web_search.searxng",
	LlmName:     "web_search",
	Version:     "1",
	Description: "search the internet and return summary results",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var AgentSpecTavily = tools.AgentToolSpec{
	Name:        "web_search.tavily",
	LlmName:     "web_search",
	Version:     "1",
	Description: "search the internet and return summary results",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var LlmSpec = llm.ToolSpec{
	Name:        "web_search",
	Description: stringPtr(fmt.Sprintf("search the internet and return title/link/summary. Always set max_results (default %d). Use queries for multi-search in one call.", defaultMaxResults)),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "single query; for multi-search prefer queries",
			},
			"queries": map[string]any{
				"type":        "array",
				"description": "multiple queries executed in parallel",
				"minItems":    1,
				"maxItems":    maxQueriesLimit,
				"items":       map[string]any{"type": "string"},
			},
			"max_results": map[string]any{
				"type":        "integer",
				"description": fmt.Sprintf("maximum results per query (default %d)", defaultMaxResults),
				"default":     defaultMaxResults,
				"minimum":     1,
				"maximum":     maxResultsLimit,
			},
		},
		"anyOf": []any{
			map[string]any{"required": []string{"query"}},
			map[string]any{"required": []string{"queries"}},
		},
		"additionalProperties": false,
	},
}

type ToolExecutor struct {
	provider   Provider
	resolver   sharedconfig.Resolver
	timeout    time.Duration
	forcedKind ProviderKind
}

func NewToolExecutor(resolver sharedconfig.Resolver) *ToolExecutor {
	return &ToolExecutor{resolver: resolver, timeout: defaultTimeout}
}

func NewSearxngExecutor(resolver sharedconfig.Resolver) *ToolExecutor {
	return &ToolExecutor{resolver: resolver, timeout: defaultTimeout, forcedKind: ProviderKindSearxng}
}

func NewTavilyExecutor(resolver sharedconfig.Resolver) *ToolExecutor {
	return &ToolExecutor{resolver: resolver, timeout: defaultTimeout, forcedKind: ProviderKindTavily}
}

func NewToolExecutorWithProvider(provider Provider) *ToolExecutor {
	return &ToolExecutor{provider: provider, timeout: defaultTimeout}
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

	queries, maxResults, argErr := parseArgs(args)
	if argErr != nil {
		return tools.ExecutionResult{
			Error:      argErr,
			DurationMs: durationMs(started),
		}
	}

	provider := e.provider
	if provider == nil {
		built, err := e.loadProvider(ctx, execCtx)
		if err != nil {
			return tools.ExecutionResult{
				Error: &tools.ExecutionError{
					ErrorClass: errorNotConfigured,
					Message:    "web_search configuration invalid",
					Details:    map[string]any{"reason": err.Error()},
				},
				DurationMs: durationMs(started),
			}
		}
		if built == nil {
			return tools.ExecutionResult{
				Error: &tools.ExecutionError{
					ErrorClass: errorNotConfigured,
					Message:    "web_search backend not configured",
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

	items := e.searchMany(searchCtx, provider, queries, maxResults)
	payload, execErr := buildSearchPayload(items, timeout)
	if execErr != nil {
		return tools.ExecutionResult{
			Error:      execErr,
			DurationMs: durationMs(started),
		}
	}

	return tools.ExecutionResult{ResultJSON: payload, DurationMs: durationMs(started)}
}

func (e *ToolExecutor) loadProvider(ctx context.Context, execCtx tools.ExecutionContext) (Provider, error) {
	if e.resolver == nil {
		return nil, nil
	}
	scope := sharedconfig.Scope{OrgID: execCtx.OrgID}
	m, err := e.resolver.ResolvePrefix(ctx, "web_search.", scope)
	if err != nil {
		return nil, err
	}

	cfg, ok, err := configFromSettings(m, e.forcedKind)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return buildProvider(cfg)
}

func buildProvider(cfg *Config) (Provider, error) {
	switch cfg.ProviderKind {
	case ProviderKindSearxng:
		if strings.TrimSpace(cfg.SearxngBaseURL) == "" {
			return nil, fmt.Errorf("searxng base_url not configured")
		}
		return NewSearxngProvider(cfg.SearxngBaseURL), nil
	case ProviderKindTavily:
		if strings.TrimSpace(cfg.TavilyAPIKey) == "" {
			return nil, fmt.Errorf("tavily api_key not configured")
		}
		return NewTavilyProvider(cfg.TavilyAPIKey), nil
	case ProviderKindSerper:
		return nil, fmt.Errorf("web_search provider not implemented: serper")
	default:
		return nil, fmt.Errorf("web_search provider not implemented")
	}
}

func configFromSettings(m map[string]string, forcedKind ProviderKind) (*Config, bool, error) {
	kind := forcedKind
	if kind == "" {
		raw := strings.TrimSpace(m[settingProvider])
		if raw == "" {
			return nil, false, nil
		}
		parsed, err := parseProviderKind(raw)
		if err != nil {
			return nil, false, err
		}
		kind = parsed
	}

	cfg := &Config{
		ProviderKind:   kind,
		SearxngBaseURL: strings.TrimRight(strings.TrimSpace(m[settingSearxngURL]), "/"),
		TavilyAPIKey:   strings.TrimSpace(m[settingTavilyKey]),
	}
	return cfg, true, nil
}

func resultsToJSON(results []Result) []map[string]any {
	out := make([]map[string]any, 0, len(results))
	for _, item := range results {
		out = append(out, item.ToJSON())
	}
	return out
}

func parseArgs(args map[string]any) ([]string, int, *tools.ExecutionError) {
	unknown := []string{}
	for key := range args {
		if key != "query" && key != "queries" && key != "max_results" {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, 0, &tools.ExecutionError{
			ErrorClass: errorArgsInvalid,
			Message:    "tool arguments do not allow extra fields",
			Details:    map[string]any{"unknown_fields": unknown},
		}
	}

	maxResults := defaultMaxResults
	if rawMax, has := args["max_results"]; has && rawMax != nil {
		switch typed := rawMax.(type) {
		case int:
			maxResults = typed
		case float64:
			maxResults = int(typed)
			if typed != float64(maxResults) {
				return nil, 0, &tools.ExecutionError{
					ErrorClass: errorArgsInvalid,
					Message:    "parameter max_results must be an integer",
					Details:    map[string]any{"field": "max_results"},
				}
			}
		default:
			return nil, 0, &tools.ExecutionError{
				ErrorClass: errorArgsInvalid,
				Message:    "parameter max_results must be an integer",
				Details:    map[string]any{"field": "max_results"},
			}
		}
	}
	if maxResults <= 0 || maxResults > maxResultsLimit {
		return nil, 0, &tools.ExecutionError{
			ErrorClass: errorArgsInvalid,
			Message:    fmt.Sprintf("parameter max_results must be in range 1..%d", maxResultsLimit),
			Details:    map[string]any{"field": "max_results", "max": maxResultsLimit},
		}
	}

	queries, err := parseQueries(args)
	if err != nil {
		return nil, 0, err
	}
	return queries, maxResults, nil
}

func parseQueries(args map[string]any) ([]string, *tools.ExecutionError) {
	queries := []string{}

	if rawQueries, has := args["queries"]; has && rawQueries != nil {
		list, err := asStringList(rawQueries)
		if err != nil {
			return nil, &tools.ExecutionError{
				ErrorClass: errorArgsInvalid,
				Message:    "parameter queries must be an array of non-empty strings",
				Details:    map[string]any{"field": "queries"},
			}
		}
		queries = append(queries, list...)
	}

	if rawQuery, has := args["query"]; has && rawQuery != nil {
		query, ok := rawQuery.(string)
		if !ok || strings.TrimSpace(query) == "" {
			return nil, &tools.ExecutionError{
				ErrorClass: errorArgsInvalid,
				Message:    "parameter query must be a non-empty string",
				Details:    map[string]any{"field": "query"},
			}
		}
		queries = append(queries, query)
	}

	queries = normalizeQueries(queries)
	if len(queries) == 0 {
		return nil, &tools.ExecutionError{
			ErrorClass: errorArgsInvalid,
			Message:    "parameter query or queries is required",
			Details:    map[string]any{"fields": []string{"query", "queries"}},
		}
	}
	if len(queries) > maxQueriesLimit {
		return nil, &tools.ExecutionError{
			ErrorClass: errorArgsInvalid,
			Message:    fmt.Sprintf("queries count must be in range 1..%d", maxQueriesLimit),
			Details:    map[string]any{"field": "queries", "max": maxQueriesLimit},
		}
	}
	return queries, nil
}

func asStringList(value any) ([]string, error) {
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			cleaned := strings.TrimSpace(item)
			if cleaned == "" {
				return nil, fmt.Errorf("empty item")
			}
			out = append(out, cleaned)
		}
		return out, nil
	case []any:
		out := make([]string, 0, len(typed))
		for _, raw := range typed {
			item, ok := raw.(string)
			if !ok {
				return nil, fmt.Errorf("item must be string")
			}
			cleaned := strings.TrimSpace(item)
			if cleaned == "" {
				return nil, fmt.Errorf("empty item")
			}
			out = append(out, cleaned)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported type")
	}
}

func normalizeQueries(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, raw := range items {
		cleaned := strings.TrimSpace(raw)
		if cleaned == "" {
			continue
		}
		key := strings.ToLower(cleaned)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, cleaned)
	}
	return out
}

type searchJobResult struct {
	Query   string
	Results []Result
	Err     error
}

func (e *ToolExecutor) searchMany(
	ctx context.Context,
	provider Provider,
	queries []string,
	maxResults int,
) []searchJobResult {
	results := make([]searchJobResult, len(queries))
	var wg sync.WaitGroup
	wg.Add(len(queries))
	for idx := range queries {
		idx := idx
		query := queries[idx]
		go func() {
			defer wg.Done()
			hits, err := provider.Search(ctx, query, maxResults)
			results[idx] = searchJobResult{
				Query:   query,
				Results: hits,
				Err:     err,
			}
		}()
	}
	wg.Wait()
	return results
}

func buildSearchPayload(items []searchJobResult, timeout time.Duration) (map[string]any, *tools.ExecutionError) {
	flatResults := []Result{}
	byQuery := make([]map[string]any, 0, len(items))
	errorsOut := []map[string]any{}
	seenURL := map[string]struct{}{}
	successCount := 0

	for idx, item := range items {
		if item.Err != nil {
			errPayload := searchErrorPayload(item.Query, item.Err, timeout)
			byQuery = append(byQuery, map[string]any{
				"query_index":  idx,
				"query":        item.Query,
				"result_count": 0,
				"error":        errPayload,
			})
			errorsOut = append(errorsOut, errPayload)
			continue
		}

		successCount++
		byQuery = append(byQuery, map[string]any{
			"query_index":  idx,
			"query":        item.Query,
			"result_count": len(item.Results),
		})
		for _, hit := range item.Results {
			key := normalizeURL(hit.URL)
			if key != "" {
				if _, exists := seenURL[key]; exists {
					continue
				}
				seenURL[key] = struct{}{}
			}
			hit.QueryIndex = idx
			flatResults = append(flatResults, hit)
		}
	}

	if successCount == 0 {
		errClass := errorSearchFailed
		for _, item := range items {
			if item.Err != nil && errors.Is(item.Err, context.DeadlineExceeded) {
				errClass = errorTimeout
				break
			}
		}
		message := "web_search execution failed"
		if errClass == errorTimeout {
			message = "web_search timed out"
		}
		return nil, &tools.ExecutionError{
			ErrorClass: errClass,
			Message:    message,
			Details: map[string]any{
				"query_count": len(items),
				"errors":      errorsOut,
			},
		}
	}

	payload := map[string]any{
		"results":  resultsToJSON(flatResults),
		"by_query": byQuery,
		"meta": map[string]any{
			"query_count":       len(items),
			"succeeded_queries": successCount,
			"failed_queries":    len(items) - successCount,
		},
	}
	if len(errorsOut) > 0 {
		payload["errors"] = errorsOut
	}
	return payload, nil
}

func searchErrorPayload(query string, err error, timeout time.Duration) map[string]any {
	payload := map[string]any{
		"query": query,
	}
	if errors.Is(err, context.DeadlineExceeded) {
		payload["error_class"] = errorTimeout
		payload["message"] = "web_search timed out"
		payload["timeout_seconds"] = timeout.Seconds()
		return payload
	}
	if httpErr, ok := err.(HttpError); ok {
		payload["error_class"] = errorSearchFailed
		payload["message"] = "web_search request failed"
		payload["status_code"] = httpErr.StatusCode
		return payload
	}
	payload["error_class"] = errorSearchFailed
	payload["message"] = "web_search execution failed"
	payload["reason"] = err.Error()
	return payload
}

func normalizeURL(raw string) string {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return ""
	}

	parsed, err := url.Parse(cleaned)
	if err == nil && parsed != nil {
		parsed.Fragment = ""
		parsed.RawFragment = ""
		cleaned = parsed.String()
	} else {
		if idx := strings.Index(cleaned, "#"); idx >= 0 {
			cleaned = cleaned[:idx]
		}
	}

	return strings.ToLower(strings.TrimSpace(cleaned))
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
