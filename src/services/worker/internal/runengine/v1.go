//go:build !desktop

package runengine

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"

	sharedconfig "arkloop/services/shared/config"
	sharedent "arkloop/services/shared/entitlement"
	"arkloop/services/shared/objectstore"
	"arkloop/services/shared/plugin"
	"arkloop/services/shared/runlimit"
	"arkloop/services/shared/skillstore"
	sharedtoolruntime "arkloop/services/shared/toolruntime"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/mcp"
	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/queue"
	"arkloop/services/worker/internal/routing"
	workerruntime "arkloop/services/worker/internal/runtime"
	"arkloop/services/worker/internal/security"
	"arkloop/services/worker/internal/subagentctl"
	"arkloop/services/worker/internal/toolprovider"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/channel_telegram"
	"arkloop/services/worker/internal/tools/builtin/sandbox"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const defaultPersistKeepLastMessagesWorker = 40

type EngineV1 struct {
	middlewares           []pipeline.RunMiddleware
	terminal              pipeline.RunHandler
	router                *routing.ProviderRouter
	directPool            *pgxpool.Pool
	broadcastRDB          *redis.Client
	jobQueue              queue.JobQueue
	executorRegistry      pipeline.AgentExecutorBuilder
	runtimeManager        *workerruntime.Manager
	memoryProviderFactory *workerruntime.MemoryProviderFactory
	llmRetryMaxAttempts   int
	llmRetryBaseDelayMs   int
	configResolver        sharedconfig.Resolver
	releaseSlot           func(ctx context.Context, run data.Run)
	rolloutBlobStore     objectstore.BlobStore
}

type ExecuteInput struct {
	TraceID    string
	JobPayload map[string]any
}

type EngineV1Deps struct {
	Router          *routing.ProviderRouter
	DBPool          *pgxpool.Pool
	DirectDBPool    *pgxpool.Pool // LISTEN/NOTIFY 专用直连，不走 PgBouncer；nil 时 Execute 内回落 DBPool
	RunControlHub   *pipeline.RunControlHub
	AuxGateway     llm.Gateway
	EmitDebugEvents bool
	RunLimiterRDB   *redis.Client

	ConfigResolver sharedconfig.Resolver

	ToolRegistry           *tools.Registry
	ToolExecutors          map[string]tools.Executor
	AllLlmToolSpecs        []llm.ToolSpec
	BaseToolAllowlistNames []string

	PersonaRegistryGetter        func() *personas.Registry
	MCPPool                      *mcp.Pool
	MCPDiscoveryCache            *mcp.DiscoveryCache // 缓存 DiscoverFromDB 结果，nil 时跳过 per-account MCP 发现
	ToolProviderCache            *toolprovider.Cache
	ToolDescriptionOverridesRepo pipeline.ToolDescriptionOverridesReader
	ExecutorRegistry             pipeline.AgentExecutorBuilder // 必填，nil 时 NewEngineV1 返回错误

	// JobQueue 可选；非 nil 时启用 SubAgentControl
	JobQueue queue.JobQueue

	// LLM 请求重试配置
	LlmRetryMaxAttempts int
	LlmRetryBaseDelayMs int

	RuntimeManager         *workerruntime.Manager
	MemoryProviderFactory  *workerruntime.MemoryProviderFactory
	RoutingConfigLoader    *routing.ConfigLoader
	MessageAttachmentStore pipeline.MessageAttachmentStore
	RolloutBlobStore      objectstore.BlobStore // 用于创建 RolloutRecorder，非 desktop 模式下可选

	// PlatformToolExecutor: platform_manage 的执行器，nil 时跳过注入
	PlatformToolExecutor tools.Executor

	// ChannelTelegramLoader: Telegram Channel 工具取 token；nil 时不注入 telegram_react/reply
	ChannelTelegramLoader channel_telegram.TokenLoader
}

