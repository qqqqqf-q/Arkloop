package mcp

import (
	"context"
	"time"

	"arkloop/services/worker/internal/tools"
)

const (
	ErrorClassMcpTimeout       = "mcp.timeout"
	ErrorClassMcpDisconnected  = "mcp.disconnected"
	ErrorClassMcpRpcError      = "mcp.rpc_error"
	ErrorClassMcpProtocolError = "mcp.protocol_error"
	ErrorClassMcpToolError     = "mcp.tool_error"
)

type ToolExecutor struct {
	server                    ServerConfig
	remoteToolNameByToolName  map[string]string
	pool                      *Pool
}

func NewToolExecutor(server ServerConfig, remote map[string]string, pool *Pool) *ToolExecutor {
	toolMap := map[string]string{}
	for key, value := range remote {
		toolMap[key] = value
	}
	return &ToolExecutor{
		server:                   server,
		remoteToolNameByToolName: toolMap,
		pool:                     pool,
	}
}

func (e *ToolExecutor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	started := time.Now()

	remoteName := e.remoteToolNameByToolName[toolName]
	if remoteName == "" {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: ErrorClassMcpProtocolError,
				Message:    "MCP 工具未注册",
				Details:    map[string]any{"tool_name": toolName, "server_id": e.server.ServerID},
			},
			DurationMs: durationMs(started),
		}
	}

	timeoutMs := e.server.CallTimeoutMs
	if execCtx.TimeoutMs != nil && *execCtx.TimeoutMs > 0 {
		timeoutMs = *execCtx.TimeoutMs
	}

	pool := e.pool
	if pool == nil {
		pool = NewPool()
	}

	client, err := pool.Borrow(ctx, e.server)
	if err != nil {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: ErrorClassMcpProtocolError,
				Message:    "MCP client 获取失败",
				Details:    map[string]any{"tool_name": toolName, "server_id": e.server.ServerID},
			},
			DurationMs: durationMs(started),
		}
	}

	callCtx := ctx
	if timeoutMs > 0 {
		timeout := time.Duration(timeoutMs) * time.Millisecond
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	result, err := client.CallTool(callCtx, remoteName, args, timeoutMs)
	if err != nil {
		return tools.ExecutionResult{
			Error:      toExecutionError(err, toolName, e.server.ServerID),
			DurationMs: durationMs(started),
		}
	}

	if result.IsError {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: ErrorClassMcpToolError,
				Message:    "MCP 工具返回错误",
				Details: map[string]any{
					"tool_name": toolName,
					"server_id": e.server.ServerID,
					"content":   result.Content,
				},
			},
			DurationMs: durationMs(started),
		}
	}

	return tools.ExecutionResult{
		ResultJSON: map[string]any{"content": result.Content},
		DurationMs: durationMs(started),
	}
}

func toExecutionError(err error, toolName string, serverID string) *tools.ExecutionError {
	switch typed := err.(type) {
	case TimeoutError:
		return &tools.ExecutionError{
			ErrorClass: ErrorClassMcpTimeout,
			Message:    typed.Error(),
			Details:    map[string]any{"tool_name": toolName, "server_id": serverID},
		}
	case DisconnectedError:
		return &tools.ExecutionError{
			ErrorClass: ErrorClassMcpDisconnected,
			Message:    typed.Error(),
			Details:    map[string]any{"tool_name": toolName, "server_id": serverID},
		}
	case RpcError:
		details := map[string]any{"tool_name": toolName, "server_id": serverID}
		if typed.Code != nil {
			details["code"] = *typed.Code
		}
		if typed.Data != nil {
			details["data"] = typed.Data
		}
		return &tools.ExecutionError{
			ErrorClass: ErrorClassMcpRpcError,
			Message:    typed.Error(),
			Details:    details,
		}
	case ProtocolError:
		return &tools.ExecutionError{
			ErrorClass: ErrorClassMcpProtocolError,
			Message:    typed.Error(),
			Details:    map[string]any{"tool_name": toolName, "server_id": serverID},
		}
	default:
		return &tools.ExecutionError{
			ErrorClass: ErrorClassMcpProtocolError,
			Message:    "MCP 工具调用失败",
			Details:    map[string]any{"tool_name": toolName, "server_id": serverID},
		}
	}
}

func durationMs(started time.Time) int {
	elapsed := time.Since(started)
	millis := int(elapsed / time.Millisecond)
	if millis < 0 {
		return 0
	}
	return millis
}

