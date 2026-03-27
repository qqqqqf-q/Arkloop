package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	sharedmcpinstall "arkloop/services/shared/mcpinstall"
	workerCrypto "arkloop/services/worker/internal/crypto"

	"github.com/google/uuid"
)

const mcpConfigFileEnv = "ARKLOOP_MCP_CONFIG_FILE"

const defaultCallTimeoutMs = 10000

type ServerConfig = sharedmcpinstall.ServerConfig

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
	return sharedmcpinstall.ParseServerConfig(serverID, payload, defaultCallTimeoutMs)
}

type DiscoveryQueryer = sharedmcpinstall.DiscoveryQueryer

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

	installs, err := sharedmcpinstall.LoadEnabledInstalls(ctx, pool, accountID, profileRef, workspaceRef)
	if err != nil {
		return nil, fmt.Errorf("mcp: load enabled installs: %w", err)
	}
	if len(installs) == 0 {
		return nil, nil
	}

	servers := make([]ServerConfig, 0, len(installs))
	for _, install := range installs {
		headers := map[string]string{}
		if install.EncryptedValue != nil && install.KeyVersion != nil {
			plainBytes, err := workerCrypto.DecryptWithKeyVersion(*install.EncryptedValue, *install.KeyVersion)
			if err != nil {
				continue
			}
			if err := json.Unmarshal(plainBytes, &headers); err == nil {
			} else {
				plaintext := strings.TrimSpace(string(plainBytes))
				if plaintext != "" {
					headers["Authorization"] = "Bearer " + plaintext
				}
			}
		} else if install.EncryptedValue != nil {
			continue
		}
		server, err := sharedmcpinstall.ServerConfigFromInstall(install, headers, defaultCallTimeoutMs)
		if err != nil {
			continue
		}
		if err := sharedmcpinstall.CheckHostRequirement(server, install.HostRequirement); err != nil {
			continue
		}
		servers = append(servers, server)
	}

	if len(servers) == 0 {
		return nil, nil
	}
	return &Config{Servers: servers}, nil
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
