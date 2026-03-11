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
	sharedtoolruntime "arkloop/services/shared/toolruntime"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/mcp"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/queue"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/runengine"
	workerruntime "arkloop/services/worker/internal/runtime"
	"arkloop/services/worker/internal/toolprovider"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin"
	documentwritetool "arkloop/services/worker/internal/tools/builtin/document_write"
	sandboxtool "arkloop/services/worker/internal/tools/builtin/sandbox"
	conversationtool "arkloop/services/worker/internal/tools/conversation"
	memorytool "arkloop/services/worker/internal/tools/memory"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const runtimeSnapshotTTL = 5 * time.Second

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
	routingLoader := routing.NewConfigLoader(pool, routingCfg)

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
	for _, spec := range sandboxtool.AgentSpecs() {
		if err := toolRegistry.Register(spec); err != nil {
			return nil, err
		}
	}
	if err := toolRegistry.Register(sandboxtool.BrowserSpec); err != nil {
		return nil, err
	}
	for _, spec := range memorytool.AgentSpecs() {
		if err := toolRegistry.Register(spec); err != nil {
			return nil, err
		}
	}

	executors := builtin.Executors(pool, rdb, configResolver)
	allLlmSpecs := builtin.LlmSpecs()
	allLlmSpecs = append(allLlmSpecs, sandboxtool.LlmSpecs()...)
	allLlmSpecs = append(allLlmSpecs, sandboxtool.BrowserLlmSpec)
	allLlmSpecs = append(allLlmSpecs, memorytool.LlmSpecs()...)

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

	artifactStore, err := buildDocumentArtifactStore(ctx)
	if err != nil {
		slog.WarnContext(ctx, "document_write: artifact store init failed, skipping", "err", err.Error())
	}
	messageAttachmentStore, err := buildMessageAttachmentStore(ctx)
	if err != nil {
		slog.WarnContext(ctx, "message attachments: store init failed", "err", err.Error())
	}

	runtimeManager := workerruntime.NewManager(runtimeSnapshotTTL, func(loadCtx context.Context) (sharedtoolruntime.RuntimeSnapshot, error) {
		return sharedtoolruntime.BuildRuntimeSnapshot(loadCtx, sharedtoolruntime.SnapshotInput{
			ConfigResolver:         configResolver,
			HasConversationSearch:  pool != nil,
			ArtifactStoreAvailable: artifactStore != nil,
			LoadPlatformProviders: func(innerCtx context.Context) ([]sharedtoolruntime.ProviderConfig, error) {
				providers, err := toolProviderCache.GetPlatform(innerCtx, pool)
				if err != nil {
					return nil, err
				}
				return toRuntimeProviders(providers), nil
			},
		})
	})
	if directPool != nil {
		runtimeManager.StartToolProviderInvalidationListener(ctx, directPool)
	}

	sandboxExecutorFactory := workerruntime.NewSandboxExecutorFactory(pool)
	dynamicSandboxExec := workerruntime.NewDynamicSandboxExecutor(runtimeManager, sandboxExecutorFactory)
	var sandboxExec tools.Executor = dynamicSandboxExec
	if pool != nil {
		billingCfg := resolveSandboxBillingConfig(ctx, configResolver)
		entResolver := sharedent.NewResolver(pool, rdb)
		sandboxExec = sandboxtool.NewBillingExecutor(dynamicSandboxExec, pool, entResolver, billingCfg)
	}
	for _, spec := range sandboxtool.AgentSpecs() {
		executors[spec.Name] = sandboxExec
	}
	executors[sandboxtool.BrowserSpec.Name] = dynamicSandboxExec

	memoryProviderFactory := workerruntime.NewMemoryProviderFactory()
	memoryExecutorFactory := workerruntime.NewMemoryExecutorFactory(pool, data.MemorySnapshotRepository{})
	dynamicMemoryExec := workerruntime.NewDynamicMemoryExecutor(runtimeManager, memoryProviderFactory, memoryExecutorFactory)
	for _, spec := range memorytool.AgentSpecs() {
		executors[spec.Name] = dynamicMemoryExec
	}

	if pool != nil {
		convExecutor := conversationtool.NewToolExecutor(pool, data.MessagesRepository{})
		for _, spec := range conversationtool.AgentSpecs() {
			if err := toolRegistry.Register(spec); err != nil {
				return nil, err
			}
			executors[spec.Name] = convExecutor
		}
		allLlmSpecs = append(allLlmSpecs, conversationtool.LlmSpecs()...)
	}

	if artifactStore != nil {
		if err := toolRegistry.Register(documentwritetool.AgentSpec); err != nil {
			return nil, err
		}
		dwExecutor := documentwritetool.NewToolExecutor(artifactStore)
		executors[documentwritetool.AgentSpec.Name] = dwExecutor
		allLlmSpecs = append(allLlmSpecs, documentwritetool.LlmSpec)
		slog.InfoContext(ctx, "document_write: tool registered")
	}

	var toolDescriptionOverridesRepo *data.ToolDescriptionOverridesRepository
	if pool != nil {
		toolDescriptionOverridesRepo, err = data.NewToolDescriptionOverridesRepository(pool)
		if err != nil {
			return nil, err
		}
	}

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

	baseAllowlistNames := resolveBaseToolAllowlistNames(ctx, toolRegistry)

	return runengine.NewEngineV1(runengine.EngineV1Deps{
		Router:                       router,
		DBPool:                       pool,
		DirectDBPool:                 directPool,
		RunControlHub:                runControlHub,
		StubGateway:                  stubGateway,
		EmitDebugEvents:              stubCfg.EmitDebugEvents,
		ConfigResolver:               configResolver,
		ToolRegistry:                 toolRegistry,
		ToolExecutors:                executors,
		AllLlmToolSpecs:              allLlmSpecs,
		BaseToolAllowlistNames:       baseAllowlistNames,
		PersonaRegistryGetter:        nil,
		MCPPool:                      mcpPool,
		MCPDiscoveryCache:            discoveryCache,
		ToolProviderCache:            toolProviderCache,
		ToolDescriptionOverridesRepo: toolDescriptionOverridesRepo,
		ExecutorRegistry:             execRegistry,
		JobQueue:                     jobQueue,
		RunLimiterRDB:                rdb,
		LlmRetryMaxAttempts:          llmRetryMaxAttempts,
		LlmRetryBaseDelayMs:          llmRetryBaseDelayMs,
		RuntimeManager:               runtimeManager,
		MemoryProviderFactory:        memoryProviderFactory,
		RoutingConfigLoader:          routingLoader,
		MessageAttachmentStore:       messageAttachmentStore,
	})
}

