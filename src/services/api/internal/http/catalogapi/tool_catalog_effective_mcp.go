package catalogapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"arkloop/services/api/internal/data"
	sharedenvironmentref "arkloop/services/shared/environmentref"
	sharedmcpinstall "arkloop/services/shared/mcpinstall"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/google/uuid"
)

const (
	effectiveToolCatalogMCPGroup = "mcp"
	effectiveMCPConfigFileEnv    = "ARKLOOP_MCP_CONFIG_FILE"
	effectiveMCPDefaultTimeoutMs = 10000
	effectiveToolCatalogCacheEnv = "__env__"
)

type effectiveToolCatalogCache struct {
	ttl     time.Duration
	entries sync.Map
}

type effectiveToolCatalogCacheEntry struct {
	tools    []toolCatalogItem
	cachedAt time.Time
}

func newEffectiveToolCatalogCache(ttl time.Duration) *effectiveToolCatalogCache {
	return &effectiveToolCatalogCache{ttl: ttl}
}

func (c *effectiveToolCatalogCache) GetEnv(ctx context.Context) ([]toolCatalogItem, error) {
	return c.get(ctx, effectiveToolCatalogCacheEnv, func(context.Context) ([]toolCatalogItem, error) {
		servers, err := loadEffectiveMCPConfigFromEnv()
		if err != nil {
			return nil, err
		}
		return discoverEffectiveMCPTools(ctx, servers)
	})
}

func (c *effectiveToolCatalogCache) GetAccount(ctx context.Context, pool data.DB, accountID uuid.UUID, userID uuid.UUID) ([]toolCatalogItem, error) {
	if accountID == uuid.Nil || userID == uuid.Nil {
		return nil, nil
	}
	cacheKey := accountID.String() + "|" + userID.String()
	return c.get(ctx, cacheKey, func(context.Context) ([]toolCatalogItem, error) {
		servers, err := loadEffectiveMCPConfigFromDB(ctx, pool, accountID, userID)
		if err != nil {
			return nil, err
		}
		return discoverEffectiveMCPTools(ctx, servers)
	})
}

func (c *effectiveToolCatalogCache) get(ctx context.Context, key string, load func(context.Context) ([]toolCatalogItem, error)) ([]toolCatalogItem, error) {
	if c == nil || c.ttl <= 0 {
		return load(ctx)
	}
	if raw, ok := c.entries.Load(key); ok {
		entry := raw.(effectiveToolCatalogCacheEntry)
		if time.Since(entry.cachedAt) < c.ttl {
			return cloneToolCatalogItems(entry.tools), nil
		}
	}
	tools, err := load(ctx)
	if err != nil {
		return nil, err
	}
	c.entries.Store(key, effectiveToolCatalogCacheEntry{tools: cloneToolCatalogItems(tools), cachedAt: time.Now()})
	return tools, nil
}

type effectiveMCPServerConfig = sharedmcpinstall.ServerConfig

type effectiveMCPTool struct {
	Name        string
	Title       *string
	Description *string
}

func loadEffectiveMCPConfigFromEnv() ([]effectiveMCPServerConfig, error) {
	raw := strings.TrimSpace(os.Getenv(effectiveMCPConfigFileEnv))
	if raw == "" {
		return nil, nil
	}
	path := expandUserPath(raw)
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("mcp effective catalog: config file not found: %s", raw)
	}

	var parsed any
	if err := json.Unmarshal(content, &parsed); err != nil {
		return nil, fmt.Errorf("mcp effective catalog: config file is not valid json")
	}
	root, ok := parsed.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("mcp effective catalog: config root must be an object")
	}
	rawServers := root["mcpServers"]
	if rawServers == nil {
		rawServers = root["mcp_servers"]
	}
	if rawServers == nil {
		return nil, nil
	}
	serverMap, ok := rawServers.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("mcp effective catalog: mcpServers must be an object")
	}
	serverIDs := make([]string, 0, len(serverMap))
	for serverID := range serverMap {
		serverIDs = append(serverIDs, serverID)
	}
	sort.Strings(serverIDs)

	servers := make([]effectiveMCPServerConfig, 0, len(serverIDs))
	for _, serverID := range serverIDs {
		payload, ok := serverMap[serverID].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("mcp effective catalog: server %q must be an object", serverID)
		}
		server, err := parseEffectiveMCPServerConfig(serverID, payload)
		if err != nil {
			return nil, err
		}
		servers = append(servers, server)
	}
	return servers, nil
}

