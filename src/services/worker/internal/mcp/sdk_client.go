package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	sharedmcpinstall "arkloop/services/shared/mcpinstall"
	sharedmcpoauth "arkloop/services/shared/mcpoauth"
	"arkloop/services/worker/internal/llm"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/google/uuid"
)

type sdkClient struct {
	session *sdkmcp.ClientSession
	server  sharedmcpinstall.ServerConfig
	closed  atomic.Bool
}

func newSDKClient(ctx context.Context, server sharedmcpinstall.ServerConfig, authStore AuthStore) (*sdkClient, error) {
	impl := &sdkmcp.Implementation{Name: "arkloop", Version: "0"}
	client := sdkmcp.NewClient(impl, &sdkmcp.ClientOptions{
		Capabilities: &sdkmcp.ClientCapabilities{
			Extensions: map[string]any{
				"io.modelcontextprotocol/ui": map[string]any{
					"mimeTypes": []string{"text/html;profile=mcp-app"},
				},
			},
		},
	})

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

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, classifySDKError(err)
	}

	return &sdkClient{
		session: session,
		server:  server,
	}, nil
}

func (c *sdkClient) ListTools(ctx context.Context, timeoutMs int) ([]Tool, error) {
	serverID := strings.TrimSpace(c.server.ServerID)
	slog.DebugContext(ctx, "mcp: ListTools start", "server_id", serverID, "timeout_ms", timeoutMs)

	if c.closed.Load() {
		slog.DebugContext(ctx, "mcp: ListTools aborted, client closed", "server_id", serverID)
		return nil, DisconnectedError{Message: "MCP client closed"}
	}

	ctx, cancel := applyTimeout(ctx, timeoutMs)
	defer cancel()

	var out []Tool
	for tool, err := range c.session.Tools(ctx, nil) {
		if err != nil {
			slog.DebugContext(ctx, "mcp: ListTools error from session.Tools", "server_id", serverID, "error", err.Error())
			return nil, classifySDKError(err)
		}
		if tool == nil {
			continue
		}
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			slog.DebugContext(ctx, "mcp: ListTools skipped tool with empty name", "server_id", serverID)
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
			Meta:        tool.GetMeta(),
			Annotations: convertAnnotations(tool.Annotations),
		})
	}
	if out == nil {
		out = []Tool{}
	}

	toolNames := make([]string, len(out))
	for i, t := range out {
		toolNames[i] = t.Name
	}
	slog.DebugContext(ctx, "mcp: ListTools complete",
		"server_id", serverID,
		"tool_count", len(out),
		"tool_names", toolNames,
	)
	return out, nil
}

func (c *sdkClient) CallTool(ctx context.Context, name string, arguments map[string]any, timeoutMs int) (ToolCallResult, error) {
	serverID := strings.TrimSpace(c.server.ServerID)
	slog.DebugContext(ctx, "mcp: CallTool start",
		"server_id", serverID,
		"tool_name", name,
		"timeout_ms", timeoutMs,
	)

	if c.closed.Load() {
		slog.DebugContext(ctx, "mcp: CallTool aborted, client closed", "server_id", serverID, "tool_name", name)
		return ToolCallResult{}, DisconnectedError{Message: "MCP client closed"}
	}

	ctx, cancel := applyTimeout(ctx, timeoutMs)
	defer cancel()

	result, err := c.session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: arguments,
	})
	if err != nil {
		slog.DebugContext(ctx, "mcp: CallTool error",
			"server_id", serverID,
			"tool_name", name,
			"error", err.Error(),
		)
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

	slog.DebugContext(ctx, "mcp: CallTool complete",
		"server_id", serverID,
		"tool_name", name,
		"content_count", len(content),
		"is_error", isError,
	)
	return ToolCallResult{
		Content: content,
		IsError: isError,
	}, nil
}

func (c *sdkClient) ListResources(ctx context.Context, timeoutMs int) ([]Resource, error) {
	if c.closed.Load() {
		return nil, DisconnectedError{Message: "MCP client closed"}
	}

	ctx, cancel := applyTimeout(ctx, timeoutMs)
	defer cancel()

	var out []Resource
	for res, err := range c.session.Resources(ctx, nil) {
		if err != nil {
			return nil, classifySDKError(err)
		}
		if res == nil {
			continue
		}
		uri := strings.TrimSpace(res.URI)
		if uri == "" {
			continue
		}
		annotations := map[string]any{}
		if res.Annotations != nil {
			annotations["audience"] = res.Annotations.Audience
			annotations["priority"] = res.Annotations.Priority
			if res.Annotations.LastModified != "" {
				annotations["lastModified"] = res.Annotations.LastModified
			}
		}
		out = append(out, Resource{
			URI:         uri,
			Name:        strings.TrimSpace(res.Name),
			MimeType:    strings.TrimSpace(res.MIMEType),
			Annotations: annotations,
			Meta:        coerceToMap(res.GetMeta()),
		})
	}
	if out == nil {
		out = []Resource{}
	}
	return out, nil
}

func (c *sdkClient) ReadResource(ctx context.Context, uri string, timeoutMs int) (ResourceContent, error) {
	serverID := strings.TrimSpace(c.server.ServerID)
	slog.DebugContext(ctx, "mcp: ReadResource start",
		"server_id", serverID,
		"uri", uri,
		"timeout_ms", timeoutMs,
	)

	if c.closed.Load() {
		slog.DebugContext(ctx, "mcp: ReadResource aborted, client closed", "server_id", serverID, "uri", uri)
		return ResourceContent{}, DisconnectedError{Message: "MCP client closed"}
	}

	ctx, cancel := applyTimeout(ctx, timeoutMs)
	defer cancel()

	result, err := c.session.ReadResource(ctx, &sdkmcp.ReadResourceParams{
		URI: uri,
	})
	if err != nil {
		slog.DebugContext(ctx, "mcp: ReadResource error",
			"server_id", serverID,
			"uri", uri,
			"error", err.Error(),
		)
		return ResourceContent{}, classifySDKError(err)
	}

	if result == nil || len(result.Contents) == 0 {
		slog.DebugContext(ctx, "mcp: ReadResource empty contents", "server_id", serverID, "uri", uri)
		return ResourceContent{}, ProtocolError{Message: "resources/read returned empty contents"}
	}

	content := result.Contents[0]
	slog.DebugContext(ctx, "mcp: ReadResource complete",
		"server_id", serverID,
		"uri", uri,
		"mime_type", strings.TrimSpace(content.MIMEType),
		"text_len", len(content.Text),
		"blob_len", len(content.Blob),
	)
	return ResourceContent{
		URI:      strings.TrimSpace(content.URI),
		MimeType: strings.TrimSpace(content.MIMEType),
		Text:     content.Text,
		Blob:     content.Blob,
		Meta:     coerceToMap(content.Meta.GetMeta()),
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

func convertAnnotations(a *sdkmcp.ToolAnnotations) *llm.ToolAnnotations {
	if a == nil {
		return nil
	}
	return &llm.ToolAnnotations{
		DestructiveHint: a.DestructiveHint,
		IdempotentHint:  a.IdempotentHint,
		OpenWorldHint:   a.OpenWorldHint,
		ReadOnlyHint:    a.ReadOnlyHint,
		Title:           a.Title,
	}
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
