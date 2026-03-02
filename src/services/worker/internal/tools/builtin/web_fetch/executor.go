package webfetch

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	sharedconfig "arkloop/services/shared/config"
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
	Description: "fetch web page content and extract body text",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: false,
}

var AgentSpecJina = tools.AgentToolSpec{
	Name:        "web_fetch.jina",
	LlmName:     "web_fetch",
	Version:     "1",
	Description: "fetch web page content and extract body text",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: false,
}

var AgentSpecFirecrawl = tools.AgentToolSpec{
	Name:        "web_fetch.firecrawl",
	LlmName:     "web_fetch",
	Version:     "1",
	Description: "fetch web page content and extract body text",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: false,
}

var AgentSpecBasic = tools.AgentToolSpec{
	Name:        "web_fetch.basic",
	LlmName:     "web_fetch",
	Version:     "1",
	Description: "fetch web page content and extract body text",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: false,
}

var LlmSpec = llm.ToolSpec{
	Name:        "web_fetch",
	Description: stringPtr("fetch web page content, return title/content (plain text)"),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url":        map[string]any{"type": "string"},
			"max_length": map[string]any{"type": "integer", "minimum": 1, "maximum": maxLengthLimit},
		},
		"required":             []string{"url", "max_length"},
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
	return &ToolExecutor{
		resolver: resolver,
		timeout:  defaultTimeout,
	}
}

func NewBasicExecutor(resolver sharedconfig.Resolver) *ToolExecutor {
	return &ToolExecutor{
		resolver:   resolver,
		timeout:    defaultTimeout,
		forcedKind: ProviderKindBasic,
	}
}

func NewFirecrawlExecutor(resolver sharedconfig.Resolver) *ToolExecutor {
	return &ToolExecutor{
		resolver:   resolver,
		timeout:    defaultTimeout,
		forcedKind: ProviderKindFirecrawl,
	}
}

func NewJinaExecutor(resolver sharedconfig.Resolver) *ToolExecutor {
	return &ToolExecutor{
		resolver:   resolver,
		timeout:    defaultTimeout,
		forcedKind: ProviderKindJina,
	}
}

func NewToolExecutorWithProvider(provider Provider) *ToolExecutor {
	return &ToolExecutor{
		provider: provider,
		timeout:  defaultTimeout,
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
		built, err := e.loadProvider(ctx, execCtx)
		if err != nil {
			return tools.ExecutionResult{
				Error: &tools.ExecutionError{
					ErrorClass: errorNotConfigured,
					Message:    "web_fetch configuration invalid",
					Details:    map[string]any{"reason": err.Error()},
				},
				DurationMs: durationMs(started),
			}
		}
		if built == nil {
			if e.forcedKind != "" && e.forcedKind != ProviderKindBasic {
				return tools.ExecutionResult{
					Error: &tools.ExecutionError{
						ErrorClass: errorNotConfigured,
						Message:    "web_fetch backend not configured",
					},
					DurationMs: durationMs(started),
				}
			}
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
				Message:    "web_fetch URL denied by security policy",
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
		var denied UrlPolicyDeniedError
		if errors.As(err, &denied) {
			details := map[string]any{"reason": denied.Reason}
			for key, value := range denied.Details {
				details[key] = value
			}
			return tools.ExecutionResult{
				Error: &tools.ExecutionError{
					ErrorClass: errorURLDenied,
					Message:    "web_fetch URL denied by security policy",
					Details:    details,
				},
				DurationMs: durationMs(started),
			}
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return tools.ExecutionResult{
				Error: &tools.ExecutionError{
					ErrorClass: errorTimeout,
					Message:    "web_fetch timed out",
					Details:    map[string]any{"timeout_seconds": timeout.Seconds()},
				},
				DurationMs: durationMs(started),
			}
		}
		if httpErr, ok := err.(HttpError); ok {
			return tools.ExecutionResult{
				Error: &tools.ExecutionError{
					ErrorClass: errorFetchFailed,
					Message:    "web_fetch request failed",
					Details:    map[string]any{"status_code": httpErr.StatusCode},
				},
				DurationMs: durationMs(started),
			}
		}
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorFetchFailed,
				Message:    "web_fetch execution failed",
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

func (e *ToolExecutor) loadProvider(ctx context.Context, execCtx tools.ExecutionContext) (Provider, error) {
	if e.resolver == nil {
		return nil, nil
	}
	scope := sharedconfig.Scope{OrgID: execCtx.OrgID}
	m, err := e.resolver.ResolvePrefix(ctx, "web_fetch.", scope)
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
	case ProviderKindBasic:
		return NewBasicProvider(), nil
	case ProviderKindFirecrawl:
		return NewFirecrawlProvider(cfg.FirecrawlAPIKey, cfg.FirecrawlBaseURL), nil
	case ProviderKindJina:
		return NewJinaProvider(cfg.JinaAPIKey)
	default:
		return nil, fmt.Errorf("web_fetch provider not implemented")
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
		ProviderKind:     kind,
		FirecrawlAPIKey:  strings.TrimSpace(m[settingFirecrawlKey]),
		FirecrawlBaseURL: strings.TrimRight(strings.TrimSpace(m[settingFirecrawlURL]), "/"),
		JinaAPIKey:       strings.TrimSpace(m[settingJinaKey]),
	}
	return cfg, true, nil
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
			Message:    "tool arguments do not allow extra fields",
			Details:    map[string]any{"unknown_fields": unknown},
		}
	}

	rawURL, ok := args["url"].(string)
	if !ok || strings.TrimSpace(rawURL) == "" {
		return "", 0, &tools.ExecutionError{
			ErrorClass: errorArgsInvalid,
			Message:    "parameter url must be a non-empty string",
			Details:    map[string]any{"field": "url"},
		}
	}
	targetURL := normalizeTargetURL(rawURL)

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
			Message:    "parameter max_length must be an integer",
			Details:    map[string]any{"field": "max_length"},
		}
	}
	if maxLength <= 0 || maxLength > maxLengthLimit {
		return "", 0, &tools.ExecutionError{
			ErrorClass: errorArgsInvalid,
			Message:    fmt.Sprintf("parameter max_length must be in range 1..%d", maxLengthLimit),
			Details:    map[string]any{"field": "max_length", "max": maxLengthLimit},
		}
	}
	return targetURL, maxLength, nil
}

func normalizeTargetURL(raw string) string {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return ""
	}

	cleaned = fixDuplicatedScheme(cleaned)
	cleaned = unwrapJinaWrapper(cleaned)
	cleaned = fixDuplicatedScheme(cleaned)
	return strings.TrimSpace(cleaned)
}

func fixDuplicatedScheme(raw string) string {
	if strings.HasPrefix(raw, "httpshttps://") {
		return "https://" + strings.TrimPrefix(raw, "httpshttps://")
	}
	if strings.HasPrefix(raw, "httphttp://") {
		return "http://" + strings.TrimPrefix(raw, "httphttp://")
	}
	return raw
}

func unwrapJinaWrapper(raw string) string {
	trimmed := strings.TrimSpace(raw)
	for {
		stripped := false
		for _, prefix := range []string{"https://r.jina.ai/", "http://r.jina.ai/"} {
			if strings.HasPrefix(trimmed, prefix) {
				trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
				stripped = true
			}
		}
		if !stripped {
			break
		}
	}
	return trimmed
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