func loadEffectiveMCPConfigFromDB(ctx context.Context, pool data.DB, accountID uuid.UUID, userID uuid.UUID) ([]effectiveMCPServerConfig, error) {
	if pool == nil || accountID == uuid.Nil || userID == uuid.Nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	profileRepo, err := data.NewProfileRegistriesRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("mcp effective catalog: init profile repo: %w", err)
	}
	workspaceRepo, err := data.NewWorkspaceRegistriesRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("mcp effective catalog: init workspace repo: %w", err)
	}
	profileRef := sharedenvironmentref.BuildProfileRef(accountID, &userID)
	workspaceRef, err := ensureDefaultWorkspaceForProfile(ctx, profileRepo, workspaceRepo, accountID, userID, profileRef)
	if err != nil {
		return nil, fmt.Errorf("mcp effective catalog: resolve workspace: %w", err)
	}
	if strings.TrimSpace(workspaceRef) == "" {
		return nil, nil
	}
	installs, err := sharedmcpinstall.LoadEnabledInstalls(ctx, pool, accountID, profileRef, workspaceRef)
	if err != nil {
		return nil, fmt.Errorf("mcp effective catalog: load enabled installs: %w", err)
	}

	var (
		keyRing    catalogKeyRing
		keyRingErr error
	)
	decrypt := func(_ context.Context, encrypted string, keyVersion *int) ([]byte, error) {
		if keyVersion == nil {
			return nil, fmt.Errorf("tool_catalog_effective_mcp: missing key version")
		}
		if keyRing == nil && keyRingErr == nil {
			keyRing, keyRingErr = newEffectiveCatalogKeyRing()
		}
		if keyRingErr != nil {
			return nil, keyRingErr
		}
		if keyRing == nil {
			return nil, fmt.Errorf("tool_catalog_effective_mcp: key ring missing")
		}
		return keyRing.Decrypt(encrypted, *keyVersion)
	}

	servers := sharedmcpinstall.ServerConfigsFromInstalls(ctx, installs, decrypt, effectiveMCPDefaultTimeoutMs)
	return servers, nil
}

func parseEffectiveMCPServerConfig(serverID string, payload map[string]any) (effectiveMCPServerConfig, error) {
	return sharedmcpinstall.ParseServerConfig(serverID, payload, effectiveMCPDefaultTimeoutMs)
}

