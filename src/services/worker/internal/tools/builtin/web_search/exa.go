package websearch

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	sharedoutbound "arkloop/services/shared/outboundurl"
	"arkloop/services/worker/internal/tools"
)

const (
	exaDefaultMCPEndpoint = "https://mcp.exa.ai/mcp?tools=web_search_exa"
	exaMaxSearchCount     = 100
)

type ExaProvider struct {
	endpoint    string
	endpointErr error
	client      *http.Client
}

func NewExaProvider() *ExaProvider {
	return newExaProviderWithEndpoint(exaDefaultMCPEndpoint)
}

func newExaProviderWithEndpoint(endpoint string) *ExaProvider {
	resolved, err := resolveExaMCPEndpoint(endpoint)
	return &ExaProvider{
		endpoint:    resolved,
		endpointErr: err,
		client:      sharedoutbound.DefaultPolicy().NewHTTPClient(30 * time.Second),
	}
}

func (p *ExaProvider) ParseSearchRequests(args map[string]any) ([]SearchRequest, *tools.ExecutionError) {
	params, err := parseExaArgs(args)
	if err != nil {
		return nil, err
	}
	return []SearchRequest{{
		Query:      params.Query,
		MaxResults: params.NumResults,
		Args:       args,
	}}, nil
}

func (p *ExaProvider) Search(ctx context.Context, request SearchRequest) ([]Result, error) {
	if p.endpointErr != nil {
		return nil, p.endpointErr
	}
	params, execErr := parseExaArgs(request.Args)
	if execErr != nil {
		return nil, execErr
	}

	sessionID, err := p.initialize(ctx)
	if err != nil {
		return nil, err
	}
	if err := p.notifyInitialized(ctx, sessionID); err != nil {
		return nil, err
	}

	response, err := p.callTool(ctx, sessionID, params)
	if err != nil {
		return nil, err
	}
	results := normalizeExaMCPResults(response, params.NumResults)
	if len(results) == 0 {
		return nil, fmt.Errorf("exa mcp returned no usable results")
	}
	return results, nil
}

type exaParams struct {
	Query      string
	NumResults int
}

type exaMCPRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type exaMCPResponse struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  *exaMCPError    `json:"error,omitempty"`
}

type exaMCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type exaMCPToolResult struct {
	Content []exaMCPContent `json:"content"`
	IsError bool            `json:"isError,omitempty"`
}

type exaMCPContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func parseExaArgs(args map[string]any) (exaParams, *tools.ExecutionError) {
	for key := range args {
		switch key {
		case "query", "numResults":
		default:
			return exaParams{}, &tools.ExecutionError{
				ErrorClass: errorArgsInvalid,
				Message:    "tool arguments do not allow extra fields",
				Details:    map[string]any{"unknown_fields": []string{key}},
			}
		}
	}

	query, ok := args["query"].(string)
	query = strings.TrimSpace(query)
	if !ok || query == "" {
		return exaParams{}, &tools.ExecutionError{
			ErrorClass: errorArgsInvalid,
			Message:    "parameter query must be a non-empty string",
			Details:    map[string]any{"field": "query"},
		}
	}

	numResults, err := parseExaNumResults(args["numResults"])
	if err != nil {
		return exaParams{}, err
	}
	return exaParams{Query: query, NumResults: numResults}, nil
}

func parseExaNumResults(raw any) (int, *tools.ExecutionError) {
	if raw == nil {
		return defaultMaxResults, nil
	}
	var count int
	switch typed := raw.(type) {
	case int:
		count = typed
	case float64:
		count = int(typed)
		if typed != float64(count) {
			return 0, &tools.ExecutionError{ErrorClass: errorArgsInvalid, Message: "parameter numResults must be an integer", Details: map[string]any{"field": "numResults"}}
		}
	default:
		return 0, &tools.ExecutionError{ErrorClass: errorArgsInvalid, Message: "parameter numResults must be an integer", Details: map[string]any{"field": "numResults"}}
	}
	if count <= 0 || count > exaMaxSearchCount {
		return 0, &tools.ExecutionError{ErrorClass: errorArgsInvalid, Message: fmt.Sprintf("parameter numResults must be in range 1..%d", exaMaxSearchCount), Details: map[string]any{"field": "numResults", "max": exaMaxSearchCount}}
	}
	return count, nil
}

func resolveExaMCPEndpoint(endpoint string) (string, error) {
	cleaned := strings.TrimSpace(endpoint)
	if cleaned == "" {
		cleaned = exaDefaultMCPEndpoint
	}
	parsed, err := url.Parse(cleaned)
	if err != nil {
		return "", err
	}
	if !parsed.IsAbs() || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("invalid Exa MCP endpoint")
	}
	return parsed.String(), nil
}

func (p *ExaProvider) initialize(ctx context.Context) (string, error) {
	request := exaMCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "arkloop",
				"version": "1",
			},
		},
	}
	_, sessionID, err := p.doMCP(ctx, request, "")
	if err != nil {
		return "", err
	}
	if sessionID == "" {
		return "", fmt.Errorf("exa mcp did not return session id")
	}
	return sessionID, nil
}

