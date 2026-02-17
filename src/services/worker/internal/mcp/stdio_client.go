package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	rpcVersion             = "2.0"
	defaultProtocolVersion = "2025-06-18"
)

type Tool struct {
	Name        string
	Title       *string
	Description *string
	InputSchema map[string]any
}

type ToolCallResult struct {
	Content []map[string]any
	IsError bool
}

type TimeoutError struct {
	Message string
}

func (e TimeoutError) Error() string {
	return e.Message
}

type DisconnectedError struct {
	Message string
}

func (e DisconnectedError) Error() string {
	return e.Message
}

type RpcError struct {
	Code    *int
	Message string
	Data    any
}

func (e RpcError) Error() string {
	return e.Message
}

type ProtocolError struct {
	Message string
}

func (e ProtocolError) Error() string {
	return e.Message
}

type StdioClient struct {
	server ServerConfig

	mu          sync.Mutex
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdout      io.ReadCloser
	closed      bool
	nextID      int64
	pending     map[int64]chan map[string]any
	initialized bool

	writeMu sync.Mutex
}

func NewStdioClient(server ServerConfig) *StdioClient {
	return &StdioClient{
		server:  server,
		nextID:  1,
		pending: map[int64]chan map[string]any{},
	}
}

func (c *StdioClient) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	cmd := c.cmd
	stdin := c.stdin
	pending := c.pending
	c.pending = map[int64]chan map[string]any{}
	c.mu.Unlock()

	for _, ch := range pending {
		close(ch)
	}

	if stdin != nil {
		_ = stdin.Close()
	}

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
	return nil
}

func (c *StdioClient) ensureStarted(ctx context.Context) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return DisconnectedError{Message: "MCP client 已关闭"}
	}
	if c.cmd != nil {
		c.mu.Unlock()
		return nil
	}
	server := c.server
	c.mu.Unlock()

	cmd := exec.CommandContext(ctx, server.Command, server.Args...)
	if server.Cwd != nil {
		cmd.Dir = *server.Cwd
	}
	cmd.Env = buildServerEnv(server)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return err
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return err
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = cmd.Process.Kill()
		return DisconnectedError{Message: "MCP client 已关闭"}
	}
	c.cmd = cmd
	c.stdin = stdin
	c.stdout = stdout
	c.mu.Unlock()

	go c.readLoop(stdout)
	return nil
}

func buildServerEnv(server ServerConfig) []string {
	base := []string{}
	if server.InheritParentEnv {
		base = append(base, os.Environ()...)
	}
	for key, value := range server.Env {
		base = append(base, fmt.Sprintf("%s=%s", key, value))
	}
	return base
}

func (c *StdioClient) readLoop(stdout io.Reader) {
	reader := bufio.NewReader(stdout)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if !errors.Is(err, io.EOF) {
				c.failPending(DisconnectedError{Message: "MCP stdout 读取失败"})
			} else {
				c.failPending(DisconnectedError{Message: "MCP stdout 已关闭"})
			}
			return
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		var payload any
		if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
			continue
		}
		obj, ok := payload.(map[string]any)
		if !ok {
			continue
		}

		id, ok := parseID(obj["id"])
		if !ok {
			continue
		}

		c.mu.Lock()
		ch := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if ch == nil {
			continue
		}
		ch <- obj
		close(ch)
	}
}

func (c *StdioClient) failPending(err error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	pending := c.pending
	c.pending = map[int64]chan map[string]any{}
	c.mu.Unlock()

	for _, ch := range pending {
		close(ch)
	}
	_ = err
}

func parseID(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		if typed <= 0 {
			return 0, false
		}
		return int64(typed), true
	case int:
		if typed <= 0 {
			return 0, false
		}
		return int64(typed), true
	case int64:
		if typed <= 0 {
			return 0, false
		}
		return typed, true
	default:
		return 0, false
	}
}

func (c *StdioClient) Initialize(ctx context.Context, timeoutMs int) error {
	c.mu.Lock()
	if c.initialized {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	_, err := c.request(ctx, "initialize", map[string]any{
		"protocolVersion": defaultProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "arkloop", "version": "0"},
	}, timeoutMs)
	if err != nil {
		return err
	}
	if err := c.notify(ctx, "notifications/initialized", nil); err != nil {
		return err
	}

	c.mu.Lock()
	c.initialized = true
	c.mu.Unlock()
	return nil
}