func NewEngineV1(deps EngineV1Deps) (*EngineV1, error) {
	if deps.Router == nil {
		return nil, fmt.Errorf("router must not be nil")
	}
	if deps.AuxGateway == nil {
		return nil, fmt.Errorf("aux gateway must not be nil")
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
	resolvedBaseAllowlist, err := pipeline.ResolveProviderAllowlist(baseAllowlistSet, deps.ToolRegistry, nil)
	if err != nil {
		return nil, err
	}

	filteredBaseAllowlist, dropped := pipeline.FilterAllowlistToBoundExecutors(resolvedBaseAllowlist, deps.ToolExecutors)
	if len(dropped) > 0 {
		slog.Warn("base tool allowlist dropped unbound executors", "tools", dropped)
	}
	baseAllowlistSet = filteredBaseAllowlist

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
		key := runlimit.Key(run.AccountID.String())
		runlimit.Release(ctx, rdb, key)
	}

	// deps.DBPool 为 nil 时 resolver 保持 nil，EntitlementMiddleware 以 fail-open 方式跳过检查
	var resolver *sharedent.Resolver
	if deps.DBPool != nil {
		resolver = sharedent.NewResolver(deps.DBPool, rdb)
	}

	cfgResolver := deps.ConfigResolver
	if cfgResolver == nil {
		registry := sharedconfig.DefaultRegistry()
		fallback, _ := sharedconfig.NewResolver(registry, sharedconfig.NewPGXStore(deps.DBPool), nil, 0)
		cfgResolver = fallback
	}

	var injectionScanner *security.RegexScanner
	if scanner, err := security.NewRegexScanner(security.DefaultPatterns()); err == nil {
		injectionScanner = scanner
	} else {
		slog.Error("failed to initialize injection scanner", "error", err)
	}

	semanticScanner := security.NewRuntimeSemanticScanner(
		cfgResolver,
		os.Getenv("ARKLOOP_PROMPT_GUARD_MODEL_DIR"),
		os.Getenv("ARKLOOP_ONNX_RUNTIME_LIB"),
	)

	compositeScanner := security.NewCompositeScanner(injectionScanner, semanticScanner)

	var injectionAuditor *security.SecurityAuditor
	if dbSink, err := plugin.NewDBSink(deps.DBPool); err == nil {
		injectionAuditor = security.NewSecurityAuditor(dbSink)
	} else {
		slog.Error("failed to initialize security auditor", "error", err)
	}

	// 中间件执行顺序有隐含的前置条件依赖，不可随意调整：
	//   CancelGuard     — 必须最先：建立取消监听和 WaitForInput，后续中间件依赖
	//   InputLoader     — 在 Entitlement 前：Entitlement 需要 Messages 判断空输入
	//   Entitlement     — 在 Routing 前：配额检查不依赖模型路由
	//   MCPDiscovery    — 在 ToolBuild 前：发现的 MCP tools 需进入 allowlist
	//   ToolProvider    — 在 PersonaResolution 前：provider override 需先于 persona 合并
	//   PersonaResolution — 在 Memory/Routing 前：SystemPrompt、AgentConfig 由此确定
	//   ChannelContext  — 在 HeartbeatSchedule 前：后者依赖 ChannelContext.ChannelID
	//   Memory          — 在 Routing 前：可能修改 SystemPrompt，Routing 依赖最终 prompt
	//   InjectionScan   — 在 Routing 前：扫描结果影响路由决策（trust source）
	//   Routing         — 在 ContextCompact/TitleSummarizer 前：后两者依赖 Gateway
	//   ToolBuild       — 必须最后：依赖前面所有 mw 对 ToolRegistry/Specs 的修改
	//   ChannelDelivery — 必须最后：包裹 handler，在 run 结束后执行投递
	middlewares := buildPipeline(deps, runsRepo, eventsRepo, messagesRepo, resolver, cfgResolver, releaseSlot, compositeScanner, injectionAuditor, baseAllowlistSet)

	terminal := pipeline.NewAgentLoopHandler(runsRepo, eventsRepo, messagesRepo, deps.RunLimiterRDB, deps.JobQueue, usageRepo, creditsRepo, resolver)

	return &EngineV1{
		middlewares:           middlewares,
		terminal:              terminal,
		router:                deps.Router,
		directPool:            deps.DirectDBPool,
		broadcastRDB:          deps.RunLimiterRDB,
		jobQueue:              deps.JobQueue,
		executorRegistry:      deps.ExecutorRegistry,
		runtimeManager:        deps.RuntimeManager,
		memoryProviderFactory: deps.MemoryProviderFactory,
		llmRetryMaxAttempts:   deps.LlmRetryMaxAttempts,
		llmRetryBaseDelayMs:   deps.LlmRetryBaseDelayMs,
		configResolver:        cfgResolver,
		releaseSlot:           releaseSlot,
		rolloutBlobStore:     deps.RolloutBlobStore,
	}, nil
}

