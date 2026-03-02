package builtin

import (
	spawnagent "arkloop/services/worker/internal/tools/builtin/spawn_agent"
	summarizethread "arkloop/services/worker/internal/tools/builtin/summarize_thread"
	webfetch "arkloop/services/worker/internal/tools/builtin/web_fetch"
	websearch "arkloop/services/worker/internal/tools/builtin/web_search"

	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func AgentSpecs() []tools.AgentToolSpec {
	return []tools.AgentToolSpec{
		EchoAgentSpec,
		NoopAgentSpec,
		SearchPlanningTitleAgentSpec,
		websearch.AgentSpec,
		webfetch.AgentSpec,
		spawnagent.AgentSpec,
		summarizethread.AgentSpec,
	}
}

func LlmSpecs() []llm.ToolSpec {
	return []llm.ToolSpec{
		EchoLlmSpec,
		NoopLlmSpec,
		SearchPlanningTitleLlmSpec,
		websearch.LlmSpec,
		webfetch.LlmSpec,
		spawnagent.LlmSpec,
		summarizethread.LlmSpec,
	}
}

// Executors 返回所有内置工具的 Executor 实例。
// rdb 可选；非 nil 时用于跨实例通知推送。
func Executors(pool *pgxpool.Pool, rdb *redis.Client, resolver sharedconfig.Resolver) map[string]tools.Executor {
	return map[string]tools.Executor{
		EchoAgentSpec.Name:             EchoExecutor{},
		NoopAgentSpec.Name:             NoopExecutor{},
		SearchPlanningTitleAgentSpec.Name: SearchPlanningTitleExecutor{},
		websearch.AgentSpec.Name:       websearch.NewToolExecutor(resolver),
		webfetch.AgentSpec.Name:        webfetch.NewToolExecutor(resolver),
		summarizethread.AgentSpec.Name: &summarizethread.ToolExecutor{Pool: pool, RDB: rdb},
	}
}
