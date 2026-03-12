package mcp

import (
	"bufio"
	"context"
	"encoding/json"
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
	maxStderrLineBytes     = 1024
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

	mu           sync.Mutex
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdout       io.ReadCloser
	stderr       io.ReadCloser
	closed       bool
	disconnected bool // 子进程意外退出后由 handleDisconnect 设置
	nextID       int64
	pending      map[int64]chan map[string]any

	// initMu 保证并发调用时 initialize 握手只发送一次
	initMu      sync.Mutex
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
	stdout := c.stdout
	stderr := c.stderr
	pending := c.pending
	c.pending = map[int64]chan map[string]any{}
	c.mu.Unlock()

	for _, ch := range pending {
		close(ch)
	}

	if stdin != nil {
		_ = stdin.Close()
	}
	if stdout != nil {
		_ = stdout.Close()
	}
	if stderr != nil {
		_ = stderr.Close()
	}

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
	return nil
}

// IsHealthy 返回 false 表示子进程已退出或 client 已被关闭，Pool 应重建。
// 未启动（lazy init）的 client 视为健康。
func (c *StdioClient) IsHealthy(_ context.Context) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.disconnected {
		return false
	}
	if c.cmd != nil && c.cmd.ProcessState != nil {
		return false
	}
	return true
}

func (c *StdioClient) ensureStarted(ctx context.Context) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return DisconnectedError{Message: "MCP client closed"}
	}
	if c.cmd != nil {
		c.mu.Unlock()
		return nil
	}
	server := c.server
	c.mu.Unlock()

	if ctx != nil && ctx.Err() != nil {
		return TimeoutError{Message: "MCP call cancelled"}
	}

	cmd := exec.Command(server.Command, server.Args...)
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
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return err
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return err
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = cmd.Process.Kill()
		return DisconnectedError{Message: "MCP client closed"}
	}
	c.cmd = cmd
	c.stdin = stdin
	c.stdout = stdout
	c.stderr = stderr
	c.mu.Unlock()

	go c.readLoop(stdout)
	go c.stderrLoop(server, stderr)
	return nil
}

// buildServerEnv 仅传递 ServerConfig.Env 中显式声明的条目，永不继承父进程环境变量。
// exec.Cmd.Env = nil 会导致子进程继承全部父进程环境，make 保证返回非 nil 切片。
func buildServerEnv(server ServerConfig) []string {
	env := make([]string, 0, len(server.Env))
	for key, value := range server.Env {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}
	return env
}

func (c *StdioClient) readLoop(stdout io.Reader) {
	reader := bufio.NewReader(stdout)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			c.handleDisconnect()
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

func (c *StdioClient) stderrLoop(server ServerConfig, stderr io.Reader) {
	reader := bufio.NewReader(stderr)
	for {
		line, err := readLimitedLine(reader, maxStderrLineBytes)
		if err != nil {
			return
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		writeMcpStderrLog(server, trimmed)
	}
}

func readLimitedLine(reader *bufio.Reader, limit int) (string, error) {
	if limit <= 0 {
		limit = maxStderrLineBytes
	}
	buf := make([]byte, 0, limit)
	for {
		part, isPrefix, err := reader.ReadLine()
		if err != nil {
			return "", err
		}
		if len(buf) < limit {
			remaining := limit - len(buf)
			if len(part) > remaining {
				buf = append(buf, part[:remaining]...)
			} else {
				buf = append(buf, part...)
			}
		}
		if !isPrefix {
			break
		}
	}
	return string(buf), nil
}

func writeMcpStderrLog(server ServerConfig, line string) {
	accountID := any(nil)
	if strings.TrimSpace(server.AccountID) != "" {
		accountID = server.AccountID
	}

	record := map[string]any{
		"ts":        formatTimestamp(time.Now()),
		"level":     "warn",
		"msg":       "mcp.stderr",
		"component": "mcp_stderr",
		"server_id": server.ServerID,
		"account_id":    accountID,
		"line":      strings.ToValidUTF8(line, "�"),
	}

	encoded, err := json.Marshal(record)
	if err != nil {
		encoded = []byte(`{"level":"error","msg":"mcp.stderr.marshal_failed","component":"mcp_stderr"}`)
	}
	encoded = append(encoded, '\n')
	_, _ = os.Stdout.Write(encoded)
}

func formatTimestamp(now time.Time) string {
	utc := now.UTC().Format("2006-01-02T15:04:05.000Z07:00")
	if len(utc) >= 6 && utc[len(utc)-6:] == "+00:00" {
		return utc[:len(utc)-6] + "Z"
	}
	return utc
}

func (c *StdioClient) handleDisconnect() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	cmd := c.cmd
	stdin := c.stdin
	stdout := c.stdout
	stderr := c.stderr
	pending := c.pending
	c.pending = map[int64]chan map[string]any{}
	c.cmd = nil
	c.stdin = nil
	c.stdout = nil
	c.stderr = nil
	c.initialized = false
	c.disconnected = true
	c.mu.Unlock()

	for _, ch := range pending {
		close(ch)
	}

	if stdin != nil {
		_ = stdin.Close()
	}
	if stdout != nil {
		_ = stdout.Close()
	}
	if stderr != nil {
		_ = stderr.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
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
	c.initMu.Lock()
	defer c.initMu.Unlock()

	if c.initialized {
		return nil
	}

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

func (c *StdioClient) request(ctx context.Context, method string, params map[string]any, timeoutMs int) (map[string]any, error) {
	if err := c.ensureStarted(ctx); err != nil {
		return nil, err
	}

	id := c.reserveID()
	ch := make(chan map[string]any, 1)

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, DisconnectedError{Message: "MCP client closed"}
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
		c.handleDisconnect()
		return nil, DisconnectedError{Message: "MCP stdin write failed"}
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, TimeoutError{Message: "MCP call cancelled"}
	case <-time.After(timeout):
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, TimeoutError{Message: "MCP call timed out"}
	case resp, ok := <-ch:
		if !ok {
			return nil, DisconnectedError{Message: "MCP client disconnected"}
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
		return nil, ProtocolError{Message: "MCP response missing result"}
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
		c.handleDisconnect()
		return DisconnectedError{Message: "MCP stdin write failed"}
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
