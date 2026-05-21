package xsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	sharedoutbound "arkloop/services/shared/outboundurl"
	sharedtoolmeta "arkloop/services/shared/toolmeta"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

const (
	GroupName       = "x_search"
	ProviderNameXAI = "x_search.xai"
	DefaultModel    = "grok-4.20-reasoning"
	defaultBaseURL  = "https://api.x.ai/v1"
	defaultTimeout  = 180 * time.Second
	defaultRetries  = 2

	errorArgsInvalid   = "tool.args_invalid"
	errorNotConfigured = "config.missing"
	errorTimeout       = "tool.timeout"
	errorSearchFailed  = "tool.search_failed"
)

var AgentSpec = tools.AgentToolSpec{
	Name:            GroupName,
	Version:         "1",
	Description:     "search X posts and return a synthesized answer with citations",
	RiskLevel:       tools.RiskLevelLow,
	SideEffects:     false,
	HardTimeoutMode: tools.HardTimeoutModeIgnored,
}

var AgentSpecXAI = tools.AgentToolSpec{
	Name:            ProviderNameXAI,
	LlmName:         GroupName,
	Version:         "1",
	Description:     "search X posts through xAI and return a synthesized answer with citations",
	RiskLevel:       tools.RiskLevelLow,
	SideEffects:     false,
	HardTimeoutMode: tools.HardTimeoutModeIgnored,
}

var LlmSpec = llm.ToolSpec{
	Name:        GroupName,
	Description: stringPtr(sharedtoolmeta.Must(GroupName).LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "question or search query for X posts",
			},
			"allowed_x_handles": map[string]any{
				"type":        "array",
				"description": "optional X handles to include, without @",
				"maxItems":    10,
				"items":       map[string]any{"type": "string"},
			},
			"excluded_x_handles": map[string]any{
				"type":        "array",
				"description": "optional X handles to exclude, without @",
				"maxItems":    10,
				"items":       map[string]any{"type": "string"},
			},
			"from_date": map[string]any{
				"type":        "string",
				"description": "optional lower date bound in YYYY-MM-DD format",
			},
			"to_date": map[string]any{
				"type":        "string",
				"description": "optional upper date bound in YYYY-MM-DD format",
			},
			"enable_image_understanding": map[string]any{
				"type":        "boolean",
				"description": "allow image understanding in matched X posts",
				"default":     false,
			},
			"enable_video_understanding": map[string]any{
				"type":        "boolean",
				"description": "allow video understanding in matched X posts",
				"default":     false,
			},
		},
		"required":             []any{"query"},
		"additionalProperties": false,
	},
}

type ToolExecutor struct {
	provider Provider
	timeout  time.Duration
}

func NewToolExecutor(_ any) *ToolExecutor {
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

	req, argErr := parseArgs(args)
	if argErr != nil {
		return tools.ExecutionResult{Error: argErr, DurationMs: durationMs(started)}
	}
	if e.provider == nil {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: errorNotConfigured, Message: "x_search backend not configured"},
			DurationMs: durationMs(started),
		}
	}

	timeout := e.timeout
	if execCtx.TimeoutMs != nil && *execCtx.TimeoutMs > 0 {
		timeout = time.Duration(*execCtx.TimeoutMs) * time.Millisecond
	}
	if timeout < defaultTimeout {
		timeout = defaultTimeout
	}
	searchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := e.provider.Search(searchCtx, req)
	if err != nil {
		return tools.ExecutionResult{Error: providerError(err), DurationMs: durationMs(started)}
	}
	return tools.ExecutionResult{ResultJSON: result.ToJSON(), DurationMs: durationMs(started)}
}

type Provider interface {
	Search(ctx context.Context, request Request) (Result, error)
}

type Request struct {
	Query                    string
	AllowedXHandles          []string
	ExcludedXHandles         []string
	FromDate                 string
	ToDate                   string
	EnableImageUnderstanding bool
	EnableVideoUnderstanding bool
}

type Result struct {
	Query            string
	Answer           string
	Citations        []string
	InlineCitations  []any
	Provider         string
	CredentialSource string
	Model            string
	RawOutput        any
}

func (r Result) ToJSON() map[string]any {
	payload := map[string]any{
		"query":             r.Query,
		"answer":            strings.TrimSpace(r.Answer),
		"provider":          strings.TrimSpace(r.Provider),
		"credential_source": strings.TrimSpace(r.CredentialSource),
		"model":             strings.TrimSpace(r.Model),
	}
	if len(r.Citations) > 0 {
		payload["citations"] = append([]string{}, r.Citations...)
	}
	if len(r.InlineCitations) > 0 {
		payload["inline_citations"] = append([]any{}, r.InlineCitations...)
	}
	if r.RawOutput != nil && payload["answer"] == "" {
		payload["raw_output"] = r.RawOutput
	}
	return payload
}

