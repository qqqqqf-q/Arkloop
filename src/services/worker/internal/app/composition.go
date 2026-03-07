package app

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	sharedconfig "arkloop/services/shared/config"
	sharedent "arkloop/services/shared/entitlement"
	"arkloop/services/shared/objectstore"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/mcp"
	"arkloop/services/worker/internal/memory/openviking"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/queue"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/runengine"
	"arkloop/services/worker/internal/toolprovider"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin"
	browsertool "arkloop/services/worker/internal/tools/builtin/browser"
	documentwritetool "arkloop/services/worker/internal/tools/builtin/document_write"
	sandboxtool "arkloop/services/worker/internal/tools/builtin/sandbox"
	memorytool "arkloop/services/worker/internal/tools/memory"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// ComposeNativeEngine 组装原生运行引擎。
// pool 不为 nil 时优先从数据库加载路由配置，若数据库无配置则回退到环境变量。
// directPool 不为 nil 时用于 LISTEN/NOTIFY 直连（绕过 PgBouncer）。
// rdb 不为 nil 时在 run 终态时 DECR 并发计数器。
// execRegistry 为 executor 注册表，不得为 nil。
// jobQueue 可选；非 nil 时启用 SpawnChildRun。
func ComposeNativeEngine(ctx context.Context, pool *pgxpool.Pool, directPool *pgxpool.Pool, rdb *redis.Client, cfg Config, execRegistry pipeline.AgentExecutorBuilder, jobQueue queue.JobQueue) (*runengine.EngineV1, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	configRegistry := sharedconfig.DefaultRegistry()
	var configCache sharedconfig.Cache
	configCacheTTL := sharedconfig.CacheTTLFromEnv()
	if rdb != nil && configCacheTTL > 0 {
		configCache = sharedconfig.NewRedisCache(rdb)
	}
	configResolver, _ := sharedconfig.NewResolver(configRegistry, sharedconfig.NewPGXStore(pool), configCache, configCacheTTL)

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

	executors := builtin.Executors(pool, rdb, configResolver)
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

	toolProviderTTL := time.Duration(cfg.ToolProviderCacheTTLSeconds) * time.Second
	toolProviderCache := toolprovider.NewCache(toolProviderTTL)
	if directPool != nil {
		toolProviderCache.StartInvalidationListener(ctx, directPool)
	}

	listenPool := directPool
	if listenPool == nil {
		listenPool = pool
	}
	runControlHub := pipeline.NewRunControlHub()
	runControlHub.Start(ctx, listenPool)

	baseAllowlistNames := tools.ParseAllowlistNamesFromEnv()

	// 加载 platform 级 tool_provider_configs，用于 sandbox/memory 配置
	// 解析优先级: env (显式设置) -> tool_provider_configs DB
	platformProviders, err := toolprovider.LoadActivePlatformProviders(ctx, pool)
	if err != nil {
		slog.WarnContext(ctx, "tool_provider_configs: platform load failed", "err", err.Error())
	}

	// -- Memory (OpenViking) --
	ovCfg := openviking.Config{
		BaseURL:    strings.TrimSpace(os.Getenv("ARKLOOP_OPENVIKING_BASE_URL")),
		RootAPIKey: strings.TrimSpace(os.Getenv("ARKLOOP_OPENVIKING_ROOT_API_KEY")),
	}
	if ovCfg.BaseURL == "" || ovCfg.RootAPIKey == "" {
		if p := findActiveProvider(platformProviders, "memory"); p != nil {
			if ovCfg.BaseURL == "" && p.BaseURL != nil {
				ovCfg.BaseURL = strings.TrimSpace(*p.BaseURL)
			}
			if ovCfg.RootAPIKey == "" && p.APIKeyValue != nil {
				ovCfg.RootAPIKey = strings.TrimSpace(*p.APIKeyValue)
			}
		}
	}
	memoryProvider := openviking.NewProvider(ovCfg)
	if memoryProvider == nil {
		slog.InfoContext(ctx, "memory: openviking not configured, running without memory")
	} else {
		// MemoryProvider 可用时条件注册 memory tools
		memExecutor := memorytool.NewToolExecutor(memoryProvider)
		for _, spec := range memorytool.AgentSpecs() {
			if err := toolRegistry.Register(spec); err != nil {
				return nil, err
			}
			executors[spec.Name] = memExecutor
		}
		allLlmSpecs = append(allLlmSpecs, memorytool.LlmSpecs()...)
	}

	if browserBaseURL := browsertool.BaseURLFromEnv(); browserBaseURL != "" {
		browserExecutor := browsertool.NewToolExecutor(browserBaseURL)
		for _, spec := range browsertool.AgentSpecs() {
			if err := toolRegistry.Register(spec); err != nil {
				return nil, err
			}
			executors[spec.Name] = browserExecutor
		}
		allLlmSpecs = append(allLlmSpecs, browsertool.LlmSpecs()...)
		slog.InfoContext(ctx, "browser: tools registered", "base_url", browserBaseURL)
	}

	// -- Sandbox --
	sandboxBaseURL := sandboxtool.BaseURLFromEnv()
	if sandboxBaseURL == "" {
		if p := findActiveProvider(platformProviders, "sandbox"); p != nil && p.BaseURL != nil {
			sandboxBaseURL = strings.TrimRight(strings.TrimSpace(*p.BaseURL), "/")
		}
	}
	if sandboxBaseURL != "" {
		sandboxAuthToken := sandboxtool.AuthTokenFromEnv()
		var sandboxExec tools.Executor = sandboxtool.NewToolExecutor(sandboxBaseURL, sandboxAuthToken)

		// 当 DB 可用时，包装计费装饰器
		if pool != nil {
			billingCfg := resolveSandboxBillingConfig(ctx, configResolver)
			entResolver := sharedent.NewResolver(pool, rdb)
			sandboxExec = sandboxtool.NewBillingExecutor(sandboxExec, pool, entResolver, billingCfg)
		}

		for _, spec := range sandboxtool.AgentSpecs() {
			if err := toolRegistry.Register(spec); err != nil {
				return nil, err
			}
			executors[spec.Name] = sandboxExec
		}
		allLlmSpecs = append(allLlmSpecs, sandboxtool.LlmSpecs()...)
		slog.InfoContext(ctx, "sandbox: tools registered", "base_url", sandboxBaseURL)
	}

	if s3Endpoint := strings.TrimSpace(os.Getenv("ARKLOOP_S3_ENDPOINT")); s3Endpoint != "" {
		s3AccessKey := strings.TrimSpace(os.Getenv("ARKLOOP_S3_ACCESS_KEY"))
		s3SecretKey := strings.TrimSpace(os.Getenv("ARKLOOP_S3_SECRET_KEY"))
		s3Region := strings.TrimSpace(os.Getenv("ARKLOOP_S3_REGION"))
		artifactStore, err := objectstore.New(ctx, s3Endpoint, s3AccessKey, s3SecretKey, objectstore.ArtifactBucket, s3Region)
		if err != nil {
			slog.WarnContext(ctx, "document_write: s3 init failed, skipping", "err", err.Error())
		} else {
			dwExecutor := documentwritetool.NewToolExecutor(artifactStore)
			executors[documentwritetool.AgentSpec.Name] = dwExecutor
			slog.InfoContext(ctx, "document_write: tool registered")
		}
	}

	personasRoot, err := personas.BuiltinPersonasRoot()
	if err != nil {
		return nil, err
	}
	initialPersonaRegistry, err := personas.LoadRegistry(personasRoot)
	if err != nil {
		return nil, err
	}
	watchedPersonas := personas.NewWatchedRegistry(personasRoot, initialPersonaRegistry)
	watchedPersonas.Watch(ctx)

	llmRetryMaxAttempts := 3
	llmRetryBaseDelayMs := 1000
	if configResolver != nil {
		m, err := configResolver.ResolvePrefix(ctx, "llm.retry.", sharedconfig.Scope{})
		if err != nil {
			slog.WarnContext(ctx, "llm retry config load failed, using defaults", "err", err.Error())
		} else {
			if raw := strings.TrimSpace(m["llm.retry.max_attempts"]); raw != "" {
				if v, err := strconv.Atoi(raw); err == nil && v > 0 {
					llmRetryMaxAttempts = v
				}
			}
			if raw := strings.TrimSpace(m["llm.retry.base_delay_ms"]); raw != "" {
				if v, err := strconv.Atoi(raw); err == nil && v > 0 {
					llmRetryBaseDelayMs = v
				}
			}
		}
	}

	return runengine.NewEngineV1(runengine.EngineV1Deps{
		Router:                 router,
		DBPool:                 pool,
		DirectDBPool:           directPool,
		RunControlHub:          runControlHub,
		StubGateway:            stubGateway,
		EmitDebugEvents:        stubCfg.EmitDebugEvents,
		ConfigResolver:         configResolver,
		ToolRegistry:           toolRegistry,
		ToolExecutors:          executors,
		AllLlmToolSpecs:        allLlmSpecs,
		BaseToolAllowlistNames: baseAllowlistNames,
		PersonaRegistryGetter:  watchedPersonas.Get,
		MCPPool:                mcpPool,
		MCPDiscoveryCache:      discoveryCache,
		ToolProviderCache:      toolProviderCache,
		ExecutorRegistry:       execRegistry,
		JobQueue:               jobQueue,
		RunLimiterRDB:          rdb,
		LlmRetryMaxAttempts:    llmRetryMaxAttempts,
		LlmRetryBaseDelayMs:    llmRetryBaseDelayMs,
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

func resolveSandboxBillingConfig(ctx context.Context, resolver sharedconfig.Resolver) sandboxtool.BillingConfig {
	cfg := sandboxtool.BillingConfig{BaseFee: 1, RatePerSecond: 0.5}
	if resolver == nil {
		return cfg
	}
	m, err := resolver.ResolvePrefix(ctx, "sandbox.credit_", sharedconfig.Scope{})
	if err != nil {
		return cfg
	}
	if raw := strings.TrimSpace(m["sandbox.credit_base_fee"]); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v >= 0 {
			cfg.BaseFee = v
		}
	}
	if raw := strings.TrimSpace(m["sandbox.credit_rate_per_second"]); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil && v >= 0 {
			cfg.RatePerSecond = v
		}
	}
	return cfg
}

func findActiveProvider(providers []toolprovider.ActiveProviderConfig, groupName string) *toolprovider.ActiveProviderConfig {
	for i := range providers {
		if providers[i].GroupName == groupName {
			return &providers[i]
		}
	}
	return nil
}
