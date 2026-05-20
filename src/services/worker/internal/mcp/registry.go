package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

var toolNameSafeRegex = regexp.MustCompile(`[^A-Za-z0-9_]+`)

type Registration struct {
	AgentSpecs      []tools.AgentToolSpec
	LlmSpecs        []llm.ToolSpec
	Executors       map[string]tools.Executor
	UIOnlyExecutors map[string]tools.Executor
	Instructions    map[string]string // serverID -> instructions from MCP InitializeResult
}

type DiscoverDiagnostics struct {
	ServerCount int
	ToolCount   int
	Servers     []ServerDiagnostics
}

type ServerDiagnostics struct {
	ServerID     string
	Transport    string
	DurationMs   int64
	Outcome      string
	ErrorClass   string
	ToolCount    int
	ReusedClient bool
}

// DiscoverFromDB 按 account_id 从数据库加载 MCP 配置并发现工具。
// 若该 account 无活跃配置，返回空 Registration（不报错）。
func DiscoverFromDB(ctx context.Context, dbPool DiscoveryQueryer, accountID uuid.UUID, profileRef string, workspaceRef string, mcpPool *Pool) (Registration, error) {
	reg, _, err := DiscoverFromDBWithDiagnostics(ctx, dbPool, accountID, profileRef, workspaceRef, mcpPool)
	return reg, err
}

func DiscoverFromDBWithDiagnostics(ctx context.Context, dbPool DiscoveryQueryer, accountID uuid.UUID, profileRef string, workspaceRef string, mcpPool *Pool) (Registration, DiscoverDiagnostics, error) {
	cfg, err := LoadConfigFromDB(ctx, dbPool, accountID, profileRef, workspaceRef)
	if err != nil {
		return Registration{}, DiscoverDiagnostics{}, err
	}
	if cfg == nil || len(cfg.Servers) == 0 {
		return Registration{Executors: map[string]tools.Executor{}}, DiscoverDiagnostics{}, nil
	}
	return DiscoverWithDiagnostics(ctx, *cfg, mcpPool)
}

func DiscoverFromEnv(ctx context.Context, pool *Pool) (Registration, error) {
	cfg, err := LoadConfigFromEnv()
	if err != nil {
		return Registration{}, err
	}
	if cfg == nil || len(cfg.Servers) == 0 {
		return Registration{Executors: map[string]tools.Executor{}}, nil
	}
	return Discover(ctx, *cfg, pool)
}

func Discover(ctx context.Context, cfg Config, pool *Pool) (Registration, error) {
	reg, _, err := DiscoverWithDiagnostics(ctx, cfg, pool)
	return reg, err
}