type XAIProviderConfig struct {
	APIKey     string
	OAuthValue string
	BaseURL    string
	Model      string
	AuthMode   string
}

type XAIProvider struct {
	token            string
	credentialSource string
	baseURL          string
	model            string
	client           *http.Client
}

func NewXAIProvider(cfg XAIProviderConfig) (*XAIProvider, error) {
	token, source := resolveCredential(cfg.APIKey, cfg.OAuthValue, cfg.AuthMode)
	if token == "" {
		return nil, fmt.Errorf("missing xAI credentials")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = DefaultModel
	}
	return &XAIProvider{
		token:            token,
		credentialSource: source,
		baseURL:          baseURL,
		model:            model,
		client:           sharedoutbound.DefaultPolicy().NewHTTPClient(defaultTimeout),
	}, nil
}

func (p *XAIProvider) Search(ctx context.Context, request Request) (Result, error) {
	if strings.TrimSpace(request.Query) == "" {
		return Result{}, fmt.Errorf("query must not be empty")
	}
	payload := map[string]any{
		"model": p.model,
		"input": []map[string]any{{
			"role":    "user",
			"content": request.Query,
		}},
		"tools": []map[string]any{{
			"type": "x_search",
		}},
		"store": false,
	}
	tool := payload["tools"].([]map[string]any)[0]
	if len(request.AllowedXHandles) > 0 {
		tool["allowed_x_handles"] = request.AllowedXHandles
	}
	if len(request.ExcludedXHandles) > 0 {
		tool["excluded_x_handles"] = request.ExcludedXHandles
	}
	if request.FromDate != "" {
		tool["from_date"] = request.FromDate
	}
	if request.ToDate != "" {
		tool["to_date"] = request.ToDate
	}
	if request.EnableImageUnderstanding {
		tool["enable_image_understanding"] = true
	}
	if request.EnableVideoUnderstanding {
		tool["enable_video_understanding"] = true
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/responses", bytes.NewReader(encoded))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.doWithRetries(req, encoded)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, HttpError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Result{}, err
	}
	answer, inlineCitations := extractAnswer(parsed)
	return Result{
		Query:            request.Query,
		Answer:           answer,
		Citations:        extractCitations(parsed),
		InlineCitations:  inlineCitations,
		Provider:         "xai",
		CredentialSource: p.credentialSource,
		Model:            p.model,
		RawOutput:        parsed["output"],
	}, nil
}

func (p *XAIProvider) doWithRetries(req *http.Request, body []byte) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= defaultRetries; attempt++ {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		if attempt > 0 {
			if err := sleepWithContext(req.Context(), time.Duration(min(5000, 1500*attempt))*time.Millisecond); err != nil {
				return nil, err
			}
		}
		nextReq := req.Clone(req.Context())
		nextReq.Body = io.NopCloser(bytes.NewReader(body))
		resp, err := p.client.Do(nextReq)
		if err != nil {
			lastErr = err
			if !isRetryableRequestError(req.Context(), err) || attempt >= defaultRetries {
				return nil, err
			}
			continue
		}
		if resp.StatusCode >= 500 && attempt < defaultRetries {
			lastErr = HttpError{StatusCode: resp.StatusCode}
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
			_ = resp.Body.Close()
			continue
		}
		return resp, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("x_search request did not return a response")
}

func sleepWithContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isRetryableRequestError(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	return true
}

type HttpError struct {
	StatusCode int
	Body       string
}

func (e HttpError) Error() string {
	return "http error"
}

func resolveCredential(apiKey string, oauthValue string, authMode string) (string, string) {
	oauthToken := oauthAccessToken(oauthValue)
	switch strings.TrimSpace(authMode) {
	case "api_key":
		if key := strings.TrimSpace(apiKey); key != "" {
			return key, "api_key"
		}
		return "", ""
	case "oauth":
		if oauthToken != "" {
			return oauthToken, "oauth"
		}
		return "", ""
	}
	if oauthToken != "" {
		return oauthToken, "oauth"
	}
	if key := strings.TrimSpace(apiKey); key != "" {
		return key, "api_key"
	}
	return "", ""
}

func oauthAccessToken(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return raw
	}
	if token, ok := obj["access_token"].(string); ok {
		return strings.TrimSpace(token)
	}
	return ""
}

