package pipeline

import (
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/skills"
	"arkloop/services/worker/internal/tools"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RunContext 承载单次 Execute 调用的全部运行时状态，在 Pipeline 各中间件间共享。
type RunContext struct {
	// -- 初始化时写入 --
	Run     data.Run
	Pool    *pgxpool.Pool
	TraceID string
	Emitter events.Emitter
	Router  *routing.ProviderRouter

	// -- InputLoaderMiddleware 写入 --
	InputJSON map[string]any
	Messages  []llm.Message

	// -- SkillResolutionMiddleware 写入 --
	SystemPrompt    string
	SkillDefinition *skills.Definition
	MaxOutputTokens *int
	ToolTimeoutMs   *int
	ToolBudget      map[string]any

	// -- 初始化时写入 base 值，MCPDiscovery/ToolBuild 覆盖 --
	ToolSpecs      []llm.ToolSpec
	ToolExecutors  map[string]tools.Executor
	AllowlistSet   map[string]struct{}

	// -- RoutingMiddleware 写入 --
	Gateway llm.Gateway

	// -- 默认 10，SkillResolution 可覆盖 --
	MaxIterations int
}
