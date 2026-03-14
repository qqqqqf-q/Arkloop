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

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var toolNameSafeRegex = regexp.MustCompile(`[^A-Za-z0-9_]+`)

type Registration struct {
	AgentSpecs []tools.AgentToolSpec
	LlmSpecs   []llm.ToolSpec
	Executors  map[string]tools.Executor
}

// DiscoverFromDB 按 account_id 从数据库加载 MCP 配置并发现工具。
// 若该 account 无活跃配置，返回空 Registration（不报错）。
func DiscoverFromDB(ctx context.Context, dbPool *pgxpool.Pool, accountID uuid.UUID, mcpPool *Pool) (Registration, error) {
	cfg, err := LoadConfigFromDB(ctx, dbPool, accountID)
	if err != nil {
		return Registration{}, err
	}
	if cfg == nil || len(cfg.Servers) == 0 {
		return Registration{Executors: map[string]tools.Executor{}}, nil
	}
	return Discover(ctx, *cfg, mcpPool)
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
	if pool == nil {
		pool = NewPool()
	}

	type serverResult struct {
		index  int
		server ServerConfig
		tools  []Tool
	}

	results := make([]serverResult, len(cfg.Servers))
	var wg sync.WaitGroup
	for i, server := range cfg.Servers {
		wg.Add(1)
		go func(idx int, srv ServerConfig) {
			defer wg.Done()
			client, err := pool.Borrow(ctx, srv)
			if err != nil {
				return
			}
			toolsList, err := client.ListTools(ctx, srv.CallTimeoutMs)
			if err != nil || len(toolsList) == 0 {
				return
			}
			results[idx] = serverResult{index: idx, server: srv, tools: toolsList}
		}(i, server)
	}
	wg.Wait()

	discoveredByServer := []struct {
		server ServerConfig
		tools  []Tool
	}{}

	baseCounts := map[string]int{}

	for _, r := range results {
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

	usedNames := map[string]struct{}{}
	agentSpecs := []tools.AgentToolSpec{}
	llmSpecs := []llm.ToolSpec{}
	executors := map[string]tools.Executor{}

	for _, entry := range discoveredByServer {
		server := entry.server
		remoteMap := map[string]string{}

		for _, tool := range entry.tools {
			base := mcpToolBaseName(server.ServerID, tool.Name)
			internal := base
			if baseCounts[base] > 1 {
				raw := mcpToolRawName(server.ServerID, tool.Name)
				internal = base + "__" + shortHash(raw)
			}
			internal = ensureUniqueToolName(internal, usedNames)
			remoteMap[internal] = tool.Name

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
				RiskLevel:   tools.RiskLevelHigh,
				SideEffects: true,
			})
			llmSpecs = append(llmSpecs, llm.ToolSpec{
				Name:        internal,
				Description: stringPtr(description),
				JSONSchema:  tool.InputSchema,
			})
		}

		executor := NewToolExecutor(server, remoteMap, pool)
		for internalName := range remoteMap {
			executors[internalName] = executor
		}
	}

	sort.Slice(agentSpecs, func(i, j int) bool { return agentSpecs[i].Name < agentSpecs[j].Name })
	sort.Slice(llmSpecs, func(i, j int) bool { return llmSpecs[i].Name < llmSpecs[j].Name })

	return Registration{
		AgentSpecs: agentSpecs,
		LlmSpecs:   llmSpecs,
		Executors:  executors,
	}, nil
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