func DiscoverWithDiagnostics(ctx context.Context, cfg Config, pool *Pool) (Registration, DiscoverDiagnostics, error) {
	if pool == nil {
		pool = NewPool()
	}

	type serverResult struct {
		index       int
		server      ServerConfig
		tools       []Tool
		instructions string
		diag        ServerDiagnostics
	}

	results := make([]serverResult, len(cfg.Servers))
	var wg sync.WaitGroup
	for i, server := range cfg.Servers {
		wg.Add(1)
		go func(idx int, srv ServerConfig) {
			defer wg.Done()
			startedAt := time.Now()
			result := serverResult{
				index:  idx,
				server: srv,
				diag: ServerDiagnostics{
					ServerID:   strings.TrimSpace(srv.ServerID),
					Transport:  strings.TrimSpace(srv.Transport),
					Outcome:    "borrow_failed",
					ErrorClass: "",
				},
			}
			client, meta, err := pool.BorrowWithMeta(ctx, srv)
			if err != nil {
				result.diag.DurationMs = time.Since(startedAt).Milliseconds()
				result.diag.ErrorClass = classifyDiscoverError(err)
				results[idx] = result
				return
			}
			result.diag.ReusedClient = meta.Reused
			toolsList, err := client.ListTools(ctx, srv.CallTimeoutMs)
			result.diag.DurationMs = time.Since(startedAt).Milliseconds()
			if err != nil {
				result.diag.Outcome = "list_failed"
				result.diag.ErrorClass = classifyDiscoverError(err)
				results[idx] = result
				return
			}
			if len(toolsList) == 0 {
				result.diag.Outcome = "empty"
				results[idx] = result
				return
			}
			result.tools = toolsList
			result.instructions = client.ServerInstructions()
			result.diag.Outcome = "ok"
			result.diag.ToolCount = len(toolsList)
			results[idx] = result
		}(i, server)
	}
	wg.Wait()

	diag := DiscoverDiagnostics{
		ServerCount: len(cfg.Servers),
		Servers:     make([]ServerDiagnostics, 0, len(results)),
	}
	discoveredByServer := []struct {
		server ServerConfig
		tools  []Tool
	}{}

	baseCounts := map[string]int{}
	instructions := map[string]string{}

	for _, r := range results {
		diag.Servers = append(diag.Servers, r.diag)
		if r.instructions != "" {
			instructions[strings.TrimSpace(r.server.ServerID)] = r.instructions
		}
		if len(r.tools) == 0 {
			continue
		}
		discoveredByServer = append(discoveredByServer, struct {
			server ServerConfig
			tools  []Tool
		}{server: r.server, tools: r.tools})
		for _, tool := range r.tools {
			base := mcpToolBaseName(r.server.ServerID, tool.Name)
			baseCounts[base]++
		}
	}
	sort.Slice(diag.Servers, func(i, j int) bool {
		if diag.Servers[i].DurationMs == diag.Servers[j].DurationMs {
			return diag.Servers[i].ServerID < diag.Servers[j].ServerID
		}
		return diag.Servers[i].DurationMs > diag.Servers[j].DurationMs
	})

	usedNames := map[string]struct{}{}
	agentSpecs := []tools.AgentToolSpec{}
	llmSpecs := []llm.ToolSpec{}
	executors := map[string]tools.Executor{}
	uiOnlyExecutors := map[string]tools.Executor{}

	for _, entry := range discoveredByServer {
		server := entry.server
		remoteMap := map[string]string{}
		resourceURIs := map[string]string{}
		uiOnlyRemoteMap := map[string]string{}

		for _, tool := range entry.tools {
			if !isToolVisibleToModel(tool) {
				if isToolAppOnly(tool) {
					base := mcpToolBaseName(server.ServerID, tool.Name)
					internal := base
					if baseCounts[base] > 1 {
						raw := mcpToolRawName(server.ServerID, tool.Name)
						internal = base + "__" + shortHash(raw)
					}
					internal = ensureUniqueToolName(internal, usedNames)
					uiOnlyRemoteMap[internal] = tool.Name
				}
				continue
			}
			base := mcpToolBaseName(server.ServerID, tool.Name)
			internal := base
			if baseCounts[base] > 1 {
				raw := mcpToolRawName(server.ServerID, tool.Name)
				internal = base + "__" + shortHash(raw)
			}
			internal = ensureUniqueToolName(internal, usedNames)
			remoteMap[internal] = tool.Name

			if uri := extractToolResourceURI(tool); uri != "" {
				resourceURIs[internal] = uri
			}

			description := ""
			if tool.Description != nil && strings.TrimSpace(*tool.Description) != "" {
				description = strings.TrimSpace(*tool.Description)
			} else if tool.Title != nil && strings.TrimSpace(*tool.Title) != "" {
				description = strings.TrimSpace(*tool.Title)
			} else {
				description = "MCP tool: " + tool.Name
			}

			agentSpecs = append(agentSpecs, tools.AgentToolSpec{
				Name:        internal,
				Version:     "1",
				Description: description,
				RiskLevel:   mcpRiskLevel(tool.Annotations),
				SideEffects: mcpSideEffects(tool.Annotations),
				Annotations: tool.Annotations,
				ResourceURI: resourceURIs[internal],
			})
			llmSpecs = append(llmSpecs, llm.ToolSpec{
				Name:        internal,
				Description: stringPtr(description),
				JSONSchema:  tool.InputSchema,
				Annotations: tool.Annotations,
			})
		}

		executor := NewToolExecutor(server, remoteMap, resourceURIs, pool)
		for internalName := range remoteMap {
			executors[internalName] = executor
		}

		if len(uiOnlyRemoteMap) > 0 {
			uiExecutor := NewToolExecutor(server, uiOnlyRemoteMap, nil, pool)
			for internalName := range uiOnlyRemoteMap {
				uiOnlyExecutors[internalName] = uiExecutor
			}
		}
	}

	sort.Slice(agentSpecs, func(i, j int) bool { return agentSpecs[i].Name < agentSpecs[j].Name })
	sort.Slice(llmSpecs, func(i, j int) bool { return llmSpecs[i].Name < llmSpecs[j].Name })
	diag.ToolCount = len(llmSpecs)

	return Registration{
		AgentSpecs:      agentSpecs,
		LlmSpecs:        llmSpecs,
		Executors:       executors,
		UIOnlyExecutors: uiOnlyExecutors,
		Instructions:    instructions,
	}, diag, nil
}

