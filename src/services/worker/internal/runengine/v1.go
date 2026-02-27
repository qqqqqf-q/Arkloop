package runengine

import (
	"context"
	"fmt"
	"strings"

	sharedent "arkloop/services/shared/entitlement"
	"arkloop/services/shared/runlimit"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/mcp"
	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/queue"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/skills"
	"arkloop/services/worker/internal/tools"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type EngineV1 struct {
	middlewares         []pipeline.RunMiddleware
	terminal            pipeline.RunHandler
	router              *routing.ProviderRouter
	directPool          *pgxpool.Pool
	broadcastRDB        *redis.Client
	jobQueue            queue.JobQueue
	executorRegistry    pipeline.AgentExecutorBuilder
	memoryProvider      memory.MemoryProvider
	llmRetryMaxAttempts int
	llmRetryBaseDelayMs int
}

type ExecuteInput struct {
	TraceID string
}

type EngineV1Deps struct {
	Router          *routing.ProviderRouter
	DBPool          *pgxpool.Pool
	DirectDBPool    *pgxpool.Pool // LISTEN/NOTIFY 专用直连，不走 PgBouncer；nil 时 Execute 内回落 DBPool
	StubGateway     llm.Gateway
	EmitDebugEvents bool
	RunLimiterRDB   *redis.Client

	ToolRegistry           *tools.Registry
	ToolExecutors          map[string]tools.Executor
	AllLlmToolSpecs        []llm.ToolSpec
	BaseToolAllowlistNames []string

	SkillRegistry   *skills.Registry
	MCPPool         *mcp.Pool
	MCPDiscoveryCache *mcp.DiscoveryCache  // 缓存 DiscoverFromDB 结果，nil 时跳过 per-org MCP 发现
	ExecutorRegistry pipeline.AgentExecutorBuilder // 必填，nil 时 NewEngineV1 返回错误

	// JobQueue 可选；非 nil 时启用 SpawnChildRun（AS-3.5.2）
	JobQueue queue.JobQueue

	// LLM 请求重试配置
	LlmRetryMaxAttempts int
	LlmRetryBaseDelayMs int

	// MemoryProvider 可选；nil 时跳过整个 MemoryMiddleware（AS-5.4）
	MemoryProvider memory.MemoryProvider
}

func NewEngineV1(deps EngineV1Deps) (*EngineV1, error) {
	if deps.Router == nil {
		return nil, fmt.Errorf("router must not be nil")
	}
	if deps.StubGateway == nil {
		return nil, fmt.Errorf("stub gateway must not be nil")
	}
	if deps.ToolRegistry == nil {
		return nil, fmt.Errorf("tool registry must not be nil")
	}
	if deps.ExecutorRegistry == nil {
		return nil, fmt.Errorf("executor registry must not be nil")
	}
	if deps.ToolExecutors == nil {
		deps.ToolExecutors = map[string]tools.Executor{}
	}

	baseAllowlistSet := map[string]struct{}{}
	for _, name := range deps.BaseToolAllowlistNames {
		cleaned := strings.TrimSpace(name)
		if cleaned == "" {
			continue
		}
		baseAllowlistSet[cleaned] = struct{}{}
	}

	// 验证 base 工具集可构建
	if _, err := pipeline.BuildDispatchExecutor(deps.ToolRegistry, deps.ToolExecutors, baseAllowlistSet); err != nil {
		return nil, err
	}

	runsRepo := data.RunsRepository{}
	eventsRepo := data.RunEventsRepository{}
	messagesRepo := data.MessagesRepository{}
	usageRepo := data.UsageRecordsRepository{}
	creditsRepo := data.CreditsRepository{}

	rdb := deps.RunLimiterRDB
	releaseSlot := func(ctx context.Context, run data.Run) {
		// 子 Run 没有通过 API 层 TryAcquire，不释放并发槽
		if run.ParentRunID != nil {
			return
		}
		key := runlimit.Key(run.OrgID.String())
		runlimit.Release(ctx, rdb, key)
	}

	// deps.DBPool 为 nil 时 resolver 保持 nil，EntitlementMiddleware 以 fail-open 方式跳过检查
	var resolver *sharedent.Resolver
	if deps.DBPool != nil {
		resolver = sharedent.NewResolver(deps.DBPool, rdb)
	}

	middlewares := []pipeline.RunMiddleware{
		pipeline.NewCancelGuardMiddleware(runsRepo, eventsRepo),
		pipeline.NewInputLoaderMiddleware(eventsRepo, messagesRepo),
		pipeline.NewEntitlementMiddleware(resolver, runsRepo, eventsRepo, releaseSlot),
		pipeline.NewMCPDiscoveryMiddleware(
			deps.MCPDiscoveryCache,
			deps.ToolExecutors,
			deps.AllLlmToolSpecs,
			baseAllowlistSet,
			deps.ToolRegistry,
		),
		pipeline.NewAgentConfigMiddleware(deps.DBPool),
		pipeline.NewSkillResolutionMiddleware(deps.SkillRegistry, deps.DBPool, runsRepo, eventsRepo, releaseSlot),
		pipeline.NewMemoryMiddleware(deps.MemoryProvider),
		pipeline.NewRoutingMiddleware(deps.Router, deps.DBPool, deps.StubGateway, deps.EmitDebugEvents, runsRepo, eventsRepo, releaseSlot, resolver),
		pipeline.NewToolBuildMiddleware(),
	}

	terminal := pipeline.NewAgentLoopHandler(runsRepo, eventsRepo, messagesRepo, deps.RunLimiterRDB, usageRepo, creditsRepo, resolver)

	return &EngineV1{
		middlewares:         middlewares,
		terminal:            terminal,
		router:              deps.Router,
		directPool:          deps.DirectDBPool,
		broadcastRDB:        deps.RunLimiterRDB,
		jobQueue:            deps.JobQueue,
		executorRegistry:    deps.ExecutorRegistry,
		memoryProvider:      deps.MemoryProvider,
		llmRetryMaxAttempts: deps.LlmRetryMaxAttempts,
		llmRetryBaseDelayMs: deps.LlmRetryBaseDelayMs,
	}, nil
}

func (e *EngineV1) Execute(ctx context.Context, pool *pgxpool.Pool, run data.Run, input ExecuteInput) error {
	if pool == nil {
		return fmt.Errorf("pool must not be nil")
	}

	traceID := strings.TrimSpace(input.TraceID)

	directPool := e.directPool
	if directPool == nil {
		directPool = pool
	}
	rc := &pipeline.RunContext{
		Run:                 run,
		Pool:                pool,
		DirectPool:          directPool,
		BroadcastRDB:        e.broadcastRDB,
		TraceID:             traceID,
		Emitter:             events.NewEmitter(traceID),
		Router:              e.router,
		UserID:              run.CreatedByUserID,
		ExecutorBuilder:     e.executorRegistry,
		MaxIterations:       10,
		ToolBudget:          map[string]any{},
		LlmRetryMaxAttempts: e.llmRetryMaxAttempts,
		LlmRetryBaseDelayMs: e.llmRetryBaseDelayMs,
	}

	if e.jobQueue != nil && e.broadcastRDB != nil {
		rc.SpawnChildRun = newSpawnChildRunFunc(pool, e.broadcastRDB, e.jobQueue, run, traceID)
	}

	handler := pipeline.Build(e.middlewares, e.terminal)
	return handler(ctx, rc)
}