func (e *EngineV1) Execute(ctx context.Context, pool *pgxpool.Pool, run data.Run, input ExecuteInput) error {
	if pool == nil {
		return fmt.Errorf("pool must not be nil")
	}

	resolvedRun, err := resolveAndPersistEnvironmentBindings(ctx, pool, run)
	if err != nil {
		return fmt.Errorf("resolve environment bindings: %w", err)
	}
	run = resolvedRun
	if err := subagentctl.MarkRunning(ctx, pool, run.ID); err != nil {
		return fmt.Errorf("mark sub_agent running: %w", err)
	}

	traceID := strings.TrimSpace(input.TraceID)

	runtimeSnapshot := sharedtoolruntime.RuntimeSnapshot{}
	if e.runtimeManager != nil {
		snapshot, snapshotErr := e.runtimeManager.Current(ctx)
		if snapshotErr != nil {
			slog.WarnContext(ctx, "runtime snapshot load failed, using empty snapshot", "err", snapshotErr.Error())
		} else {
			runtimeSnapshot = snapshot
		}
	}

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
		Runtime:             &runtimeSnapshot,
		UserID:              run.CreatedByUserID,
		JobPayload:          cloneMap(input.JobPayload),
		ProfileRef:          derefString(run.ProfileRef),
		WorkspaceRef:        derefString(run.WorkspaceRef),
		ExecutorBuilder:     e.executorRegistry,
		MemoryProvider:      nil,
		PendingMemoryWrites: memory.NewPendingWriteBuffer(),
		ToolBudget:          map[string]any{},
		PerToolSoftLimits:   tools.DefaultPerToolSoftLimits(),
		LlmRetryMaxAttempts: e.llmRetryMaxAttempts,
		LlmRetryBaseDelayMs: e.llmRetryBaseDelayMs,
	}

	registry := sharedconfig.DefaultRegistry()
	platformScope := sharedconfig.Scope{}
	rc.ThreadMessageHistoryLimit = resolvePositiveInt(ctx, e.configResolver, registry, "limit.thread_message_history", platformScope, 200)
	persistPct := resolveNonNegativeInt(ctx, e.configResolver, registry, "context.compact.persist_trigger_context_pct", platformScope, 0)
	if persistPct > 100 {
		persistPct = 100
	}
	rc.ContextCompact = pipeline.ContextCompactSettings{
		Enabled:                    resolveBool(ctx, e.configResolver, registry, "context.compact.enabled", platformScope, false),
		MaxMessages:                resolveNonNegativeInt(ctx, e.configResolver, registry, "context.compact.max_messages", platformScope, 0),
		MaxUserMessageTokens:       resolveNonNegativeInt(ctx, e.configResolver, registry, "context.compact.max_user_message_tokens", platformScope, 0),
		MaxTotalTextTokens:         resolveNonNegativeInt(ctx, e.configResolver, registry, "context.compact.max_total_text_tokens", platformScope, 0),
		MaxUserTextBytes:           resolveNonNegativeInt(ctx, e.configResolver, registry, "context.compact.max_user_text_bytes", platformScope, 0),
		MaxTotalTextBytes:          resolveNonNegativeInt(ctx, e.configResolver, registry, "context.compact.max_total_text_bytes", platformScope, 0),
		PersistEnabled:             resolveBool(ctx, e.configResolver, registry, "context.compact.persist_enabled", platformScope, false),
		PersistTriggerApproxTokens: resolvePositiveInt(ctx, e.configResolver, registry, "context.compact.persist_trigger_approx_tokens", platformScope, 0),
		PersistTriggerContextPct:   persistPct,
		FallbackContextWindowTokens: resolvePositiveInt(ctx, e.configResolver, registry, "context.compact.fallback_context_window_tokens", platformScope, 200000),
		PersistKeepLastMessages:    resolvePositiveInt(ctx, e.configResolver, registry, "context.compact.persist_keep_last_messages", platformScope, defaultPersistKeepLastMessagesWorker),
	}
	rc.AgentReasoningIterationsLimit = resolveNonNegativeInt(ctx, e.configResolver, registry, "limit.agent_reasoning_iterations", platformScope, 0)
	rc.ToolContinuationBudgetLimit = resolvePositiveInt(ctx, e.configResolver, registry, "limit.tool_continuation_budget", platformScope, 32)
	rc.MaxParallelTasks = resolvePositiveInt(ctx, e.configResolver, registry, "limit.max_parallel_tasks", sharedconfig.Scope{}, 32)
	rc.CreditPerUSD = resolvePositiveInt(ctx, e.configResolver, registry, "credit.per_usd", sharedconfig.Scope{}, 1000)
	rc.LlmMaxResponseBytes = resolvePositiveInt(ctx, e.configResolver, registry, "llm.max_response_bytes", sharedconfig.Scope{}, 16384)
	rc.ReasoningIterations = rc.AgentReasoningIterationsLimit
	rc.ToolContinuationBudget = rc.ToolContinuationBudgetLimit
	if e.memoryProviderFactory != nil {
		rc.MemoryProvider = e.memoryProviderFactory.Resolve(runtimeSnapshot)
	}

	if e.jobQueue != nil && e.broadcastRDB != nil {
		subAgentLimits := subagentctl.SubAgentLimits{
			MaxDepth:                 resolveNonNegativeInt(ctx, e.configResolver, registry, "limit.subagent_max_depth", platformScope, 5),
			MaxActivePerRootRun:      resolveNonNegativeInt(ctx, e.configResolver, registry, "limit.subagent_max_active_per_root_run", platformScope, 20),
			MaxParallelChildren:      resolveNonNegativeInt(ctx, e.configResolver, registry, "limit.subagent_max_parallel_children", platformScope, 5),
			MaxDescendantsPerRootRun: resolveNonNegativeInt(ctx, e.configResolver, registry, "limit.subagent_max_descendants_per_root_run", platformScope, 50),
			MaxPendingPerRootRun:     resolveNonNegativeInt(ctx, e.configResolver, registry, "limit.subagent_max_pending_per_root_run", platformScope, 20),
		}
		bpConfig := subagentctl.BackpressureConfig{
			Enabled:        resolveBool(ctx, e.configResolver, registry, "backpressure.enabled", platformScope, true),
			QueueThreshold: resolveNonNegativeInt(ctx, e.configResolver, registry, "backpressure.queue_threshold", platformScope, 15),
			Strategy:       resolveString(ctx, e.configResolver, registry, "backpressure.strategy", platformScope, "serial"),
		}
		rc.SubAgentControl = subagentctl.NewService(pool, e.broadcastRDB, e.jobQueue, run, traceID, subAgentLimits, bpConfig, e.rolloutBlobStore)
	}

	// Per-run idempotent slot release; deferred as safety net for all exit paths.
	var slotOnce sync.Once
	rc.ReleaseSlot = func() {
		slotOnce.Do(func() {
			if e.releaseSlot != nil {
				e.releaseSlot(context.Background(), run)
			}
		})
	}
	defer rc.ReleaseSlot()

	handler := pipeline.Build(e.middlewares, e.terminal)
	err = handler(ctx, rc)

	// run 结束后清理 sandbox session（不阻塞返回结果）
	if runtimeSnapshot.SandboxBaseURL != "" {
		accountID := run.AccountID.String()
		go sandbox.CleanupSession(runtimeSnapshot.SandboxBaseURL, runtimeSnapshot.SandboxAuthToken, run.ID.String(), accountID)
	}

	return err
}

