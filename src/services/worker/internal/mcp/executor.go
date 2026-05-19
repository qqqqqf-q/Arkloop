package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"arkloop/services/shared/objectstore"
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
	resourceURIByToolName    map[string]string
	pool                     *Pool
}

func NewToolExecutor(server ServerConfig, remote map[string]string, resourceURIs map[string]string, pool *Pool) *ToolExecutor {
	toolMap := map[string]string{}
	for key, value := range remote {
		toolMap[key] = value
	}
	uriMap := map[string]string{}
	for key, value := range resourceURIs {
		uriMap[key] = value
	}
	return &ToolExecutor{
		server:                   server,
		remoteToolNameByToolName: toolMap,
		resourceURIByToolName:    uriMap,
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

	resultJSON := map[string]any{"content": content}

	resourceURI := e.resourceURIByToolName[toolName]
	if resourceURI != "" {
		resourceContent, err := client.ReadResource(callCtx, resourceURI, timeoutMs)
		if err != nil {
			slog.WarnContext(ctx, "mcp ext-apps: read resource failed", "tool_name", toolName, "resource_uri", resourceURI, "err", err.Error())
		} else if resourceContent.Text == "" && len(resourceContent.Blob) == 0 {
			slog.WarnContext(ctx, "mcp ext-apps: resource content empty", "tool_name", toolName, "resource_uri", resourceURI)
		} else {
			data := []byte(resourceContent.Text)
			if len(data) == 0 {
				data = resourceContent.Blob
			}
			mimeType := resourceContent.MimeType
			if mimeType == "" {
				mimeType = "text/html;profile=mcp-app"
			}

			var csp map[string]any
			if resourceContent.Meta != nil {
				if metaUI, ok := resourceContent.Meta["ui"].(map[string]any); ok {
					if cspRaw, ok := metaUI["csp"].(map[string]any); ok {
						csp = cspRaw
					}
				}
			}

			store := e.pool.ArtifactStore()
			if store == nil {
				slog.WarnContext(ctx, "mcp ext-apps: artifact store nil, falling back to attachment", "tool_name", toolName)
				attachments = append([]tools.ContentAttachment{{
					MimeType: mimeType,
					Data:     data,
					URI:      resourceContent.URI,
					Text:     resourceContent.Text,
				}}, attachments...)
			} else {
				key := buildMcpAppArtifactKey(execCtx, toolName)
				filename := fmt.Sprintf("mcp-app-%s.html", toolName)
				accountID := "_anonymous"
				if execCtx.AccountID != nil {
					accountID = execCtx.AccountID.String()
				}
				var threadID *string
				if execCtx.ThreadID != nil {
					value := execCtx.ThreadID.String()
					threadID = &value
				}
				metadata := objectstore.ArtifactMetadata(objectstore.ArtifactOwnerKindRun, execCtx.RunID.String(), accountID, threadID)
				putErr := store.PutObject(ctx, key, data, objectstore.PutOptions{
					ContentType: mimeType,
					Metadata:    metadata,
				})
				if putErr != nil {
					slog.ErrorContext(ctx, "mcp ext-apps: put artifact failed", "tool_name", toolName, "key", key, "err", putErr.Error())
				} else {
					resultJSON["resources"] = []map[string]any{
						{
							"key":       key,
							"uri":       resourceURI,
							"filename":  filename,
							"size":      len(data),
							"mime_type": mimeType,
							"csp":       csp,
						},
					}
					slog.InfoContext(ctx, "mcp ext-apps: artifact uploaded", "tool_name", toolName, "key", key, "size", len(data))
				}
			}
		}
	}

	return tools.ExecutionResult{
		ResultJSON:   resultJSON,
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
		itemType := strings.TrimSpace(stringFromAny(item["type"]))
		if strings.EqualFold(itemType, "image") {
			next, attachment, ok := imageContentAttachment(item)
			cleaned = append(cleaned, next)
			if ok {
				attachments = append(attachments, attachment)
			}
			continue
		}
		if strings.EqualFold(itemType, "resource") {
			next, attachment, ok := resourceContentAttachment(item)
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

func resourceContentAttachment(item map[string]any) (map[string]any, tools.ContentAttachment, bool) {
	mimeType := firstMCPString(item["mimeType"], item["mime_type"])
	if mimeType == "" {
		mimeType = "text/html"
	}
	uri := strings.TrimSpace(stringFromAny(item["uri"]))
	text := strings.TrimSpace(stringFromAny(item["text"]))
	data := []byte(text)
	if len(data) == 0 {
		dataText := strings.TrimSpace(stringFromAny(item["data"]))
		if decoded, err := base64.StdEncoding.DecodeString(dataText); err == nil {
			data = decoded
		}
	}
	if uri == "" && len(data) == 0 {
		return map[string]any{
			"type":     "resource",
			"mimeType": mimeType,
			"error":    "invalid_resource_data",
		}, tools.ContentAttachment{}, false
	}
	return map[string]any{
		"type":     "resource",
		"mimeType": mimeType,
		"uri":      uri,
		"bytes":    len(data),
		"attached": true,
	}, tools.ContentAttachment{
		MimeType: mimeType,
		Data:     data,
		URI:      uri,
		Text:     text,
	}, true
}

func buildMcpAppArtifactKey(execCtx tools.ExecutionContext, toolName string) string {
	accountID := "_anonymous"
	if execCtx.AccountID != nil {
		accountID = execCtx.AccountID.String()
	}
	return fmt.Sprintf("%s/%s/mcp-app-%s.html", accountID, execCtx.RunID.String(), toolName)
}
