package builtin

import (
	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/acptool"
	"arkloop/services/worker/internal/tools/builtin/askuser"
	searchtools "arkloop/services/worker/internal/tools/builtin/search_tools"
	spawnagent "arkloop/services/worker/internal/tools/builtin/spawn_agent"
	summarizethread "arkloop/services/worker/internal/tools/builtin/summarize_thread"
	webfetch "arkloop/services/worker/internal/tools/builtin/web_fetch"
	websearch "arkloop/services/worker/internal/tools/builtin/web_search"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func AgentSpecs() []tools.AgentToolSpec {
	return []tools.AgentToolSpec{
		searchtools.AgentSpec,
		TimelineTitleAgentSpec,
		websearch.AgentSpec,
		websearch.AgentSpecTavily,
		websearch.AgentSpecSearxng,
		webfetch.AgentSpec,
		webfetch.AgentSpecJina,
		webfetch.AgentSpecFirecrawl,
		webfetch.AgentSpecBasic,
		spawnagent.AgentSpec,
		spawnagent.SendInputSpec,
		spawnagent.WaitAgentSpec,
		spawnagent.ResumeAgentSpec,
		spawnagent.CloseAgentSpec,
		spawnagent.InterruptAgentSpec,
		summarizethread.AgentSpec,
		askuser.AgentSpec,
		acptool.AgentSpec,
	}
}

func LlmSpecs() []llm.ToolSpec {
	return []llm.ToolSpec{
		searchtools.LlmSpec,
		TimelineTitleLlmSpec,
		websearch.LlmSpec,
		webfetch.LlmSpec,
		// spawn_agent 由 NewSpawnAgentMiddleware 按需动态注入
		summarizethread.LlmSpec,
		askuser.LlmSpec,
		acptool.LlmSpec,
	}
}

// Executors 返回所有内置工具的 Executor 实例。
// rdb 可选；非 nil 时用于跨实例通知推送。
func Executors(pool *pgxpool.Pool, rdb *redis.Client, resolver sharedconfig.Resolver) map[string]tools.Executor {
	return map[string]tools.Executor{
		TimelineTitleAgentSpec.Name:      TimelineTitleExecutor{},
		websearch.AgentSpec.Name:         websearch.NewToolExecutor(resolver),
		websearch.AgentSpecTavily.Name:   websearch.NewTavilyExecutor(resolver),
		websearch.AgentSpecSearxng.Name:  websearch.NewSearxngExecutor(resolver),
		webfetch.AgentSpec.Name:          webfetch.NewToolExecutor(resolver),
		webfetch.AgentSpecJina.Name:      webfetch.NewJinaExecutor(resolver),
		webfetch.AgentSpecFirecrawl.Name: webfetch.NewFirecrawlExecutor(resolver),
		webfetch.AgentSpecBasic.Name:     webfetch.NewBasicExecutor(resolver),
		summarizethread.AgentSpec.Name:   &summarizethread.ToolExecutor{Pool: pool, RDB: rdb},
		askuser.AgentSpec.Name:           askuser.ToolExecutor{},
		acptool.AgentSpec.Name:           acptool.ToolExecutor{ConfigResolver: resolver},
	}
}
