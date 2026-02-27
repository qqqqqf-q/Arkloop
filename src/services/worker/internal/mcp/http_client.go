package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// HTTPClient 实现 Client 接口，支持 streamable_http 和 http_sse 两种传输。
// 两种传输都通过 HTTP POST 发送 JSON-RPC 请求；区别在于响应格式：
//   - streamable_http: 服务端可返回 JSON 或 SSE stream
//   - http_sse: 同上，但 Accept 头明确偏好 SSE
type HTTPClient struct {
	server     ServerConfig
	httpClient *http.Client
	nextID     atomic.Int64
	mu         sync.Mutex
	closed     bool
}

func NewHTTPClient(server ServerConfig) *HTTPClient {
	client := &HTTPClient{
		server: server,
		httpClient: &http.Client{
			Timeout: 0, // 由调用方通过 context deadline 控制
		},
	}
	client.nextID.Store(1)
	return client
}

func (c *HTTPClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *HTTPClient) IsHealthy(_ context.Context) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closed
}

func (c *HTTPClient) ListTools(ctx context.Context, timeoutMs int) ([]Tool, error) {
	if err := c.checkClosed(); err != nil {
		return nil, err
	}

	result, err := c.doRequest(ctx, "tools/list", map[string]any{}, timeoutMs)
	if err != nil {
		return nil, err
	}

	rawTools := result["tools"]
	if rawTools == nil {
		return nil, nil
	}
	list, ok := rawTools.([]any)
	if !ok {
		return nil, ProtocolError{Message: "tools/list returned tools is not an array"}
	}

	out := []Tool{}
	for _, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(asString(obj["name"]))
		if name == "" {
			continue
		}
		title := optionalString(obj["title"])
		description := optionalString(obj["description"])
		schema := map[string]any{}
		if rawSchema, ok := obj["inputSchema"].(map[string]any); ok {
			for key, value := range rawSchema {
				schema[key] = value
			}
		}
		out = append(out, Tool{
			Name:        name,
			Title:       title,
			Description: description,
			InputSchema: schema,
		})
	}
	return out, nil
}

func (c *HTTPClient) CallTool(ctx context.Context, name string, arguments map[string]any, timeoutMs int) (ToolCallResult, error) {
	if err := c.checkClosed(); err != nil {
		return ToolCallResult{}, err
	}

	result, err := c.doRequest(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": arguments,
	}, timeoutMs)
	if err != nil {
		return ToolCallResult{}, err
	}

	rawContent := result["content"]
	contentList, ok := rawContent.([]any)
	if rawContent != nil && !ok {
		return ToolCallResult{}, ProtocolError{Message: "tools/call returned content is not an array"}
	}

	content := []map[string]any{}
	for _, item := range contentList {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		content = append(content, obj)
	}

	isError := false
	if raw, ok := result["isError"].(bool); ok {
		isError = raw
	}

	return ToolCallResult{
		Content: content,
		IsError: isError,
	}, nil
}

// doRequest 发送 JSON-RPC 请求并等待响应。
// 对 streamable_http 和 http_sse 的处理方式：
//   - 如果响应 Content-Type 是 application/json，直接解析 JSON。
//   - 如果响应 Content-Type 是 text/event-stream，从 SSE 流中读取第一条含 result/error 的消息。
func (c *HTTPClient) doRequest(ctx context.Context, method string, params map[string]any, timeoutMs int) (map[string]any, error) {
	reqID := c.nextID.Add(1)

	body := map[string]any{
		"jsonrpc": rpcVersion,
		"id":      reqID,
		"method":  method,
	}
	if params != nil {
		body["params"] = params
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("mcp: marshal request: %w", err)
	}

	if timeoutMs > 0 {
		deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.server.URL, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("mcp: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.server.BearerToken != nil && *c.server.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+*c.server.BearerToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, TimeoutError{Message: "MCP HTTP call timed out"}
		}
		return nil, DisconnectedError{Message: "MCP HTTP request failed: " + err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, RpcError{
			Message: fmt.Sprintf("MCP HTTP server returned %d", resp.StatusCode),
		}
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		return parseSSEResponse(resp.Body, reqID)
	}

	// 直接 JSON 响应
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, ProtocolError{Message: "MCP HTTP response is not valid JSON"}
	}
	return parseResponse(raw)
}

// parseSSEResponse 从 SSE 流中读取第一条含有匹配 id 的 JSON-RPC 响应。
func parseSSEResponse(r io.Reader, reqID int64) (map[string]any, error) {
	scanner := bufio.NewScanner(r)
	var dataLine string

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			dataLine = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			continue
		}
		// 空行是 SSE 事件分隔符
		if line == "" && dataLine != "" {
			var payload map[string]any
			if err := json.Unmarshal([]byte(dataLine), &payload); err != nil {
				dataLine = ""
				continue
			}
			// 检查是否是我们的响应（id 匹配）
			id, ok := parseID(payload["id"])
			if ok && id == reqID {
				return parseResponse(payload)
			}
			dataLine = ""
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, DisconnectedError{Message: "MCP SSE stream read error: " + err.Error()}
	}
	return nil, DisconnectedError{Message: "MCP SSE stream ended without response"}
}

func (c *HTTPClient) checkClosed() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return DisconnectedError{Message: "MCP HTTP client closed"}
	}
	return nil
}
