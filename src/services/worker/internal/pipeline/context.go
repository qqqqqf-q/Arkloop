package pipeline

import (
	"context"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/skills"
	"arkloop/services/worker/internal/tools"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ResolvedAgentConfig 保存继承链解析后的合并配置。
type ResolvedAgentConfig struct {
	SystemPrompt       *string
	Model              *string
	Temperature        *float64
	MaxOutputTokens    *int
	TopP               *float64
	ContextWindowLimit *int
	ToolPolicy         string // "allowlist" | "denylist" | "none"
	ToolAllowlist      []string
	ToolDenylist       []string
	ContentFilterLevel string
	SafetyRulesJSON    map[string]any
}

// RunContext 承载单次 Execute 调用的全部运行时状态，在 Pipeline 各中间件间共享。
type RunContext struct {
	// -- 初始化时写入 --
	Run     data.Run
	Pool    *pgxpool.Pool
	TraceID string
	Emitter events.Emitter
	Router  *routing.ProviderRouter

	// -- CancelGuardMiddleware 写入 --
	CancelFunc context.CancelFunc // 释放 LISTEN 连接
	ListenDone <-chan struct{}    // LISTEN goroutine 完成信号

	// -- InputLoaderMiddleware 写入 --
	InputJSON map[string]any
	Messages  []llm.Message

	// -- AgentConfigMiddleware 写入 --
	AgentConfig *ResolvedAgentConfig

	// -- SkillResolutionMiddleware 写入 --
	SystemPrompt    string
	SkillDefinition *skills.Definition
	MaxOutputTokens *int
	ToolTimeoutMs   *int
	ToolBudget      map[string]any

	// -- 初始化时写入 base 值，MCPDiscovery/ToolBuild 覆盖 --
	ToolSpecs     []llm.ToolSpec
	ToolExecutors map[string]tools.Executor
	AllowlistSet  map[string]struct{}
	ToolRegistry  *tools.Registry

	// -- RoutingMiddleware 写入 --
	Gateway       llm.Gateway
	SelectedRoute *routing.SelectedProviderRoute

	// -- ToolBuildMiddleware 写入 --
	ToolExecutor *tools.DispatchingExecutor
	FinalSpecs   []llm.ToolSpec

	// -- 默认 10，SkillResolution 可覆盖 --
	MaxIterations int
}
