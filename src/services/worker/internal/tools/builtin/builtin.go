package builtin

import (
	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/shared/objectstore"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
	arkloophelp "arkloop/services/worker/internal/tools/builtin/arkloop_help"
	artifactguidelines "arkloop/services/worker/internal/tools/builtin/artifact_guidelines"
	"arkloop/services/worker/internal/tools/builtin/askuser"
	"arkloop/services/worker/internal/tools/builtin/edit"
	enterplanmode "arkloop/services/worker/internal/tools/builtin/enter_plan_mode"
	exitplanmode "arkloop/services/worker/internal/tools/builtin/exit_plan_mode"
	"arkloop/services/worker/internal/tools/builtin/fileops"
	"arkloop/services/worker/internal/tools/builtin/glob"
	"arkloop/services/worker/internal/tools/builtin/grep"
	loadskill "arkloop/services/worker/internal/tools/builtin/load_skill"
	loadtools "arkloop/services/worker/internal/tools/builtin/load_tools"
	read "arkloop/services/worker/internal/tools/builtin/read"
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
		loadtools.AgentSpec,
		loadskill.AgentSpec,
		TimelineTitleAgentSpec,
		visualizereadme.AgentSpec,
		artifactguidelines.AgentSpec,
		arkloophelp.AgentSpec,
		websearch.AgentSpec,
		websearch.AgentSpecBasic,
		websearch.AgentSpecTavily,
		websearch.AgentSpecSearxng,
		websearch.AgentSpecExa,
		webfetch.AgentSpec,
		webfetch.AgentSpecJina,
		webfetch.AgentSpecFirecrawl,
		webfetch.AgentSpecBasic,
		read.AgentSpec,
		read.AgentSpecMiniMax,
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
		showwidget.AgentSpec,
		todowrite.AgentSpec,
		enterplanmode.AgentSpec,
		exitplanmode.AgentSpec,
	}
}

func LlmSpecs() []llm.ToolSpec {
	return []llm.ToolSpec{
		loadtools.LlmSpec,
		loadskill.LlmSpec,
		TimelineTitleLlmSpec,
		visualizereadme.LlmSpec,
		artifactguidelines.LlmSpec,
		arkloophelp.LlmSpec,
		websearch.LlmSpec,
		webfetch.LlmSpec,
		read.LlmSpec,
		writefile.LlmSpec,
		edit.LlmSpec,
		glob.LlmSpec,
		grep.LlmSpec,
		// spawn_agent 由 NewToolProviderMiddleware 按需动态注入
		summarizethread.LlmSpec,
		askuser.LlmSpec,
		showwidget.LlmSpec,
		todowrite.LlmSpec,
		enterplanmode.LlmSpec,
		exitplanmode.LlmSpec,
	}
}

// Executors 返回所有内置工具的 Executor 实例。
// rdb 可选；非 nil 时用于跨实例通知推送。
func Executors(pool *pgxpool.Pool, rdb *redis.Client, resolver sharedconfig.Resolver, skillStore objectstore.Store) (map[string]tools.Executor, *fileops.FileTracker) {
	tracker := fileops.NewFileTracker()
	return map[string]tools.Executor{
		TimelineTitleAgentSpec.Name:       TimelineTitleExecutor{},
		loadskill.AgentSpec.Name:          loadskill.NewToolExecutor(skillStore),
		visualizereadme.AgentSpec.Name:    visualizereadme.NewToolExecutor(),
		artifactguidelines.AgentSpec.Name: artifactguidelines.ToolExecutor{},
		arkloophelp.AgentSpec.Name:        arkloophelp.Executor{},
		websearch.AgentSpec.Name:          websearch.NewToolExecutor(resolver),
		websearch.AgentSpecBasic.Name:     websearch.NewToolExecutorWithProvider(websearch.NewBasicProvider()),
		websearch.AgentSpecTavily.Name:    websearch.NewTavilyExecutor(resolver),
		websearch.AgentSpecSearxng.Name:   websearch.NewSearxngExecutor(resolver),
		websearch.AgentSpecExa.Name:       websearch.NewExaExecutor(resolver),
		webfetch.AgentSpec.Name:           webfetch.NewToolExecutor(resolver),
		webfetch.AgentSpecJina.Name:       webfetch.NewJinaExecutor(resolver),
		webfetch.AgentSpecFirecrawl.Name:  webfetch.NewFirecrawlExecutor(resolver),
		webfetch.AgentSpecBasic.Name:      webfetch.NewBasicExecutor(resolver),
		read.AgentSpec.Name:               read.NewToolExecutorWithTracker(tracker),
		read.AgentSpecMiniMax.Name:        read.NewToolExecutorWithTracker(tracker),
		writefile.AgentSpec.Name:          &writefile.Executor{Tracker: tracker},
		edit.AgentSpec.Name:               &edit.Executor{Tracker: tracker},
		glob.AgentSpec.Name:               &glob.Executor{},
		grep.AgentSpec.Name:               &grep.Executor{},
		summarizethread.AgentSpec.Name:    &summarizethread.ToolExecutor{Pool: pool, RDB: rdb},
		askuser.AgentSpec.Name:            askuser.ToolExecutor{},
		showwidget.AgentSpec.Name:         showwidget.NewToolExecutor(),
		todowrite.AgentSpec.Name:          &todowrite.Executor{},
		enterplanmode.AgentSpec.Name:      enterplanmode.New(),
		exitplanmode.AgentSpec.Name:       exitplanmode.New(),
	}, tracker
}