func (p *ExaProvider) notifyInitialized(ctx context.Context, sessionID string) error {
	request := exaMCPRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
		Params:  map[string]any{},
	}
	_, _, err := p.doMCP(ctx, request, sessionID)
	return err
}

func (p *ExaProvider) callTool(ctx context.Context, sessionID string, params exaParams) (exaMCPToolResult, error) {
	request := exaMCPRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/call",
		Params: map[string]any{
			"name": "web_search_exa",
			"arguments": map[string]any{
				"query":      params.Query,
				"numResults": params.NumResults,
			},
		},
	}
	envelope, _, err := p.doMCP(ctx, request, sessionID)
	if err != nil {
		return exaMCPToolResult{}, err
	}
	var result exaMCPToolResult
	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		return exaMCPToolResult{}, fmt.Errorf("decode exa mcp tool result: %w", err)
	}
	if result.IsError {
		return exaMCPToolResult{}, fmt.Errorf("exa mcp tool returned error: %s", exaMCPContentText(result.Content))
	}
	return result, nil
}

func (p *ExaProvider) doMCP(ctx context.Context, payload exaMCPRequest, sessionID string) (exaMCPResponse, string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return exaMCPResponse{}, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(encoded))
	if err != nil {
		return exaMCPResponse{}, "", err
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return exaMCPResponse{}, "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return exaMCPResponse{}, "", HttpError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return exaMCPResponse{}, resp.Header.Get("Mcp-Session-Id"), nil
	}

	envelope, err := parseExaMCPEnvelope(body)
	if err != nil {
		return exaMCPResponse{}, "", err
	}
	if envelope.Error != nil {
		return exaMCPResponse{}, "", fmt.Errorf("exa mcp error %d: %s", envelope.Error.Code, envelope.Error.Message)
	}
	return envelope, resp.Header.Get("Mcp-Session-Id"), nil
}

func parseExaMCPEnvelope(body []byte) (exaMCPResponse, error) {
	trimmed := bytes.TrimSpace(body)
	if bytes.HasPrefix(trimmed, []byte("{")) {
		var envelope exaMCPResponse
		if err := json.Unmarshal(trimmed, &envelope); err != nil {
			return exaMCPResponse{}, err
		}
		return envelope, nil
	}

	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		var envelope exaMCPResponse
		if err := json.Unmarshal([]byte(data), &envelope); err != nil {
			return exaMCPResponse{}, err
		}
		return envelope, nil
	}
	if err := scanner.Err(); err != nil {
		return exaMCPResponse{}, err
	}
	return exaMCPResponse{}, fmt.Errorf("exa mcp response did not contain data")
}

func normalizeExaMCPResults(payload exaMCPToolResult, maxResults int) []Result {
	out := []Result{}
	for _, item := range payload.Content {
		if len(out) >= maxResults {
			break
		}
		if item.Type != "text" {
			continue
		}
		for _, parsed := range parseExaMCPText(item.Text) {
			if len(out) >= maxResults {
				break
			}
			out = append(out, parsed)
		}
	}
	return out
}

func exaMCPContentText(content []exaMCPContent) string {
	parts := make([]string, 0, len(content))
	for _, item := range content {
		if item.Type == "text" && strings.TrimSpace(item.Text) != "" {
			parts = append(parts, strings.TrimSpace(item.Text))
		}
	}
	return strings.Join(parts, "\n")
}

func parseExaMCPText(text string) []Result {
	blocks := splitExaMCPBlocks(text)
	out := make([]Result, 0, len(blocks))
	for _, block := range blocks {
		result := parseExaMCPBlock(block)
		if result.Title == "" || result.URL == "" {
			continue
		}
		out = append(out, result)
	}
	return out
}

func splitExaMCPBlocks(text string) []string {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	parts := strings.Split(normalized, "\n---\n")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if cleaned := strings.TrimSpace(part); cleaned != "" {
			out = append(out, cleaned)
		}
	}
	return out
}

func parseExaMCPBlock(block string) Result {
	var title, resultURL, published string
	lines := strings.Split(block, "\n")
	highlightStart := -1
	for idx, line := range lines {
		cleaned := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(cleaned, "Title:"):
			title = strings.TrimSpace(strings.TrimPrefix(cleaned, "Title:"))
		case strings.HasPrefix(cleaned, "URL:"):
			resultURL = strings.TrimSpace(strings.TrimPrefix(cleaned, "URL:"))
		case strings.HasPrefix(cleaned, "Published:"):
			published = strings.TrimSpace(strings.TrimPrefix(cleaned, "Published:"))
		case strings.HasPrefix(cleaned, "Highlights:"):
			highlightStart = idx + 1
		}
	}

	snippet := ""
	if highlightStart >= 0 && highlightStart < len(lines) {
		snippet = strings.TrimSpace(strings.Join(lines[highlightStart:], "\n"))
	}
	if snippet == "" {
		snippet = strings.TrimSpace(block)
	}
	return Result{
		Title:     title,
		URL:       resultURL,
		Snippet:   snippet,
		Published: published,
		SiteName:  exaMCPSiteName(resultURL),
		Text:      strings.TrimSpace(block),
	}
}

func exaMCPSiteName(urlText string) string {
	if urlText == "" {
		return ""
	}
	return titleFromURL(urlText)
}
