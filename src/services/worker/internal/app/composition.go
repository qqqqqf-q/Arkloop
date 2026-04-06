//go:build !desktop

package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	sharedconfig "arkloop/services/shared/config"
	sharedent "arkloop/services/shared/entitlement"
	"arkloop/services/shared/objectstore"
	sharedtoolruntime "arkloop/services/shared/toolruntime"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/mcp"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/queue"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/runengine"
	workerruntime "arkloop/services/worker/internal/runtime"
	"arkloop/services/worker/internal/toolprovider"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin"
	"arkloop/services/worker/internal/tools/builtin/channel_telegram"
	"arkloop/services/worker/internal/tools/builtin/platform"
	sandboxtool "arkloop/services/worker/internal/tools/builtin/sandbox"
	"arkloop/services/worker/internal/tools/builtin/read"
	conversationtool "arkloop/services/worker/internal/tools/conversation"
	notebookprovider "arkloop/services/worker/internal/memory/notebook"
	memorytool "arkloop/services/worker/internal/tools/memory"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type pgTelegramChannelTokenLoader struct {
	pool *pgxpool.Pool
	repo data.ChannelDeliveryRepository
}

func (l *pgTelegramChannelTokenLoader) BotToken(ctx context.Context, channelID uuid.UUID) (string, error) {
	if l.pool == nil {
		return "", fmt.Errorf("telegram channel tools: db unavailable")
	}
	ch, err := l.repo.GetChannel(ctx, l.pool, channelID)
	if err != nil {
		return "", err
	}
	if ch == nil {
		return "", fmt.Errorf("telegram channel tools: channel not found")
	}
	return strings.TrimSpace(ch.Token), nil
}

const runtimeSnapshotTTL = 5 * time.Second