func parseArgs(args map[string]any) (Request, *tools.ExecutionError) {
	for key := range args {
		switch key {
		case "query", "allowed_x_handles", "excluded_x_handles", "from_date", "to_date", "enable_image_understanding", "enable_video_understanding":
		default:
			return Request{}, &tools.ExecutionError{
				ErrorClass: errorArgsInvalid,
				Message:    "tool arguments do not allow extra fields",
				Details:    map[string]any{"unknown_fields": []string{key}},
			}
		}
	}
	query, ok := args["query"].(string)
	query = strings.TrimSpace(query)
	if !ok || query == "" {
		return Request{}, &tools.ExecutionError{
			ErrorClass: errorArgsInvalid,
			Message:    "parameter query must be a non-empty string",
			Details:    map[string]any{"field": "query"},
		}
	}
	req := Request{Query: query}
	var err *tools.ExecutionError
	req.AllowedXHandles, err = parseHandleList(args["allowed_x_handles"], "allowed_x_handles")
	if err != nil {
		return Request{}, err
	}
	req.ExcludedXHandles, err = parseHandleList(args["excluded_x_handles"], "excluded_x_handles")
	if err != nil {
		return Request{}, err
	}
	req.FromDate, err = parseOptionalString(args["from_date"], "from_date")
	if err != nil {
		return Request{}, err
	}
	req.ToDate, err = parseOptionalString(args["to_date"], "to_date")
	if err != nil {
		return Request{}, err
	}
	req.EnableImageUnderstanding, err = parseOptionalBool(args["enable_image_understanding"], "enable_image_understanding")
	if err != nil {
		return Request{}, err
	}
	req.EnableVideoUnderstanding, err = parseOptionalBool(args["enable_video_understanding"], "enable_video_understanding")
	if err != nil {
		return Request{}, err
	}
	return req, nil
}

func parseHandleList(raw any, field string) ([]string, *tools.ExecutionError) {
	if raw == nil {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, &tools.ExecutionError{ErrorClass: errorArgsInvalid, Message: "parameter " + field + " must be an array", Details: map[string]any{"field": field}}
	}
	out := make([]string, 0, len(list))
	seen := map[string]struct{}{}
	for _, item := range list {
		text, ok := item.(string)
		text = strings.TrimPrefix(strings.TrimSpace(text), "@")
		if !ok || text == "" {
			return nil, &tools.ExecutionError{ErrorClass: errorArgsInvalid, Message: "parameter " + field + " must contain non-empty strings", Details: map[string]any{"field": field}}
		}
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		out = append(out, text)
	}
	if len(out) > 10 {
		return nil, &tools.ExecutionError{ErrorClass: errorArgsInvalid, Message: "parameter " + field + " allows at most 10 handles", Details: map[string]any{"field": field}}
	}
	sort.Strings(out)
	return out, nil
}

func parseOptionalString(raw any, field string) (string, *tools.ExecutionError) {
	if raw == nil {
		return "", nil
	}
	text, ok := raw.(string)
	if !ok {
		return "", &tools.ExecutionError{ErrorClass: errorArgsInvalid, Message: "parameter " + field + " must be a string", Details: map[string]any{"field": field}}
	}
	return strings.TrimSpace(text), nil
}

func parseOptionalBool(raw any, field string) (bool, *tools.ExecutionError) {
	if raw == nil {
		return false, nil
	}
	value, ok := raw.(bool)
	if !ok {
		return false, &tools.ExecutionError{ErrorClass: errorArgsInvalid, Message: "parameter " + field + " must be a boolean", Details: map[string]any{"field": field}}
	}
	return value, nil
}

func extractAnswer(root map[string]any) (string, []any) {
	if text, ok := root["output_text"].(string); ok && strings.TrimSpace(text) != "" {
		return strings.TrimSpace(text), nil
	}
	output, _ := root["output"].([]any)
	parts := make([]string, 0)
	citations := make([]any, 0)
	for _, item := range output {
		obj, _ := item.(map[string]any)
		content, _ := obj["content"].([]any)
		for _, rawContent := range content {
			contentObj, _ := rawContent.(map[string]any)
			if text, ok := contentObj["text"].(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, strings.TrimSpace(text))
			}
			if rawCitations, ok := contentObj["citations"].([]any); ok {
				citations = append(citations, rawCitations...)
			}
		}
	}
	return strings.Join(parts, "\n\n"), citations
}

func extractCitations(root map[string]any) []string {
	seen := map[string]struct{}{}
	out := []string{}
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			for key, item := range typed {
				if strings.EqualFold(key, "url") {
					if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
						if _, exists := seen[text]; !exists {
							seen[text] = struct{}{}
							out = append(out, text)
						}
					}
				}
				walk(item)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(root)
	return out
}

func providerError(err error) *tools.ExecutionError {
	if errors.Is(err, context.DeadlineExceeded) {
		return &tools.ExecutionError{ErrorClass: errorTimeout, Message: "x_search timed out"}
	}
	var httpErr HttpError
	if errors.As(err, &httpErr) {
		return &tools.ExecutionError{
			ErrorClass: errorSearchFailed,
			Message:    "xAI x_search request failed",
			Details:    map[string]any{"status_code": httpErr.StatusCode},
		}
	}
	return &tools.ExecutionError{ErrorClass: errorSearchFailed, Message: err.Error()}
}

func durationMs(start time.Time) int {
	return int(time.Since(start).Milliseconds())
}

func stringPtr(value string) *string {
	return &value
}
