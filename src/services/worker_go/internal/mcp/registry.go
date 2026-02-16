package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"arkloop/services/worker_go/internal/llm"
	"arkloop/services/worker_go/internal/tools"
)

var toolNameSafeRegex = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

type Registration struct {
	AgentSpecs []tools.AgentToolSpec
	LlmSpecs   []llm.ToolSpec
	Executors  map[string]tools.Executor
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

	discoveredByServer := []struct {
		server ServerConfig
		tools  []Tool
	}{}

	baseCounts := map[string]int{}

	for _, server := range cfg.Servers {
		client, err := pool.Borrow(ctx, server)
		if err != nil {
			continue
		}
		toolsList, err := client.ListTools(ctx, server.CallTimeoutMs)
		if err != nil {
			continue
		}
		if len(toolsList) == 0 {
			continue
		}
		discoveredByServer = append(discoveredByServer, struct {
			server ServerConfig
			tools  []Tool
		}{server: server, tools: toolsList})
		for _, tool := range toolsList {
			base := mcpToolBaseName(server.ServerID, tool.Name)
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
				description = "MCP 工具：" + tool.Name
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
