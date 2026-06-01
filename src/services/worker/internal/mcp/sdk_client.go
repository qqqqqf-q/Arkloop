package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	sharedmcpinstall "arkloop/services/shared/mcpinstall"
	sharedmcpoauth "arkloop/services/shared/mcpoauth"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type sdkClient struct {
	session *sdkmcp.ClientSession
	server  sharedmcpinstall.ServerConfig
	closed  atomic.Bool
}

func newSDKClient(ctx context.Context, server sharedmcpinstall.ServerConfig, authStore AuthStore) (*sdkClient, error) {
	impl := &sdkmcp.Implementation{Name: "arkloop", Version: "0"}
	client := sdkmcp.NewClient(impl, nil)

	var transport sdkmcp.Transport
	switch server.Transport {
	case "stdio", "":
		transport = sharedmcpinstall.BuildCommandTransport(server)
	case "http_sse":
		transport = sharedmcpinstall.BuildSSETransport(server, sharedmcpinstall.NewSafeHTTPClient())
	case "streamable_http":
		safeClient := sharedmcpinstall.NewSafeHTTPClient()
		var onRefresh func(updated *sharedmcpoauth.AuthState)
		if authStore != nil && server.AuthSecretID != "" {
			onRefresh = func(updated *sharedmcpoauth.AuthState) {
				_ = persistOAuthRefresh(context.Background(), authStore, server, updated)
			}
		}
		transport = sharedmcpinstall.BuildStreamableTransport(server, safeClient, onRefresh)
	default:
		return nil, fmt.Errorf("mcp: unsupported transport: %s", server.Transport)
	}

	// Connect 包含建链 + initialize 握手，go-sdk 把 session 后台循环绑定在传入的 ctx 上，
	// 因此不能用带超时的派生 ctx（cancel 会连带杀掉 pool 复用的 session）。改用竞速：
	// 用原 ctx 跑 Connect，握手超过 connectTimeout 就放弃，被放弃的 goroutine 随 run ctx 结束回收。
	connectTimeout := time.Duration(server.CallTimeoutMs) * time.Millisecond
	if connectTimeout <= 0 {
		connectTimeout = time.Duration(defaultCallTimeoutMs) * time.Millisecond
	}
	type connectResult struct {
		session *sdkmcp.ClientSession
		err     error
	}
	resultCh := make(chan connectResult, 1)
	go func() {
		session, err := client.Connect(ctx, transport, nil)
		resultCh <- connectResult{session: session, err: err}
	}()

	timer := time.NewTimer(connectTimeout)
	defer timer.Stop()
	select {
	case res := <-resultCh:
		if res.err != nil {
			return nil, classifySDKError(res.err)
		}
		return &sdkClient{
			session: res.session,
			server:  server,
		}, nil
	case <-timer.C:
		return nil, TimeoutError{Message: "MCP connect timed out"}
	case <-ctx.Done():
		return nil, classifySDKError(ctx.Err())
	}
}

func (c *sdkClient) ListTools(ctx context.Context, timeoutMs int) ([]Tool, error) {
	if c.closed.Load() {
		return nil, DisconnectedError{Message: "MCP client closed"}
	}

	ctx, cancel := applyTimeout(ctx, timeoutMs)
	defer cancel()

	var out []Tool
	for tool, err := range c.session.Tools(ctx, nil) {
		if err != nil {
			return nil, classifySDKError(err)
		}
		if tool == nil {
			continue
		}
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		schema := map[string]any{}
		if tool.InputSchema != nil {
			schema = coerceToMap(tool.InputSchema)
		}
		out = append(out, Tool{
			Name:        name,
			Title:       stringPtr(tool.Title),
			Description: stringPtr(tool.Description),
			InputSchema: schema,
		})
	}
	if out == nil {
		out = []Tool{}
	}
	return out, nil
}

func (c *sdkClient) CallTool(ctx context.Context, name string, arguments map[string]any, timeoutMs int) (ToolCallResult, error) {
	if c.closed.Load() {
		return ToolCallResult{}, DisconnectedError{Message: "MCP client closed"}
	}

	ctx, cancel := applyTimeout(ctx, timeoutMs)
	defer cancel()

	result, err := c.session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: arguments,
	})
	if err != nil {
		return ToolCallResult{}, classifySDKError(err)
	}

	content := []map[string]any{}
	if result != nil {
		for _, item := range result.Content {
			m, err := contentToMap(item)
			if err != nil {
				m = map[string]any{"type": "text", "text": fmt.Sprintf("[content decode error: %s]", err.Error())}
			}
			content = append(content, m)
		}
	}

	isError := false
	if result != nil {
		isError = result.IsError
	}

	return ToolCallResult{
		Content: content,
		IsError: isError,
	}, nil
}

func (c *sdkClient) IsHealthy(ctx context.Context) bool {
	if c.closed.Load() {
		return false
	}

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	err := c.session.Ping(pingCtx, nil)
	if err == nil {
		return true
	}
	// Connection-level errors mean the session is dead.
	var disconn DisconnectedError
	var timeout TimeoutError
	if errors.As(err, &disconn) || errors.As(err, &timeout) {
		return false
	}
	if errors.Is(err, sdkmcp.ErrConnectionClosed) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}
	// RPC errors (method not found, etc.) mean the connection is still alive.
	return true
}

func (c *sdkClient) ServerInstructions() string {
	if c.closed.Load() {
		return ""
	}
	if ir := c.session.InitializeResult(); ir != nil {
		return ir.Instructions
	}
	return ""
}

func (c *sdkClient) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	return c.session.Close()
}

func contentToMap(c sdkmcp.Content) (map[string]any, error) {
	raw, err := c.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func classifySDKError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, sdkmcp.ErrConnectionClosed) {
		return DisconnectedError{Message: "MCP connection closed: " + err.Error()}
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return TimeoutError{Message: "MCP call timed out: " + err.Error()}
	}

	// OAuth — check sentinel errors from ArkloopOAuthHandler
	if errors.Is(err, sharedmcpinstall.ErrOAuthAuthRequired) || errors.Is(err, sharedmcpinstall.ErrOAuthRefreshFailed) {
		return AuthRequiredError{Reason: "oauth_required", Cause: err}
	}

	var rpcErr *jsonrpc.Error
	if errors.As(err, &rpcErr) {
		rpc := RpcError{
			Code:    intPtr(int(rpcErr.Code)),
			Message: rpcErr.Message,
		}
		if len(rpcErr.Data) > 0 {
			var data any
			if json.Unmarshal(rpcErr.Data, &data) == nil {
				rpc.Data = data
			}
		}
		return rpc
	}

	return ProtocolError{Message: err.Error()}
}

func applyTimeout(ctx context.Context, timeoutMs int) (context.Context, context.CancelFunc) {
	if timeoutMs > 0 {
		return context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	}
	return ctx, func() {}
}

func intPtr(v int) *int { return &v }

func persistOAuthRefresh(ctx context.Context, store AuthStore, server sharedmcpinstall.ServerConfig, updated *sharedmcpoauth.AuthState) error {
	if store == nil || server.AuthSecretID == "" {
		return nil
	}
	secretID, err := uuid.Parse(server.AuthSecretID)
	if err != nil {
		return err
	}
	return store.Save(ctx, secretID, sharedmcpinstall.AuthPayload{
		Headers: cloneStringMap(server.Headers),
		Env:     cloneStringMap(server.Env),
		OAuth:   updated,
	})
}

func coerceToMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}