// 以下辅助函数共享三层回退优先级：
//  1. resolver.Resolve（运行时动态配置，来自数据库）
//  2. registry.Default（编译时静态默认值，来自 DefaultRegistry）
//  3. lastResort（调用方硬编码兜底）
// resolver 或 registry 为 nil 时跳过对应层直接降级。
func resolvePositiveInt(ctx context.Context, resolver sharedconfig.Resolver, registry *sharedconfig.Registry, key string, scope sharedconfig.Scope, lastResort int) int {
	fallback := lastResort
	if registry != nil {
		if entry, ok := registry.Get(key); ok {
			if v, err := strconv.Atoi(strings.TrimSpace(entry.Default)); err == nil && v > 0 {
				fallback = v
			}
		}
	}

	if resolver == nil {
		return fallback
	}
	raw, err := resolver.Resolve(ctx, key, scope)
	if err != nil {
		return fallback
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func resolveBool(ctx context.Context, resolver sharedconfig.Resolver, registry *sharedconfig.Registry, key string, scope sharedconfig.Scope, lastResort bool) bool {
	fallback := lastResort
	if registry != nil {
		if entry, ok := registry.Get(key); ok {
			if v, err := strconv.ParseBool(strings.TrimSpace(entry.Default)); err == nil {
				fallback = v
			}
		}
	}
	if resolver == nil {
		return fallback
	}
	raw, err := resolver.Resolve(ctx, key, scope)
	if err != nil {
		return fallback
	}
	v, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return v
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func resolveString(ctx context.Context, resolver sharedconfig.Resolver, registry *sharedconfig.Registry, key string, scope sharedconfig.Scope, lastResort string) string {
	fallback := lastResort
	if registry != nil {
		if entry, ok := registry.Get(key); ok && entry.Default != "" {
			fallback = entry.Default
		}
	}
	if resolver == nil {
		return fallback
	}
	raw, err := resolver.Resolve(ctx, key, scope)
	if err != nil || strings.TrimSpace(raw) == "" {
		return fallback
	}
	return raw
}

// buildPipeline 按分组组装完整中间件管道，各 layer 间顺序是硬约束。
func buildPipeline(
	deps EngineV1Deps,
	runsRepo data.RunsRepository,
	eventsRepo data.RunEventsRepository,
	messagesRepo data.MessagesRepository,
	resolver *sharedent.Resolver,
	cfgResolver sharedconfig.Resolver,
	releaseSlot func(ctx context.Context, run data.Run),
	compositeScanner *security.CompositeScanner,
	injectionAuditor *security.SecurityAuditor,
	baseAllowlistSet map[string]struct{},
) []pipeline.RunMiddleware {
	var mws []pipeline.RunMiddleware
	mws = append(mws, buildBaseLayer(runsRepo, eventsRepo, messagesRepo, deps.RunControlHub, deps.MessageAttachmentStore, resolver, releaseSlot)...)
	mws = append(mws, buildAgentConfigLayer(deps, runsRepo, eventsRepo, baseAllowlistSet, releaseSlot)...)
	mws = append(mws, buildChannelLayer(deps)...)
	mws = append(mws, buildCapabilityLayer(deps, cfgResolver, compositeScanner, injectionAuditor, eventsRepo)...)
	mws = append(mws, buildRoutingLayer(deps, runsRepo, eventsRepo, messagesRepo, resolver, releaseSlot)...)
	mws = append(mws, buildToolFinalizeLayer(deps)...)
	mws = append(mws, buildDeliveryLayer(deps)...)
	return mws
}

func buildBaseLayer(
	runsRepo data.RunsRepository,
	eventsRepo data.RunEventsRepository,
	messagesRepo data.MessagesRepository,
	runControlHub *pipeline.RunControlHub,
	attachmentStore pipeline.MessageAttachmentStore,
	resolver *sharedent.Resolver,
	releaseSlot func(ctx context.Context, run data.Run),
) []pipeline.RunMiddleware {
	return []pipeline.RunMiddleware{
		pipeline.NewCancelGuardMiddleware(runsRepo, eventsRepo, runControlHub),
		pipeline.NewInputLoaderMiddleware(eventsRepo, messagesRepo, attachmentStore),
		pipeline.NewEntitlementMiddleware(resolver, runsRepo, eventsRepo, releaseSlot),
	}
}

func buildAgentConfigLayer(
	deps EngineV1Deps,
	runsRepo data.RunsRepository,
	eventsRepo data.RunEventsRepository,
	baseAllowlistSet map[string]struct{},
	releaseSlot func(ctx context.Context, run data.Run),
) []pipeline.RunMiddleware {
	return []pipeline.RunMiddleware{
		pipeline.NewMCPDiscoveryMiddleware(
			deps.MCPDiscoveryCache,
			deps.ToolExecutors,
			deps.AllLlmToolSpecs,
			baseAllowlistSet,
			deps.ToolRegistry,
		),
		pipeline.NewToolProviderMiddleware(deps.ToolProviderCache),
		pipeline.NewPersonaResolutionMiddleware(deps.PersonaRegistryGetter, deps.DBPool, runsRepo, eventsRepo, releaseSlot),
	}
}

func buildChannelLayer(deps EngineV1Deps) []pipeline.RunMiddleware {
	return []pipeline.RunMiddleware{
		pipeline.NewChannelContextMiddleware(deps.DBPool),
		pipeline.NewHeartbeatScheduleMiddleware(deps.DBPool),
		pipeline.NewChannelTelegramGroupUserMergeMiddleware(),
		pipeline.NewChannelGroupContextTrimMiddleware(),
		pipeline.NewChannelTelegramToolsMiddleware(deps.ChannelTelegramLoader, nil),
	}
}

func buildCapabilityLayer(
	deps EngineV1Deps,
	cfgResolver sharedconfig.Resolver,
	compositeScanner *security.CompositeScanner,
	injectionAuditor *security.SecurityAuditor,
	eventsRepo data.RunEventsRepository,
) []pipeline.RunMiddleware {
	return []pipeline.RunMiddleware{
		pipeline.NewSubAgentContextMiddleware(subagentctl.NewSnapshotStorage()),
		pipeline.NewSkillContextMiddleware(pipeline.SkillContextConfig{
			Resolve: func(ctx context.Context, accountID uuid.UUID, profileRef, workspaceRef string) ([]skillstore.ResolvedSkill, error) {
				return data.NewSkillsRepository(deps.DBPool).ResolveEnabledSkills(ctx, accountID, profileRef, workspaceRef)
			},
		}),
		pipeline.NewMemoryMiddleware(nil, deps.DBPool, deps.ConfigResolver),
		pipeline.NewTrustSourceMiddleware(cfgResolver),
		pipeline.NewInjectionScanMiddleware(compositeScanner, injectionAuditor, cfgResolver, eventsRepo),
	}
}

func buildRoutingLayer(
	deps EngineV1Deps,
	runsRepo data.RunsRepository,
	eventsRepo data.RunEventsRepository,
	messagesRepo data.MessagesRepository,
	resolver *sharedent.Resolver,
	releaseSlot func(ctx context.Context, run data.Run),
) []pipeline.RunMiddleware {
	return []pipeline.RunMiddleware{
		pipeline.NewRoutingMiddleware(deps.Router, deps.RoutingConfigLoader, deps.AuxGateway, deps.EmitDebugEvents, runsRepo, eventsRepo, releaseSlot, resolver),
		pipeline.NewTitleSummarizerMiddleware(deps.DBPool, deps.RunLimiterRDB, deps.AuxGateway, deps.EmitDebugEvents, deps.RoutingConfigLoader),
		pipeline.NewContextCompactMiddleware(deps.DBPool, messagesRepo, eventsRepo, deps.AuxGateway, deps.EmitDebugEvents, deps.RoutingConfigLoader),
	}
}

func buildToolFinalizeLayer(deps EngineV1Deps) []pipeline.RunMiddleware {
	return []pipeline.RunMiddleware{
		pipeline.NewHeartbeatPrepareMiddleware(),
		pipeline.NewToolDescriptionOverrideMiddleware(deps.ToolDescriptionOverridesRepo),
		pipeline.NewPlatformMiddleware(deps.PlatformToolExecutor),
		pipeline.NewToolBuildMiddleware(),
		pipeline.NewResultSummarizerMiddleware(deps.DBPool, deps.AuxGateway, deps.EmitDebugEvents, 0, deps.RoutingConfigLoader),
	}
}

func buildDeliveryLayer(deps EngineV1Deps) []pipeline.RunMiddleware {
	return []pipeline.RunMiddleware{
		pipeline.NewChannelDeliveryMiddleware(deps.DBPool),
	}
}

func resolveNonNegativeInt(ctx context.Context, resolver sharedconfig.Resolver, registry *sharedconfig.Registry, key string, scope sharedconfig.Scope, lastResort int) int {
	fallback := lastResort
	if registry != nil {
		if entry, ok := registry.Get(key); ok {
			if v, err := strconv.Atoi(strings.TrimSpace(entry.Default)); err == nil && v >= 0 {
				fallback = v
			}
		}
	}

	if resolver == nil {
		return fallback
	}
	raw, err := resolver.Resolve(ctx, key, scope)
	if err != nil {
		return fallback
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v < 0 {
		return fallback
	}
	return v
}