func (c *StdioClient) ListTools(ctx context.Context, timeoutMs int) ([]Tool, error) {
	if err := c.Initialize(ctx, timeoutMs); err != nil {
		return nil, err
	}
	result, err := c.request(ctx, "tools/list", map[string]any{}, timeoutMs)
	if err != nil {
		return nil, err
	}

	rawTools, ok := result["tools"]
	if rawTools == nil {
		return nil, nil
	}
	list, ok := rawTools.([]any)
	if !ok {
		return nil, ProtocolError{Message: "tools/list 返回 tools 不是数组"}
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

func (c *StdioClient) CallTool(ctx context.Context, name string, arguments map[string]any, timeoutMs int) (ToolCallResult, error) {
	if err := c.Initialize(ctx, timeoutMs); err != nil {
		return ToolCallResult{}, err
	}
	result, err := c.request(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": arguments,
	}, timeoutMs)
	if err != nil {
		return ToolCallResult{}, err
	}

	rawContent := result["content"]
	contentList, ok := rawContent.([]any)
	if rawContent != nil && !ok {
		return ToolCallResult{}, ProtocolError{Message: "tools/call 返回 content 不是数组"}
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

func (c *StdioClient) request(ctx context.Context, method string, params map[string]any, timeoutMs int) (map[string]any, error) {
	if err := c.ensureStarted(ctx); err != nil {
		return nil, err
	}

	id := c.reserveID()
	ch := make(chan map[string]any, 1)

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, DisconnectedError{Message: "MCP client 已关闭"}
	}
	c.pending[id] = ch
	stdin := c.stdin
	c.mu.Unlock()

	payload := map[string]any{
		"jsonrpc": rpcVersion,
		"id":      id,
		"method":  method,
	}
	if params != nil {
		payload["params"] = params
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	c.writeMu.Lock()
	_, writeErr := stdin.Write(append(encoded, '\n'))
	c.writeMu.Unlock()
	if writeErr != nil {
		return nil, DisconnectedError{Message: "MCP stdin 写入失败"}
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	select {
	case <-ctx.Done():
		return nil, TimeoutError{Message: "MCP 调用已取消"}
	case <-time.After(timeout):
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, TimeoutError{Message: "MCP 调用超时"}
	case resp, ok := <-ch:
		if !ok {
			return nil, DisconnectedError{Message: "MCP client 已断开"}
		}
		return parseResponse(resp)
	}
}

func parseResponse(obj map[string]any) (map[string]any, error) {
	if rawErr, ok := obj["error"].(map[string]any); ok {
		message := strings.TrimSpace(asString(rawErr["message"]))
		if message == "" {
			message = "MCP RPC error"
		}
		var code *int
		if rawCode, ok := rawErr["code"].(float64); ok {
			value := int(rawCode)
			code = &value
		}
		return nil, RpcError{
			Code:    code,
			Message: message,
			Data:    rawErr["data"],
		}
	}
	result, ok := obj["result"].(map[string]any)
	if !ok {
		return nil, ProtocolError{Message: "MCP 响应缺少 result"}
	}
	return result, nil
}

func (c *StdioClient) notify(ctx context.Context, method string, params map[string]any) error {
	if err := c.ensureStarted(ctx); err != nil {
		return err
	}

	c.mu.Lock()
	stdin := c.stdin
	c.mu.Unlock()

	payload := map[string]any{
		"jsonrpc": rpcVersion,
		"method":  method,
	}
	if params != nil {
		payload["params"] = params
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	c.writeMu.Lock()
	_, writeErr := stdin.Write(append(encoded, '\n'))
	c.writeMu.Unlock()
	if writeErr != nil {
		return DisconnectedError{Message: "MCP stdin 写入失败"}
	}
	return nil
}

func (c *StdioClient) reserveID() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	value := c.nextID
	c.nextID++
	return value
}

func optionalString(value any) *string {
	text, ok := value.(string)
	if !ok {
		return nil
	}
	cleaned := strings.TrimSpace(text)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}