func classifyDiscoverError(err error) string {
	switch err.(type) {
	case TimeoutError:
		return "timeout"
	case DisconnectedError:
		return "disconnected"
	case ProtocolError:
		return "protocol"
	case RpcError:
		return "rpc"
	case AuthRequiredError:
		return "auth_required"
	default:
		return "unknown"
	}
}

func mcpToolRawName(serverID string, toolName string) string {
	return "mcp__" + serverID + "__" + toolName
}

func mcpToolBaseName(serverID string, toolName string) string {
	raw := mcpToolRawName(serverID, toolName)
	cleaned := toolNameSafeRegex.ReplaceAllString(raw, "_")
	cleaned = strings.Trim(cleaned, "_")
	if cleaned == "" {
		return "mcp_tool"
	}
	return cleaned
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:8]
}

func ensureUniqueToolName(name string, used map[string]struct{}) string {
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

func stringPtr(value string) *string {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}

func mcpRiskLevel(a *llm.ToolAnnotations) tools.RiskLevel {
	if a == nil {
		return tools.RiskLevelHigh
	}
	if a.ReadOnlyHint {
		return tools.RiskLevelLow
	}
	if a.DestructiveHint != nil && *a.DestructiveHint {
		return tools.RiskLevelHigh
	}
	return tools.RiskLevelMedium
}

func mcpSideEffects(a *llm.ToolAnnotations) bool {
	if a == nil {
		return true
	}
	return !a.ReadOnlyHint
}

func isToolVisibleToModel(tool Tool) bool {
	if tool.Meta == nil {
		return true
	}
	metaUI, _ := tool.Meta["ui"].(map[string]any)
	if metaUI == nil {
		return true
	}
	rawVisibility, ok := metaUI["visibility"].([]any)
	if !ok || len(rawVisibility) == 0 {
		return true
	}
	for _, v := range rawVisibility {
		if strings.TrimSpace(asString(v)) == "model" {
			return true
		}
	}
	return false
}

func isToolAppOnly(tool Tool) bool {
	if tool.Meta == nil {
		return false
	}
	metaUI, _ := tool.Meta["ui"].(map[string]any)
	if metaUI == nil {
		return false
	}
	rawVisibility, ok := metaUI["visibility"].([]any)
	if !ok || len(rawVisibility) == 0 {
		return false
	}
	hasApp := false
	for _, v := range rawVisibility {
		if strings.TrimSpace(asString(v)) == "app" {
			hasApp = true
		}
	}
	return hasApp
}

func extractToolResourceURI(tool Tool) string {
	if tool.Meta == nil {
		return ""
	}
	metaUI, _ := tool.Meta["ui"].(map[string]any)
	if metaUI != nil {
		uri := strings.TrimSpace(asString(metaUI["resourceUri"]))
		if uri != "" {
			return uri
		}
	}
	return strings.TrimSpace(asString(tool.Meta["ui/resourceUri"]))
}
