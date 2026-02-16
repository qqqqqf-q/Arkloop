package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const mcpConfigFileEnv = "ARKLOOP_MCP_CONFIG_FILE"

const defaultCallTimeoutMs = 10000

type ServerConfig struct {
	ServerID         string
	Command          string
	Args             []string
	Cwd              *string
	Env              map[string]string
	InheritParentEnv bool
	CallTimeoutMs    int
	Transport        string
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
		return nil, fmt.Errorf("%s 指向的文件不存在: %s", mcpConfigFileEnv, raw)
	}

	var parsed any
	if err := json.Unmarshal(content, &parsed); err != nil {
		return nil, fmt.Errorf("MCP 配置文件不是合法 JSON: %s", raw)
	}
	root, ok := parsed.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("MCP 配置文件必须为 JSON 对象")
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
		return nil, fmt.Errorf("mcpServers 必须为对象")
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
			return nil, fmt.Errorf("mcpServers[%q] 必须为对象", serverID)
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
		return ServerConfig{}, fmt.Errorf("MCP server_id 不能为空")
	}

	transport := strings.TrimSpace(asString(payload["transport"]))
	if transport == "" {
		transport = "stdio"
	}
	transport = strings.ToLower(transport)

	timeout := defaultCallTimeoutMs
	if raw := payload["callTimeoutMs"]; raw == nil {
		raw = payload["call_timeout_ms"]
	} else {
		if value, ok := raw.(float64); ok {
			timeout = int(value)
		} else if value, ok := raw.(int); ok {
			timeout = value
		}
	}
	if timeout <= 0 {
		return ServerConfig{}, fmt.Errorf("MCP server %q callTimeoutMs 必须为正整数", cleanedID)
	}

	if transport != "stdio" {
		return ServerConfig{}, fmt.Errorf("MCP server %q transport 暂不支持: %s", cleanedID, transport)
	}

	command := strings.TrimSpace(asString(payload["command"]))
	if command == "" {
		return ServerConfig{}, fmt.Errorf("MCP server %q 缺少 command", cleanedID)
	}

	args := []string{}
	if rawArgs, ok := payload["args"].([]any); ok {
		for _, item := range rawArgs {
			text, ok := item.(string)
			if !ok {
				return ServerConfig{}, fmt.Errorf("MCP server %q args 必须为字符串数组", cleanedID)
			}
			cleaned := strings.TrimSpace(text)
			if cleaned == "" {
				continue
			}
			args = append(args, cleaned)
		}
	} else if payload["args"] != nil {
		return ServerConfig{}, fmt.Errorf("MCP server %q args 必须为字符串数组", cleanedID)
	}

	var cwd *string
	if rawCwd, ok := payload["cwd"]; ok && rawCwd != nil {
		value, ok := rawCwd.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return ServerConfig{}, fmt.Errorf("MCP server %q cwd 必须为字符串", cleanedID)
		}
		cleaned := strings.TrimSpace(value)
		cwd = &cleaned
	}

	env := map[string]string{}
	if rawEnv, ok := payload["env"]; ok && rawEnv != nil {
		mapped, ok := rawEnv.(map[string]any)
		if !ok {
			return ServerConfig{}, fmt.Errorf("MCP server %q env 必须为对象", cleanedID)
		}
		for key, value := range mapped {
			if strings.TrimSpace(key) == "" {
				return ServerConfig{}, fmt.Errorf("MCP server %q env key 非法", cleanedID)
			}
			text, ok := value.(string)
			if !ok {
				return ServerConfig{}, fmt.Errorf("MCP server %q env[%q] 必须为字符串", cleanedID, key)
			}
			env[strings.TrimSpace(key)] = text
		}
	}

	inherit := false
	if rawInherit, ok := payload["inheritParentEnv"]; ok {
		if rawInherit == nil {
			rawInherit = payload["inherit_parent_env"]
		}
		if value, ok := rawInherit.(bool); ok {
			inherit = value
		}
	}

	return ServerConfig{
		ServerID:         cleanedID,
		Command:          command,
		Args:             args,
		Cwd:              cwd,
		Env:              env,
		InheritParentEnv: inherit,
		CallTimeoutMs:    timeout,
		Transport:        transport,
	}, nil
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

