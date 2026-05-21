package catalogapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	httpkit "arkloop/services/api/internal/http/httpkit"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/shared/mcpinstall"
	sharedenvironmentref "arkloop/services/shared/environmentref"

	"github.com/google/uuid"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpToolCallRequest struct {
	ServerID  string         `json:"server_id"`
	ToolName  string         `json:"tool_name"`
	Arguments map[string]any `json:"arguments"`
}

type mcpToolCallResponse struct {
	Content []map[string]any `json:"content"`
	IsError bool             `json:"is_error"`
}

func mcpToolCallEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	installsRepo *data.ProfileMCPInstallsRepository,
	secretsRepo *data.SecretsRepository,
) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		traceID := ""
		if authService == nil {
			httpkit.WriteAuthNotConfigured(w, traceID)
			return
		}
		actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
		if !ok {
			return
		}

		var req mcpToolCallRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, "invalid_request", "invalid request body", traceID, nil)
			return
		}
		req.ServerID = strings.TrimSpace(req.ServerID)
		req.ToolName = strings.TrimSpace(req.ToolName)
		if req.ServerID == "" || req.ToolName == "" {
			httpkit.WriteError(w, http.StatusBadRequest, "invalid_request", "server_id and tool_name are required", traceID, nil)
			return
		}

		profileRef := sharedenvironmentref.BuildProfileRef(actor.AccountID, &actor.UserID)
		install, err := findMCPInstallByKey(r.Context(), installsRepo, actor.AccountID, profileRef, req.ServerID)
		if err != nil {
			slog.WarnContext(r.Context(), "mcp_tool_call_lookup_failed",
				"account_id", actor.AccountID,
				"server_id", req.ServerID,
				"error", err.Error(),
			)
			httpkit.WriteError(w, http.StatusInternalServerError, "lookup_failed", err.Error(), traceID, nil)
			return
		}
		if install == nil {
			httpkit.WriteNotFound(w, r)
			return
		}

		result, err := callMCPTool(r.Context(), install, secretsRepo, req.ToolName, req.Arguments)
		if err != nil {
			slog.WarnContext(r.Context(), "mcp_tool_call_failed",
				"account_id", actor.AccountID,
				"server_id", req.ServerID,
				"tool_name", req.ToolName,
				"error", err.Error(),
			)
			httpkit.WriteError(w, http.StatusBadGateway, "mcp.tool_error", err.Error(), traceID, nil)
			return
		}

		httpkit.WriteJSON(w, traceID, http.StatusOK, result)
	}
}

func findMCPInstallByKey(ctx context.Context, repo *data.ProfileMCPInstallsRepository, accountID uuid.UUID, profileRef string, key string) (*data.ProfileMCPInstall, error) {
	installs, err := repo.ListByProfile(ctx, accountID, profileRef)
	if err != nil {
		return nil, err
	}
	for i := range installs {
		if installs[i].InstallKey == key {
			return &installs[i], nil
		}
	}
	return nil, nil
}

func callMCPTool(ctx context.Context, install *data.ProfileMCPInstall, secretsRepo *data.SecretsRepository, toolName string, arguments map[string]any) (mcpToolCallResponse, error) {
	spec := map[string]any{}
	if len(install.LaunchSpecJSON) > 0 {
		if err := json.Unmarshal(install.LaunchSpecJSON, &spec); err != nil {
			return mcpToolCallResponse{}, fmt.Errorf("invalid launch spec: %w", err)
		}
	}
	if strings.TrimSpace(install.Transport) != "" {
		if rawTransport, ok := spec["transport"]; !ok || strings.TrimSpace(asString(rawTransport)) == "" {
			spec["transport"] = strings.TrimSpace(install.Transport)
		}
	}

	serverCfg, err := mcpinstall.ParseServerConfig(install.InstallKey, spec, 30000)
	if err != nil {
		return mcpToolCallResponse{}, fmt.Errorf("parse server config: %w", err)
	}

	// Decrypt auth headers if present
	if install.AuthHeadersSecretID != nil {
		plain, err := secretsRepo.DecryptByID(ctx, *install.AuthHeadersSecretID)
		if err == nil && plain != nil && *plain != "" {
			var headers map[string]string
			if json.Unmarshal([]byte(*plain), &headers) == nil {
				for key, value := range headers {
					key = strings.TrimSpace(key)
					if key == "" {
						continue
					}
					if serverCfg.Headers == nil {
						serverCfg.Headers = map[string]string{}
					}
					serverCfg.Headers[key] = value
				}
			} else {
				// Fallback: treat as bearer token
				if serverCfg.Headers == nil {
					serverCfg.Headers = map[string]string{}
				}
				serverCfg.Headers["Authorization"] = "Bearer " + *plain
			}
		}
	}

	timeoutMs := serverCfg.CallTimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 30000
	}

	callCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	impl := &sdkmcp.Implementation{Name: "arkloop-api", Version: "0"}
	client := sdkmcp.NewClient(impl, nil)

	var transport sdkmcp.Transport
	switch serverCfg.Transport {
	case "stdio", "":
		transport = mcpinstall.BuildCommandTransport(serverCfg)
	case "http_sse":
		transport = mcpinstall.BuildSSETransport(serverCfg, mcpinstall.NewSafeHTTPClient())
	case "streamable_http":
		transport = mcpinstall.BuildStreamableTransport(serverCfg, mcpinstall.NewSafeHTTPClient(), nil)
	default:
		return mcpToolCallResponse{}, fmt.Errorf("unsupported transport: %s", serverCfg.Transport)
	}

	session, err := client.Connect(callCtx, transport, nil)
	if err != nil {
		return mcpToolCallResponse{}, fmt.Errorf("mcp connect: %w", err)
	}
	defer session.Close()

	result, err := session.CallTool(callCtx, &sdkmcp.CallToolParams{
		Name:      toolName,
		Arguments: arguments,
	})
	if err != nil {
		return mcpToolCallResponse{}, fmt.Errorf("mcp call tool: %w", err)
	}

	content := serializeContent(result.Content)
	return mcpToolCallResponse{
		Content: content,
		IsError: result.IsError,
	}, nil
}

func serializeContent(content []sdkmcp.Content) []map[string]any {
	result := make([]map[string]any, 0, len(content))
	for _, c := range content {
		switch tc := c.(type) {
		case *sdkmcp.TextContent:
			result = append(result, map[string]any{"type": "text", "text": tc.Text})
		case *sdkmcp.ImageContent:
			result = append(result, map[string]any{"type": "image", "data": tc.Data, "mimeType": tc.MIMEType})
		case *sdkmcp.AudioContent:
			result = append(result, map[string]any{"type": "audio", "data": tc.Data, "mimeType": tc.MIMEType})
		case *sdkmcp.ResourceLink:
			result = append(result, map[string]any{"type": "resource_link", "uri": tc.URI})
		case *sdkmcp.EmbeddedResource:
			result = append(result, map[string]any{"type": "resource", "resource": tc.Resource})
		default:
			if b, err := json.Marshal(c); err == nil {
				var m map[string]any
				if json.Unmarshal(b, &m) == nil {
					result = append(result, m)
				}
			}
		}
	}
	return result
}

func asString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case fmt.Stringer:
		return s.String()
	default:
		return ""
	}
}
