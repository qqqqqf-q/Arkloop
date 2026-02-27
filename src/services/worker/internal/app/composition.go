package app

import (
	"context"
	"log/slog"
	"time"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/mcp"
	"arkloop/services/worker/internal/memory/openviking"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/queue"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/runengine"
	"arkloop/services/worker/internal/skills"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// ComposeNativeEngine 组装原生运行引擎。
// pool 不为 nil 时优先从数据库加载路由配置，若数据库无配置则回退到环境变量。
// directPool 不为 nil 时用于 LISTEN/NOTIFY 直连（绕过 PgBouncer）。
// rdb 不为 nil 时在 run 终态时 DECR 并发计数器。
// execRegistry 为 executor 注册表，不得为 nil。
// jobQueue 可选；非 nil 时启用 SpawnChildRun（AS-3.5.2）。
func ComposeNativeEngine(ctx context.Context, pool *pgxpool.Pool, directPool *pgxpool.Pool, rdb *redis.Client, cfg Config, execRegistry pipeline.AgentExecutorBuilder, jobQueue queue.JobQueue) (*runengine.EngineV1, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	routingCfg, err := loadRoutingConfig(ctx, pool)
	if err != nil {
		return nil, err
	}
	router := routing.NewProviderRouter(routingCfg)

	stubCfg, err := llm.StubGatewayConfigFromEnv()
	if err != nil {
		return nil, err
	}
	stubGateway := llm.NewStubGateway(stubCfg)

	toolRegistry := tools.NewRegistry()
	for _, spec := range builtin.AgentSpecs() {
		if err := toolRegistry.Register(spec); err != nil {
			return nil, err
		}
	}

	executors := builtin.Executors(pool)
	allLlmSpecs := builtin.LlmSpecs()

	// 全局 MCP pool，用于 env-loaded 工具及 per-run org 工具的连接复用
	mcpPool := mcp.NewPool()
	mcpRegistration, err := mcp.DiscoverFromEnv(ctx, mcpPool)
	if err != nil {
		return nil, err
	}
	for _, spec := range mcpRegistration.AgentSpecs {
		if err := toolRegistry.Register(spec); err != nil {
			return nil, err
		}
	}
	for name, executor := range mcpRegistration.Executors {
		executors[name] = executor
	}
	allLlmSpecs = append(allLlmSpecs, mcpRegistration.LlmSpecs...)

	cacheTTL := time.Duration(cfg.MCPCacheTTLSeconds) * time.Second
	discoveryCache := mcp.NewDiscoveryCache(cacheTTL, mcpPool)
	if directPool != nil {
		discoveryCache.StartInvalidationListener(ctx, directPool)
	}

	baseAllowlistNames := tools.ParseAllowlistNamesFromEnv()

	// MemoryProvider：DB 优先，ENV 兜底；未配置时传 nil，MemoryMiddleware 自动降级为 no-op
	ovCfg, found, err := openviking.LoadConfigFromDB(ctx, pool)
	if err != nil {
		slog.WarnContext(ctx, "memory: db config load failed, falling back to env", "err", err.Error())
	}
	if !found {
		ovCfg = openviking.LoadConfigFromEnv()
	}
	memoryProvider := openviking.NewProvider(ovCfg)
	if memoryProvider == nil {
		slog.InfoContext(ctx, "memory: openviking not configured, running without memory")
	}

	skillsRoot, err := skills.BuiltinSkillsRoot()
	if err != nil {
		return nil, err
	}
	skillRegistry, err := skills.LoadRegistry(skillsRoot)
	if err != nil {
		return nil, err
	}

	return runengine.NewEngineV1(runengine.EngineV1Deps{
		Router:                 router,
		DBPool:                 pool,
		DirectDBPool:           directPool,
		StubGateway:            stubGateway,
		EmitDebugEvents:        stubCfg.EmitDebugEvents,
		ToolRegistry:           toolRegistry,
		ToolExecutors:          executors,
		AllLlmToolSpecs:        allLlmSpecs,
		BaseToolAllowlistNames: baseAllowlistNames,
		SkillRegistry:          skillRegistry,
		MCPPool:                mcpPool,
		MCPDiscoveryCache:      discoveryCache,
		ExecutorRegistry:       execRegistry,
		JobQueue:               jobQueue,
		RunLimiterRDB:          rdb,
		LlmRetryMaxAttempts:    cfg.LlmRetryMaxAttempts,
		LlmRetryBaseDelayMs:    cfg.LlmRetryBaseDelayMs,
		MemoryProvider:         memoryProvider,
	})
}

// loadRoutingConfig 优先从 DB 加载路由配置，无数据时回退到环境变量。
func loadRoutingConfig(ctx context.Context, pool *pgxpool.Pool) (routing.ProviderRoutingConfig, error) {
	if pool != nil {
		dbCfg, err := routing.LoadRoutingConfigFromDB(ctx, pool)
		if err != nil {
			slog.WarnContext(ctx, "routing: db load failed, falling back to env", "err", err.Error())
		} else if len(dbCfg.Routes) > 0 {
			return dbCfg, nil
		}
	}
	return routing.LoadRoutingConfigFromEnv()
}
