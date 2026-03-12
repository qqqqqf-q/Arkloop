package pipeline

import (
	"context"

	"arkloop/services/shared/database"
	"arkloop/services/shared/eventbus"
	"arkloop/services/shared/skillstore"
	sharedtoolruntime "arkloop/services/shared/toolruntime"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
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
	PromptCacheControl string // "none" | "system_prompt"
	ReasoningMode      string // "auto" | "enabled" | "disabled" | "none"
}

// RunContext 承载单次 Execute 调用的全部运行时状态，在 Pipeline 各中间件间共享。
type RunContext struct {
	// -- 初始化时写入 --
	Run          data.Run
	DB           database.DB
	Pool         *pgxpool.Pool
	DirectPool   *pgxpool.Pool // LISTEN/NOTIFY 专用直连，不走 PgBouncer；由 Execute 保证非 nil
	BroadcastRDB *redis.Client // 跨实例 SSE 广播，nil 时仅走 pg_notify
	EventBus     eventbus.EventBus // EventBus 抽象，替代直接 Redis Pub/Sub
	TraceID      string
	Emitter      events.Emitter
	Router       *routing.ProviderRouter
	Runtime      *sharedtoolruntime.RuntimeSnapshot

	// -- EngineV1.Execute 从 Run.CreatedByUserID 注入；nil 时 MemoryMiddleware 跳过写入 --
	// agent_id 约定：默认取 PersonaDefinition.ID，字符集 [a-zA-Z0-9_-]，adapter 层 sanitize
	UserID *uuid.UUID
	// 长期环境绑定，由 EngineV1.Execute 在 run 启动时解析并注入。
	ProfileRef    string
	WorkspaceRef  string
	EnabledSkills []skillstore.ResolvedSkill

	// -- AgentLoopHandler 写入：run 完成后的 assistant 最终拼接文本，供 MemoryMiddleware 写入 --
	FinalAssistantOutput string

	// -- CancelGuardMiddleware 写入 --
	CancelFunc context.CancelFunc // 释放 LISTEN 连接
	ListenDone <-chan struct{}    // LISTEN goroutine 完成信号

	// -- InputLoaderMiddleware 写入 --
	InputJSON map[string]any
	Messages  []llm.Message

	// -- AgentConfigMiddleware 写入 --
	AgentConfig     *ResolvedAgentConfig
	AgentConfigID   *uuid.UUID
	AgentConfigName string

	// -- PersonaResolutionMiddleware 写入 --
	SystemPrompt            string
	PersonaDefinition       *personas.Definition
	MaxOutputTokens         *int
	Temperature             *float64
	TopP                    *float64
	ToolTimeoutMs           *int
	ToolBudget              map[string]any
	PerToolSoftLimits       tools.PerToolSoftLimits
	PreferredCredentialName string // Persona.PreferredCredential 解析结果，供 RoutingMiddleware 使用
	ReasoningMode           string // "auto" | "enabled" | "disabled" | "none"

	// -- 初始化时写入 base 值，MCPDiscovery/ToolBuild 覆盖 --
	ToolSpecs     []llm.ToolSpec
	ToolExecutors map[string]tools.Executor
	AllowlistSet  map[string]struct{}
	ToolRegistry  *tools.Registry
	// group_name -> provider_name
	ActiveToolProviderByGroup map[string]string

	// -- RoutingMiddleware 写入 --
	Gateway       llm.Gateway
	SelectedRoute *routing.SelectedProviderRoute
	// ResolveGatewayForRouteID 按 route_id 构建目标 Gateway，用于同一 run 内切换输出模型。
	// route_id 为空时应回退当前主路由；返回 error 时由上层决定是否降级。
	ResolveGatewayForRouteID func(ctx context.Context, routeID string) (llm.Gateway, *routing.SelectedProviderRoute, error)
	// ResolveGatewayForAgentName 按 Agent 配置名称构建目标 Gateway，用于 Lua 中直接按 agent 名称切换输出模型。
	ResolveGatewayForAgentName func(ctx context.Context, agentName string) (llm.Gateway, *routing.SelectedProviderRoute, error)

	// -- ToolBuildMiddleware 写入 --
	ToolExecutor *tools.DispatchingExecutor
	FinalSpecs   []llm.ToolSpec

	// -- EngineV1.Execute 注入：平台限制 --
	ThreadMessageHistoryLimit     int
	AgentReasoningIterationsLimit int
	ToolContinuationBudgetLimit   int
	MaxParallelTasks              int
	CreditPerUSD                  int
	LlmMaxResponseBytes           int

	// -- 默认来自平台限制，PersonaResolution 可缩小 --
	ReasoningIterations    int
	ToolContinuationBudget int

	// -- EngineV1.Execute 注入 --
	ExecutorBuilder AgentExecutorBuilder

	// -- MemoryProvider，由 EngineV1.Execute 注入；nil 时 Lua binding 返回空结果 --
	MemoryProvider memory.MemoryProvider
	// -- 当前 run 内显式 memory_write 的待刷写缓冲区 --
	PendingMemoryWrites *memory.PendingWriteBuffer

	// -- LLM 重试，由 EngineV1.Execute 注入 --
	LlmRetryMaxAttempts int
	LlmRetryBaseDelayMs int

	// -- Human-in-the-loop 钩子，均为 nil 时 Executor 不触发 --
	// WaitForInput 非 nil 时，Executor 在 CheckInAt 边界调用此函数阻塞等待用户输入。
	// 返回 ("", false) 表示超时或不注入；返回 (text, true) 则将 text 作为 user message 注入。
	WaitForInput func(ctx context.Context) (string, bool)
	// CheckInAt 判断当前迭代 iter 是否为 check-in 边界，仅当 WaitForInput 非 nil 时有效。
	CheckInAt func(iter int) bool

	// -- 父子 Run 调度（由 EngineV1.Execute 注入，nil 时表示未启用）--
	// SpawnChildRun 创建子 Run 并异步等待其完成，父 Run 挂起期间不持有 DB 连接。
	// ctx 取消时立即返回 error，子 Run 继续执行直至超时。
	SpawnChildRun func(ctx context.Context, personaID string, input string) (string, error)

	// -- PersonaResolutionMiddleware 写入，TitleSummarizerMiddleware 读取 --
	TitleSummarizer *personas.TitleSummarizerConfig
}
