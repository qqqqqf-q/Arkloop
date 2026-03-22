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

const (
	defaultPersistTriggerApproxTokensWorker = 120000
	defaultPersistKeepLastMessagesWorker    = 40
)

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
	StubGateway     llm.Gateway
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

	// PlatformToolExecutor: platform_manage 的执行器，nil 时跳过注入
	PlatformToolExecutor tools.Executor

	// ChannelTelegramLoader: Telegram Channel 工具取 token；nil 时不注入 telegram_react/reply
	ChannelTelegramLoader channel_telegram.TokenLoader
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

	middlewares := []pipeline.RunMiddleware{
		pipeline.NewCancelGuardMiddleware(runsRepo, eventsRepo, deps.RunControlHub),
		pipeline.NewInputLoaderMiddleware(eventsRepo, messagesRepo, deps.MessageAttachmentStore),
		pipeline.NewHeartbeatScheduleMiddleware(deps.DBPool),
		pipeline.NewEntitlementMiddleware(resolver, runsRepo, eventsRepo, releaseSlot),
		pipeline.NewMCPDiscoveryMiddleware(
			deps.MCPDiscoveryCache,
			deps.ToolExecutors,
			deps.AllLlmToolSpecs,
			baseAllowlistSet,
			deps.ToolRegistry,
		),
		pipeline.NewSpawnAgentMiddleware(),
		pipeline.NewToolProviderMiddleware(deps.ToolProviderCache),
		pipeline.NewPersonaResolutionMiddleware(deps.PersonaRegistryGetter, deps.DBPool, runsRepo, eventsRepo, releaseSlot),
		pipeline.NewChannelContextMiddleware(deps.DBPool),
		pipeline.NewChannelTelegramGroupUserMergeMiddleware(),
		pipeline.NewChannelGroupContextTrimMiddleware(),
		pipeline.NewChannelTelegramToolsMiddleware(deps.ChannelTelegramLoader, nil),
		pipeline.NewSubAgentContextMiddleware(subagentctl.NewSnapshotStorage()),
		pipeline.NewSkillContextMiddleware(pipeline.SkillContextConfig{
			Resolve: func(ctx context.Context, accountID uuid.UUID, profileRef, workspaceRef string) ([]skillstore.ResolvedSkill, error) {
				return data.NewSkillsRepository(deps.DBPool).ResolveEnabledSkills(ctx, accountID, profileRef, workspaceRef)
			},
		}),
		pipeline.NewMemoryMiddleware(nil, deps.DBPool, deps.ConfigResolver),
		pipeline.NewTrustSourceMiddleware(cfgResolver),
		pipeline.NewInjectionScanMiddleware(compositeScanner, injectionAuditor, cfgResolver, eventsRepo),
		pipeline.NewRoutingMiddleware(deps.Router, deps.RoutingConfigLoader, deps.StubGateway, deps.EmitDebugEvents, runsRepo, eventsRepo, releaseSlot, resolver),
		pipeline.NewTitleSummarizerMiddleware(deps.DBPool, deps.RunLimiterRDB, deps.StubGateway, deps.EmitDebugEvents, deps.RoutingConfigLoader),
		pipeline.NewContextCompactMiddleware(deps.DBPool, messagesRepo, eventsRepo, deps.StubGateway, deps.EmitDebugEvents, deps.RoutingConfigLoader),
		pipeline.NewLLMHeartbeatPrepareMiddleware(),
		pipeline.NewToolDescriptionOverrideMiddleware(deps.ToolDescriptionOverridesRepo),
		pipeline.NewCallPlatformMiddleware(),
		pipeline.NewPlatformToolsMiddleware(deps.PlatformToolExecutor),
		pipeline.NewToolBuildMiddleware(),
		pipeline.NewResultSummarizerMiddleware(deps.DBPool, deps.StubGateway, deps.EmitDebugEvents, 0, deps.RoutingConfigLoader),
		pipeline.NewChannelDeliveryMiddleware(deps.DBPool),
	}

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
		PersistTriggerApproxTokens: resolvePositiveInt(ctx, e.configResolver, registry, "context.compact.persist_trigger_approx_tokens", platformScope, defaultPersistTriggerApproxTokensWorker),
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
		rc.SubAgentControl = subagentctl.NewService(pool, e.broadcastRDB, e.jobQueue, run, traceID, subAgentLimits, bpConfig)
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
