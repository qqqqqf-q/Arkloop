package pipeline

import (
	"context"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/skills"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// ResolvedAgentConfig 保存继承链解析后的合并配置。
type ResolvedAgentConfig struct {
	SystemPrompt        *string
	Model               *string
	Temperature         *float64
	MaxOutputTokens     *int
	TopP                *float64
	ContextWindowLimit  *int
	ToolPolicy          string // "allowlist" | "denylist" | "none"
	ToolAllowlist       []string
	ToolDenylist        []string
	ContentFilterLevel  string
	SafetyRulesJSON     map[string]any
	PromptCacheControl  string // "none" | "system_prompt"
}

// RunContext 承载单次 Execute 调用的全部运行时状态，在 Pipeline 各中间件间共享。
type RunContext struct {
	// -- 初始化时写入 --
	Run          data.Run
	Pool         *pgxpool.Pool
	DirectPool   *pgxpool.Pool  // LISTEN/NOTIFY 专用直连，不走 PgBouncer；由 Execute 保证非 nil
	BroadcastRDB *redis.Client  // 跨实例 SSE 广播，nil 时仅走 pg_notify
	TraceID      string
	Emitter events.Emitter
	Router  *routing.ProviderRouter

	// -- EngineV1.Execute 从 Run.CreatedByUserID 注入；nil 时 MemoryMiddleware 跳过写入 --
	// agent_id 约定：默认取 SkillDefinition.ID；OpenViking 要求字符集 [a-zA-Z0-9_-]，adapter 层 sanitize
	UserID *uuid.UUID

	// -- AgentLoopHandler 写入：run 完成后的 assistant 最终拼接文本，供 MemoryMiddleware 写入 OpenViking --
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

	// -- SkillResolutionMiddleware 写入 --
	SystemPrompt           string
	SkillDefinition        *skills.Definition
	MaxOutputTokens        *int
	Temperature            *float64
	TopP                   *float64
	ToolTimeoutMs          *int
	ToolBudget             map[string]any
	PreferredCredentialName string // Skill.PreferredCredential 解析结果，供 RoutingMiddleware 使用

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

	// -- EngineV1.Execute 注入 --
	ExecutorBuilder     AgentExecutorBuilder

	// -- MemoryProvider，由 EngineV1.Execute 注入；nil 时 Lua binding 返回空结果 --
	MemoryProvider memory.MemoryProvider

	// -- LLM 重试，由 EngineV1.Execute 注入 --
	LlmRetryMaxAttempts int
	LlmRetryBaseDelayMs int

	// -- Human-in-the-loop 钩子（AS-3），均为 nil 时 Executor 不触发，零开销 --
	// WaitForInput 非 nil 时，Executor 在 CheckInAt 边界调用此函数阻塞等待用户输入。
	// 返回 ("", false) 表示超时或不注入；返回 (text, true) 则将 text 作为 user message 注入。
	WaitForInput func(ctx context.Context) (string, bool)
	// CheckInAt 判断当前迭代 iter 是否为 check-in 边界，仅当 WaitForInput 非 nil 时有效。
	CheckInAt func(iter int) bool

	// -- AS-3.5.2 父子 Run 调度（由 EngineV1.Execute 注入，nil 时表示未启用）--
	// SpawnChildRun 创建子 Run 并异步等待其完成，父 Run 挂起期间不持有 DB 连接。
	// ctx 取消时立即返回 error，子 Run 继续执行直至超时。
	SpawnChildRun func(ctx context.Context, skillID string, input string) (string, error)
}