func buildDocumentArtifactStore(ctx context.Context) (objectstore.Store, error) {
	bucketOpener, err := buildStorageBucketOpenerFromEnv()
	if err != nil {
		return nil, err
	}
	if bucketOpener == nil {
		return nil, nil
	}
	return bucketOpener.Open(ctx, objectstore.ArtifactBucket)
}

func buildMessageAttachmentStore(ctx context.Context) (objectstore.Store, error) {
	bucketOpener, err := buildStorageBucketOpenerFromEnv()
	if err != nil {
		return nil, err
	}
	if bucketOpener == nil {
		return nil, nil
	}
	s3Bucket := strings.TrimSpace(os.Getenv("ARKLOOP_S3_BUCKET"))
	if s3Bucket == "" {
		return nil, nil
	}
	return bucketOpener.Open(ctx, s3Bucket)
}

func buildStorageBucketOpenerFromEnv() (objectstore.BucketOpener, error) {
	runtimeConfig, err := objectstore.LoadRuntimeConfigFromEnv()
	if err != nil {
		return nil, err
	}
	if !runtimeConfig.Enabled() {
		return nil, nil
	}
	return runtimeConfig.BucketOpener()
}

func toRuntimeProviders(platformProviders []toolprovider.ActiveProviderConfig) []sharedtoolruntime.ProviderConfig {
	providers := make([]sharedtoolruntime.ProviderConfig, 0, len(platformProviders))
	for _, provider := range platformProviders {
		providers = append(providers, sharedtoolruntime.ProviderConfig{
			GroupName:    provider.GroupName,
			ProviderName: provider.ProviderName,
			BaseURL:      provider.BaseURL,
			APIKeyValue:  provider.APIKeyValue,
		})
	}
	return providers
}

func resolveBaseToolAllowlistNames(ctx context.Context, toolRegistry *tools.Registry) []string {
	if deprecated := tools.ParseAllowlistNamesFromEnv(); len(deprecated) > 0 {
		slog.WarnContext(ctx, "tool allowlist env is deprecated and no longer gates runtime tools", "env", "ARKLOOP_TOOL_ALLOWLIST", "tools", deprecated)
	}
	if toolRegistry == nil {
		return nil
	}
	return toolRegistry.ListNames()
}

// loadRoutingConfig 优先从 DB 加载路由配置，无数据时回退到环境变量。
func loadRoutingConfig(ctx context.Context, pool *pgxpool.Pool) (routing.ProviderRoutingConfig, error) {
	if pool != nil {
		dbCfg, err := routing.LoadRoutingConfigFromDB(ctx, pool, uuid.Nil)
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