// ComposeNativeEngine 组装原生运行引擎。
// pool 不为 nil 时优先从数据库加载路由配置，若数据库无配置则回退到环境变量。
// directPool 不为 nil 时用于 LISTEN/NOTIFY 直连（绕过 PgBouncer）。
// rdb 不为 nil 时在 run 终态时 DECR 并发计数器。
// execRegistry 为 executor 注册表，不得为 nil。
// jobQueue 可选；非 nil 时启用 SubAgentControl。
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

	auxCfg, err := llm.AuxGatewayConfigFromEnv()
	if err != nil {
		return nil, err
	}
	auxGateway := llm.NewAuxGateway(auxCfg)

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
	for _, spec := range memorytool.MemoryAgentSpecs() {
		if err := toolRegistry.Register(spec); err != nil {
			return nil, err
		}
	}

	// 提前解析存储 bucket opener，出错说明存储配置有误（不是"未配置"），直接 fail fast。
	// opener 为 nil 表示存储未启用，所有 optional store 均为 nil，下游消费方均有 nil guard。
	storageBucketOpener, err := buildStorageBucketOpenerFromEnv()
	if err != nil {
		return nil, fmt.Errorf("storage backend config: %w", err)
	}

	skillStore, err := openBucket(ctx, storageBucketOpener, objectstore.SkillStoreBucket)
	if err != nil {
		return nil, fmt.Errorf("open skill store: %w", err)
	}
	executors := builtin.Executors(pool, rdb, configResolver, skillStore)
	allLlmSpecs := builtin.LlmSpecs()

	// platform_manage executor (通过 PlatformToolsMiddleware 按需注入，不全局注册)
	var platformExec tools.Executor
	if tp := initPlatformTokenProvider(ctx, pool); tp != nil {
		apiURL := strings.TrimSpace(os.Getenv("ARKLOOP_PLATFORM_API_URL"))
		if apiURL == "" {
			apiURL = "http://127.0.0.1:19001"
		}
		bridgeURL := strings.TrimSpace(os.Getenv("ARKLOOP_BRIDGE_URL"))
		platformExec = platform.NewExecutor(apiURL, bridgeURL, tp)
		slog.InfoContext(ctx, "platform_manage tool registered", "api_url", apiURL)
	}
	allLlmSpecs = append(allLlmSpecs, sandboxtool.LlmSpecs()...)
	allLlmSpecs = append(allLlmSpecs, sandboxtool.BrowserLlmSpec)
	allLlmSpecs = append(allLlmSpecs, memorytool.MemoryLlmSpecs()...)

	// 全局 MCP pool，用于 env-loaded 工具及 per-run account 工具的连接复用
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

	artifactStore, err := openBucket(ctx, storageBucketOpener, objectstore.ArtifactBucket)
	if err != nil {
		return nil, fmt.Errorf("open artifact store: %w", err)
	}

	var messageAttachmentStore objectstore.Store
	if s3Bucket := strings.TrimSpace(os.Getenv("ARKLOOP_S3_BUCKET")); s3Bucket != "" && storageBucketOpener != nil {
		messageAttachmentStore, err = storageBucketOpener.Open(ctx, s3Bucket)
		if err != nil {
			return nil, fmt.Errorf("open message attachment store: %w", err)
		}
	}

	// 给 read executor 注入 attachment store 以支持 fallback 加载被压缩的图片
	if messageAttachmentStore != nil {
		for _, name := range []string{read.AgentSpec.Name, read.AgentSpecMiniMax.Name} {
			if exec, ok := executors[name]; ok {
				if readExec, ok := exec.(*read.Executor); ok {
					readExec.AttachmentStore = messageAttachmentStore
				}
			}
		}
	}

	var rolloutStore objectstore.BlobStore
	if storageBucketOpener != nil {
		store, openErr := storageBucketOpener.Open(ctx, objectstore.RolloutBucket)
		if openErr != nil {
			return nil, fmt.Errorf("open rollout store: %w", openErr)
		}
		blobStore, ok := store.(objectstore.BlobStore)
		if !ok {
			return nil, fmt.Errorf("rollout store does not implement blob store")
		}
		rolloutStore = blobStore
	}

	runtimeManager := workerruntime.NewManager(runtimeSnapshotTTL, func(loadCtx context.Context) (sharedtoolruntime.RuntimeSnapshot, error) {
		return sharedtoolruntime.BuildRuntimeSnapshot(loadCtx, sharedtoolruntime.SnapshotInput{
			ConfigResolver:          configResolver,
			HasConversationSearch:   pool != nil,
			HasGroupHistorySearch:   pool != nil,
			ArtifactStoreAvailable:  artifactStore != nil,
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
	for _, spec := range memorytool.MemoryAgentSpecs() {
		executors[spec.Name] = dynamicMemoryExec
	}

	// notebook: PG-backed stable notes, independent of OpenViking
	if pool != nil {
		nbProvider := notebookprovider.NewProvider(pool)
		nbExec := memorytool.NewToolExecutor(nbProvider, pool, data.MemorySnapshotRepository{})
		for _, spec := range memorytool.NotebookAgentSpecs() {
			if err := toolRegistry.Register(spec); err != nil {
				return nil, err
			}
			executors[spec.Name] = nbExec
		}
		allLlmSpecs = append(allLlmSpecs, memorytool.NotebookLlmSpecs()...)
	}

	var groupSearchExec *conversationtool.GroupSearchExecutor
	if pool != nil {
		convExecutor := conversationtool.NewToolExecutor(pool, data.MessagesRepository{})
		for _, spec := range conversationtool.AgentSpecs() {
			if err := toolRegistry.Register(spec); err != nil {
				return nil, err
			}
			executors[spec.Name] = convExecutor
		}
		allLlmSpecs = append(allLlmSpecs, conversationtool.LlmSpecs()...)

		groupSearchExec = conversationtool.NewGroupSearchExecutor(pool, nil)
		if err := toolRegistry.Register(conversationtool.GroupSearchAgentSpec); err != nil {
			return nil, err
		}
		executors[conversationtool.GroupSearchAgentSpec.Name] = groupSearchExec
		allLlmSpecs = append(allLlmSpecs, conversationtool.GroupSearchLlmSpec)
	}

	allLlmSpecs, artifactToolsRegistered, err := registerStoredArtifactTools(toolRegistry, executors, allLlmSpecs, artifactStore)
	if err != nil {
		return nil, err
	}
	if artifactToolsRegistered {
		slog.InfoContext(ctx, "stored artifact tools registered", "tools", []string{"create_artifact", "document_write"})
	}

	var toolDescriptionOverridesRepo *data.ToolDescriptionOverridesRepository
	if pool != nil {
		toolDescriptionOverridesRepo, err = data.NewToolDescriptionOverridesRepository(pool)
		if err != nil {
			return nil, err
		}
	}

	llmRetryMaxAttempts := 10
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

	// 加载文件系统 persona registry，用于与 DB persona 合并
	var personaRegistryGetter func() *personas.Registry
	personasRoot, err := personas.BuiltinPersonasRoot()
	if err == nil {
		if reg, loadErr := personas.LoadRegistry(personasRoot); loadErr == nil && len(reg.ListIDs()) > 0 {
			var (
				cached   = reg
				cachedAt = time.Now()
				mu       sync.Mutex
			)
			personaRegistryGetter = func() *personas.Registry {
				mu.Lock()
				defer mu.Unlock()
				if time.Since(cachedAt) < 30*time.Second {
					return cached
				}
				fresh, err := personas.LoadRegistry(personasRoot)
				if err != nil {
					slog.Warn("persona reload failed, using cache", "dir", personasRoot, "err", err.Error())
					return cached
				}
				cached = fresh
				cachedAt = time.Now()
				return cached
			}
		}
	}

	var chTelegram channel_telegram.TokenLoader
	if pool != nil {
		chTelegram = &pgTelegramChannelTokenLoader{pool: pool}
	}

	return runengine.NewEngineV1(runengine.EngineV1Deps{
		Router:                       router,
		DBPool:                       pool,
		DirectDBPool:                 directPool,
		RunControlHub:                runControlHub,
		AuxGateway:                   auxGateway,
		EmitDebugEvents:              auxCfg.EmitDebugEvents,
		ConfigResolver:               configResolver,
		ToolRegistry:                 toolRegistry,
		ToolExecutors:                executors,
		AllLlmToolSpecs:              allLlmSpecs,
		BaseToolAllowlistNames:       baseAllowlistNames,
		PersonaRegistryGetter:        personaRegistryGetter,
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
		RolloutBlobStore:             rolloutStore,
		PlatformToolExecutor:         platformExec,
		ChannelTelegramLoader:        chTelegram,
		GroupSearchExecutor:          groupSearchExec,
	})
}

// openBucket 在 opener 为 nil（存储未启用）时返回 (nil, nil)，其他情况透传 Open 的结果。
func openBucket(ctx context.Context, opener objectstore.BucketOpener, bucket string) (objectstore.Store, error) {
	if opener == nil {
		return nil, nil
	}
	return opener.Open(ctx, bucket)
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
			ConfigJSON:   provider.ConfigJSON,
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
		dbCfg, err := routing.LoadRoutingConfigFromDB(ctx, pool, nil)
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

// initPlatformTokenProvider 从环境变量读取 JWT secret，从 DB 查询 system_agent user ID，
// 构造用于 platform tool executor 的 TokenProvider。任何前置条件不满足时返回 nil（跳过注册）。
func initPlatformTokenProvider(ctx context.Context, pool *pgxpool.Pool) *platform.TokenProvider {
	secret := strings.TrimSpace(os.Getenv("ARKLOOP_AUTH_JWT_SECRET"))
	if len(secret) < 32 {
		slog.WarnContext(ctx, "platform tools: ARKLOOP_AUTH_JWT_SECRET not set or too short, skipping")
		return nil
	}
	if pool == nil {
		slog.WarnContext(ctx, "platform tools: no database pool, skipping")
		return nil
	}
	var userID, accountID, role string
	err := pool.QueryRow(ctx,
		`SELECT u.id, am.account_id, am.role
		   FROM users u
		   JOIN account_memberships am ON am.user_id = u.id
		  WHERE u.username = 'system_agent' AND u.deleted_at IS NULL
		  ORDER BY CASE am.role WHEN 'platform_admin' THEN 0 ELSE 1 END
		  LIMIT 1`,
	).Scan(&userID, &accountID, &role)
	if err != nil {
		slog.WarnContext(ctx, "platform tools: system_agent user/account not found, skipping", "err", err.Error())
		return nil
	}
	return platform.NewTokenProvider([]byte(secret), userID, accountID, role, 15*time.Minute)
}
