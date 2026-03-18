package builtin

import (
	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/acptool"
	artifactguidelines "arkloop/services/worker/internal/tools/builtin/artifact_guidelines"
	"arkloop/services/worker/internal/tools/builtin/askuser"
	"arkloop/services/worker/internal/tools/builtin/edit"
	"arkloop/services/worker/internal/tools/builtin/fileops"
	"arkloop/services/worker/internal/tools/builtin/glob"
	"arkloop/services/worker/internal/tools/builtin/grep"
	readfile "arkloop/services/worker/internal/tools/builtin/read_file"
	searchtools "arkloop/services/worker/internal/tools/builtin/search_tools"
	showwidget "arkloop/services/worker/internal/tools/builtin/show_widget"
	spawnagent "arkloop/services/worker/internal/tools/builtin/spawn_agent"
	summarizethread "arkloop/services/worker/internal/tools/builtin/summarize_thread"
	todowrite "arkloop/services/worker/internal/tools/builtin/todo_write"
	visualizereadme "arkloop/services/worker/internal/tools/builtin/visualize_read_me"
	webfetch "arkloop/services/worker/internal/tools/builtin/web_fetch"
	websearch "arkloop/services/worker/internal/tools/builtin/web_search"
	writefile "arkloop/services/worker/internal/tools/builtin/write_file"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func AgentSpecs() []tools.AgentToolSpec {
	return []tools.AgentToolSpec{
		searchtools.AgentSpec,
		TimelineTitleAgentSpec,
		visualizereadme.AgentSpec,
		artifactguidelines.AgentSpec,
		websearch.AgentSpec,
		websearch.AgentSpecTavily,
		websearch.AgentSpecSearxng,
		webfetch.AgentSpec,
		webfetch.AgentSpecJina,
		webfetch.AgentSpecFirecrawl,
		webfetch.AgentSpecBasic,
		readfile.AgentSpec,
		writefile.AgentSpec,
		edit.AgentSpec,
		glob.AgentSpec,
		grep.AgentSpec,
		spawnagent.AgentSpec,
		spawnagent.SendInputSpec,
		spawnagent.WaitAgentSpec,
		spawnagent.ResumeAgentSpec,
		spawnagent.CloseAgentSpec,
		spawnagent.InterruptAgentSpec,
		summarizethread.AgentSpec,
		askuser.AgentSpec,
		acptool.AgentSpec,
		showwidget.AgentSpec,
		todowrite.AgentSpec,
	}
}

func LlmSpecs() []llm.ToolSpec {
	return []llm.ToolSpec{
		searchtools.LlmSpec,
		TimelineTitleLlmSpec,
		visualizereadme.LlmSpec,
		artifactguidelines.LlmSpec,
		websearch.LlmSpec,
		webfetch.LlmSpec,
		readfile.LlmSpec,
		writefile.LlmSpec,
		edit.LlmSpec,
		glob.LlmSpec,
		grep.LlmSpec,
		// spawn_agent 由 NewSpawnAgentMiddleware 按需动态注入
		summarizethread.LlmSpec,
		askuser.LlmSpec,
		acptool.LlmSpec,
		showwidget.LlmSpec,
		todowrite.LlmSpec,
	}
}

// Executors 返回所有内置工具的 Executor 实例。
// rdb 可选；非 nil 时用于跨实例通知推送。
func Executors(pool *pgxpool.Pool, rdb *redis.Client, resolver sharedconfig.Resolver) map[string]tools.Executor {
	tracker := fileops.NewFileTracker()
	return map[string]tools.Executor{
		TimelineTitleAgentSpec.Name:       TimelineTitleExecutor{},
		visualizereadme.AgentSpec.Name:    visualizereadme.NewToolExecutor(),
		artifactguidelines.AgentSpec.Name: artifactguidelines.ToolExecutor{},
		websearch.AgentSpec.Name:          websearch.NewToolExecutor(resolver),
		websearch.AgentSpecTavily.Name:    websearch.NewTavilyExecutor(resolver),
		websearch.AgentSpecSearxng.Name:   websearch.NewSearxngExecutor(resolver),
		webfetch.AgentSpec.Name:           webfetch.NewToolExecutor(resolver),
		webfetch.AgentSpecJina.Name:       webfetch.NewJinaExecutor(resolver),
		webfetch.AgentSpecFirecrawl.Name:  webfetch.NewFirecrawlExecutor(resolver),
		webfetch.AgentSpecBasic.Name:      webfetch.NewBasicExecutor(resolver),
		readfile.AgentSpec.Name:           &readfile.Executor{Tracker: tracker},
		writefile.AgentSpec.Name:          &writefile.Executor{Tracker: tracker},
		edit.AgentSpec.Name:               &edit.Executor{Tracker: tracker},
		glob.AgentSpec.Name:               &glob.Executor{},
		grep.AgentSpec.Name:               &grep.Executor{},
		summarizethread.AgentSpec.Name:    &summarizethread.ToolExecutor{Pool: pool, RDB: rdb},
		askuser.AgentSpec.Name:            askuser.ToolExecutor{},
		acptool.AgentSpec.Name:            acptool.ToolExecutor{ConfigResolver: resolver},
		showwidget.AgentSpec.Name:         showwidget.NewToolExecutor(),
		todowrite.AgentSpec.Name:          &todowrite.Executor{},
	}
}