func discoverEffectiveMCPTools(ctx context.Context, servers []effectiveMCPServerConfig) ([]toolCatalogItem, error) {
	if len(servers) == 0 {
		return nil, nil
	}
	type discovered struct {
		server effectiveMCPServerConfig
		tools  []effectiveMCPTool
	}
	discoveredByServer := make([]discovered, 0, len(servers))
	baseCounts := map[string]int{}
	for _, server := range servers {
		tools, err := listEffectiveMCPServerTools(ctx, server)
		if err != nil || len(tools) == 0 {
			continue
		}
		discoveredByServer = append(discoveredByServer, discovered{server: server, tools: tools})
		for _, tool := range tools {
			base := effectiveMCPToolBaseName(server.ServerID, tool.Name)
			baseCounts[base]++
		}
	}
	usedNames := map[string]struct{}{}
	items := []toolCatalogItem{}
	for _, entry := range discoveredByServer {
		for _, tool := range entry.tools {
			base := effectiveMCPToolBaseName(entry.server.ServerID, tool.Name)
			internalName := base
			if baseCounts[base] > 1 {
				internalName = base + "__" + effectiveMCPShortHash(effectiveMCPToolRawName(entry.server.ServerID, tool.Name))
			}
			internalName = ensureEffectiveMCPUniqueToolName(internalName, usedNames)

			label := strings.TrimSpace(tool.Name)
			if tool.Title != nil && strings.TrimSpace(*tool.Title) != "" {
				label = strings.TrimSpace(*tool.Title)
			}
			description := "MCP tool: " + strings.TrimSpace(tool.Name)
			if tool.Description != nil && strings.TrimSpace(*tool.Description) != "" {
				description = strings.TrimSpace(*tool.Description)
			} else if tool.Title != nil && strings.TrimSpace(*tool.Title) != "" {
				description = strings.TrimSpace(*tool.Title)
			}

			items = append(items, toolCatalogItem{
				Name:              internalName,
				Label:             label,
				LLMDescription:    description,
				HasOverride:       false,
				DescriptionSource: toolDescriptionSourceDefault,
			})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func listEffectiveMCPServerTools(ctx context.Context, server effectiveMCPServerConfig) ([]effectiveMCPTool, error) {
	return listEffectiveMCPToolsWithSDK(ctx, server)
}

func listEffectiveMCPToolsWithSDK(ctx context.Context, server effectiveMCPServerConfig) ([]effectiveMCPTool, error) {
	impl := &sdkmcp.Implementation{Name: "arkloop-api", Version: "0"}
	client := sdkmcp.NewClient(impl, nil)

	var transport sdkmcp.Transport
	switch server.Transport {
	case "stdio", "":
		transport = sharedmcpinstall.BuildCommandTransport(server)
	case "http_sse":
		transport = sharedmcpinstall.BuildSSETransport(server, sharedmcpinstall.NewSafeHTTPClient())
	case "streamable_http":
		transport = sharedmcpinstall.BuildStreamableTransport(server, sharedmcpinstall.NewSafeHTTPClient(), nil)
	default:
		return nil, fmt.Errorf("mcp effective catalog: unsupported transport: %s", server.Transport)
	}

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp effective catalog: connect failed: %w", err)
	}
	defer func() { _ = session.Close() }()

	timeoutMs := server.CallTimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = effectiveMCPDefaultTimeoutMs
	}
	listCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	var out []effectiveMCPTool
	for tool, err := range session.Tools(listCtx, nil) {
		if err != nil {
			return nil, fmt.Errorf("mcp effective catalog: list tools failed: %w", err)
		}
		if tool == nil {
			continue
		}
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		out = append(out, effectiveMCPTool{
			Name:        name,
			Title:       effectiveMCPStringPtr(tool.Title),
			Description: effectiveMCPStringPtr(tool.Description),
		})
	}
	return out, nil
}

func effectiveMCPStringPtr(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

func expandUserPath(path string) string {
	if path == "" || path[0] != '~' {
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

func effectiveMCPToolRawName(serverID string, toolName string) string {
	return "mcp__" + serverID + "__" + toolName
}

func effectiveMCPToolBaseName(serverID string, toolName string) string {
	raw := effectiveMCPToolRawName(serverID, toolName)
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z':
			return r
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '_':
			return r
		default:
			return '_'
		}
	}, raw)
	cleaned = strings.Trim(cleaned, "_")
	if cleaned == "" {
		return "mcp_tool"
	}
	return cleaned
}

func effectiveMCPShortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:8]
}

func ensureEffectiveMCPUniqueToolName(name string, used map[string]struct{}) string {
	if _, ok := used[name]; !ok {
		used[name] = struct{}{}
		return name
	}
	index := 2
	for {
		candidate := name + "_" + strconv.Itoa(index)
		if _, ok := used[candidate]; !ok {
			used[candidate] = struct{}{}
			return candidate
		}
		index++
	}
}

func cloneToolCatalogItems(items []toolCatalogItem) []toolCatalogItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]toolCatalogItem, len(items))
	copy(out, items)
	return out
}

