package mcp

import (
	"context"
	"encoding/base64"
	"strings"
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
	server                   ServerConfig
	remoteToolNameByToolName map[string]string
	pool                     *Pool
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
				Message:    "MCP tool not registered",
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
				Message:    "MCP client borrow failed: " + err.Error(),
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
				Message:    "MCP tool returned error",
				Details: map[string]any{
					"tool_name": toolName,
					"server_id": e.server.ServerID,
					"content":   result.Content,
				},
			},
			DurationMs: durationMs(started),
		}
	}

	content, attachments := splitMCPContent(result.Content)
	return tools.ExecutionResult{
		ResultJSON:   map[string]any{"content": content},
		ContentParts: attachments,
		DurationMs:   durationMs(started),
	}
}

func splitMCPContent(content []map[string]any) ([]map[string]any, []tools.ContentAttachment) {
	if len(content) == 0 {
		return content, nil
	}
	cleaned := make([]map[string]any, 0, len(content))
	attachments := make([]tools.ContentAttachment, 0)
	for _, item := range content {
		if strings.EqualFold(strings.TrimSpace(stringFromAny(item["type"])), "image") {
			next, attachment, ok := imageContentAttachment(item)
			cleaned = append(cleaned, next)
			if ok {
				attachments = append(attachments, attachment)
			}
			continue
		}
		cleaned = append(cleaned, item)
	}
	return cleaned, attachments
}

func imageContentAttachment(item map[string]any) (map[string]any, tools.ContentAttachment, bool) {
	mimeType := firstMCPString(item["mimeType"], item["mime_type"])
	if mimeType == "" {
		mimeType = "image/png"
	}
	dataText := strings.TrimSpace(stringFromAny(item["data"]))
	data, err := decodeMCPImageData(dataText)
	if err != nil || len(data) == 0 {
		return map[string]any{
			"type":     "image",
			"mimeType": mimeType,
			"error":    "invalid_image_data",
		}, tools.ContentAttachment{}, false
	}
	return map[string]any{
		"type":     "image",
		"mimeType": mimeType,
		"bytes":    len(data),
		"attached": true,
	}, tools.ContentAttachment{MimeType: mimeType, Data: data}, true
}

func decodeMCPImageData(value string) ([]byte, error) {
	if index := strings.Index(value, ","); strings.HasPrefix(value, "data:") && index >= 0 {
		value = value[index+1:]
	}
	if data, err := base64.StdEncoding.DecodeString(value); err == nil {
		return data, nil
	}
	return base64.RawStdEncoding.DecodeString(value)
}

func firstMCPString(values ...any) string {
	for _, value := range values {
		if text := strings.TrimSpace(stringFromAny(value)); text != "" {
			return text
		}
	}
	return ""
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
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
	case AuthRequiredError:
		details := map[string]any{
			"tool_name":     toolName,
			"server_id":     serverID,
			"auth_required": true,
			"reason":        typed.Reason,
		}
		if typed.StatusCode > 0 {
			details["status_code"] = typed.StatusCode
		}
		return &tools.ExecutionError{
			ErrorClass: ErrorClassMcpProtocolError,
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
			Message:    "MCP tool call failed",
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
