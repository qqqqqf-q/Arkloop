package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	workerCrypto "arkloop/services/worker/internal/crypto"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const mcpConfigFileEnv = "ARKLOOP_MCP_CONFIG_FILE"

const defaultCallTimeoutMs = 10000

type ServerConfig struct {
	ServerID  string
	AccountID string // 用于 pool 隔离；全局（env 加载）工具为空字符串
	Transport string // stdio / http_sse / streamable_http
	// HTTP 传输字段
	URL     string
	Headers map[string]string
	// stdio 传输字段
	Command          string
	Args             []string
	Cwd              *string
	Env              map[string]string
	InheritParentEnv bool
	// 通用
	CallTimeoutMs int
}

type Config struct {
	Servers []ServerConfig
}

func LoadConfigFromEnv() (*Config, error) {
	raw := strings.TrimSpace(os.Getenv(mcpConfigFileEnv))
	if raw == "" {
		return nil, nil
	}
	path := expandUser(raw)
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s file not found: %s", mcpConfigFileEnv, raw)
	}

	var parsed any
	if err := json.Unmarshal(content, &parsed); err != nil {
		return nil, fmt.Errorf("MCP config file is not valid JSON: %s", raw)
	}
	root, ok := parsed.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("MCP config file must be a JSON object")
	}

	rawServers, ok := root["mcpServers"]
	if !ok {
		rawServers = root["mcp_servers"]
	}
	serverMap, ok := rawServers.(map[string]any)
	if !ok {
		if rawServers == nil {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("mcpServers must be an object")
	}

	serverIDs := make([]string, 0, len(serverMap))
	for serverID := range serverMap {
		serverIDs = append(serverIDs, serverID)
	}
	sort.Strings(serverIDs)

	servers := make([]ServerConfig, 0, len(serverIDs))
	for _, serverID := range serverIDs {
		rawCfg, ok := serverMap[serverID].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("mcpServers[%q] must be an object", serverID)
		}
		server, err := parseServerConfig(serverID, rawCfg)
		if err != nil {
			return nil, err
		}
		servers = append(servers, server)
	}
	return &Config{Servers: servers}, nil
}

func parseServerConfig(serverID string, payload map[string]any) (ServerConfig, error) {
	cleanedID := strings.TrimSpace(serverID)
	if cleanedID == "" {
		return ServerConfig{}, fmt.Errorf("MCP server_id must not be empty")
	}

	transport := strings.TrimSpace(asString(payload["transport"]))
	if transport == "" {
		transport = "stdio"
	}
	transport = strings.ToLower(transport)

	timeout := defaultCallTimeoutMs
	rawTimeout := payload["callTimeoutMs"]
	if rawTimeout == nil {
		rawTimeout = payload["call_timeout_ms"]
	}
	if rawTimeout != nil {
		switch typed := rawTimeout.(type) {
		case float64:
			timeout = int(typed)
			if typed != float64(timeout) {
				return ServerConfig{}, fmt.Errorf("MCP server %q callTimeoutMs must be an integer", cleanedID)
			}
		case int:
			timeout = typed
		case int64:
			timeout = int(typed)
		default:
			return ServerConfig{}, fmt.Errorf("MCP server %q callTimeoutMs must be an integer", cleanedID)
		}
	}
	if timeout <= 0 {
		return ServerConfig{}, fmt.Errorf("MCP server %q callTimeoutMs must be a positive integer", cleanedID)
	}

	switch transport {
	case "stdio":
		// 下方继续解析 command/args 等字段
	case "http_sse", "streamable_http":
		url := strings.TrimSpace(asString(payload["url"]))
		if url == "" {
			return ServerConfig{}, fmt.Errorf("MCP server %q missing url for %s transport", cleanedID, transport)
		}
		headers := map[string]string{}
		if rawHeaders, ok := payload["headers"].(map[string]any); ok {
			for key, value := range rawHeaders {
				if strings.TrimSpace(key) == "" {
					continue
				}
				headers[strings.TrimSpace(key)] = asString(value)
			}
		}
		if raw, ok := payload["bearer_token"]; ok && raw != nil {
			token, ok := raw.(string)
			if !ok {
				return ServerConfig{}, fmt.Errorf("MCP server %q bearer_token must be a string", cleanedID)
			}
			cleaned := strings.TrimSpace(token)
			if cleaned != "" {
				headers["Authorization"] = "Bearer " + cleaned
			}
		}
		return ServerConfig{
			ServerID:      cleanedID,
			Transport:     transport,
			URL:           url,
			Headers:       headers,
			CallTimeoutMs: timeout,
		}, nil
	default:
		return ServerConfig{}, fmt.Errorf("MCP server %q transport not supported: %s", cleanedID, transport)
	}

	command := strings.TrimSpace(asString(payload["command"]))
	if command == "" {
		return ServerConfig{}, fmt.Errorf("MCP server %q missing command", cleanedID)
	}

	args := []string{}
	if rawArgs, ok := payload["args"].([]any); ok {
		for _, item := range rawArgs {
			text, ok := item.(string)
			if !ok {
				return ServerConfig{}, fmt.Errorf("MCP server %q args must be a string array", cleanedID)
			}
			cleaned := strings.TrimSpace(text)
			if cleaned == "" {
				continue
			}
			args = append(args, cleaned)
		}
	} else if payload["args"] != nil {
		return ServerConfig{}, fmt.Errorf("MCP server %q args must be a string array", cleanedID)
	}

	var cwd *string
	if rawCwd, ok := payload["cwd"]; ok && rawCwd != nil {
		value, ok := rawCwd.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return ServerConfig{}, fmt.Errorf("MCP server %q cwd must be a string", cleanedID)
		}
		cleaned := strings.TrimSpace(value)
		cwd = &cleaned
	}

	env := map[string]string{}
	if rawEnv, ok := payload["env"]; ok && rawEnv != nil {
		mapped, ok := rawEnv.(map[string]any)
		if !ok {
			return ServerConfig{}, fmt.Errorf("MCP server %q env must be an object", cleanedID)
		}
		for key, value := range mapped {
			if strings.TrimSpace(key) == "" {
				return ServerConfig{}, fmt.Errorf("MCP server %q env key invalid", cleanedID)
			}
			text, ok := value.(string)
			if !ok {
				return ServerConfig{}, fmt.Errorf("MCP server %q env[%q] must be a string", cleanedID, key)
			}
			env[strings.TrimSpace(key)] = text
		}
	}

	return ServerConfig{
		ServerID:         cleanedID,
		Command:          command,
		Args:             args,
		Cwd:              cwd,
		Env:              env,
		InheritParentEnv: false,
		CallTimeoutMs:    timeout,
		Transport:        transport,
	}, nil
}

type DiscoveryQueryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// LoadConfigFromDB 按 account/profile/workspace 从数据库加载已启用的 MCP install。
// 返回 nil 表示当前 workspace 没有已启用配置。
func LoadConfigFromDB(ctx context.Context, pool DiscoveryQueryer, accountID uuid.UUID, profileRef string, workspaceRef string) (*Config, error) {
	if pool == nil {
		return nil, fmt.Errorf("mcp: pool must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	profileRef = strings.TrimSpace(profileRef)
	workspaceRef = strings.TrimSpace(workspaceRef)
	if accountID == uuid.Nil || profileRef == "" || workspaceRef == "" {
		return nil, nil
	}

	rows, err := pool.Query(ctx, `
		SELECT i.id, i.install_key, i.transport, i.launch_spec_json, i.host_requirement,
		       i.discovery_status, s.encrypted_value, s.key_version
		  FROM workspace_mcp_enablements w
		  JOIN profile_mcp_installs i
		    ON i.id = w.install_id
		   AND i.account_id = w.account_id
		 LEFT JOIN secrets s ON s.id = i.auth_headers_secret_id
		 WHERE w.account_id = $1
		   AND w.workspace_ref = $2
		   AND w.enabled = TRUE
		   AND i.profile_ref = $3
		 ORDER BY i.created_at ASC
	`, accountID, workspaceRef, profileRef)
	if err != nil {
		return nil, fmt.Errorf("mcp: query db: %w", err)
	}
	defer rows.Close()

	type rowData struct {
		id             uuid.UUID
		installKey     string
		transport      string
		launchSpecJSON []byte
		hostRequirement string
		discoveryStatus string
		encryptedValue *string
		keyVersion     *int
	}

	var allRows []rowData
	for rows.Next() {
		var rd rowData
		if err := rows.Scan(
			&rd.id, &rd.installKey, &rd.transport, &rd.launchSpecJSON, &rd.hostRequirement,
			&rd.discoveryStatus, &rd.encryptedValue, &rd.keyVersion,
		); err != nil {
			return nil, fmt.Errorf("mcp: scan: %w", err)
		}
		allRows = append(allRows, rd)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mcp: rows: %w", err)
	}
	if len(allRows) == 0 {
		return nil, nil
	}

	servers := make([]ServerConfig, 0, len(allRows))
	for _, rd := range allRows {
		server := ServerConfig{
			ServerID:      rd.installKey,
			AccountID:     accountID.String(),
			Transport:     rd.transport,
			CallTimeoutMs: defaultCallTimeoutMs,
			Headers:       map[string]string{},
		}

		if rd.encryptedValue != nil && rd.keyVersion != nil {
			plainBytes, err := workerCrypto.DecryptGCM(*rd.encryptedValue)
			if err != nil {
				continue
			}
			var headers map[string]string
			if err := json.Unmarshal(plainBytes, &headers); err == nil {
				server.Headers = headers
			} else {
				plaintext := strings.TrimSpace(string(plainBytes))
				if plaintext != "" {
					server.Headers["Authorization"] = "Bearer " + plaintext
				}
			}
		}

		launch := map[string]any{}
		if len(rd.launchSpecJSON) > 0 {
			if err := json.Unmarshal(rd.launchSpecJSON, &launch); err != nil {
				continue
			}
		}
		if timeout := intFromAny(launch["call_timeout_ms"]); timeout > 0 {
			server.CallTimeoutMs = timeout
		}
		switch rd.transport {
		case "http_sse", "streamable_http":
			urlValue := strings.TrimSpace(asString(launch["url"]))
			if urlValue == "" {
				continue
			}
			server.URL = urlValue
		case "stdio":
			command := strings.TrimSpace(asString(launch["command"]))
			if command == "" {
				continue
			}
			server.Command = command

			server.Args = toStringSlice(launch["args"])
			server.Cwd = optionalStringPtr(launch["cwd"])
			server.InheritParentEnv = false

			env := map[string]string{}
			if rawEnv, ok := launch["env"].(map[string]any); ok {
				for key, value := range rawEnv {
					if strings.TrimSpace(key) == "" {
						continue
					}
					env[strings.TrimSpace(key)] = asString(value)
				}
			}
			server.Env = env
		default:
			continue
		}

		servers = append(servers, server)
	}

	if len(servers) == 0 {
		return nil, nil
	}
	return &Config{Servers: servers}, nil
}

func toStringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text := strings.TrimSpace(asString(item))
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

func optionalStringPtr(value any) *string {
	text := strings.TrimSpace(asString(value))
	if text == "" {
		return nil
	}
	return &text
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	default:
		return 0
	}
}

func asString(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}

func expandUser(path string) string {
	if path == "" {
		return path
	}
	if path[0] != '~' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}
