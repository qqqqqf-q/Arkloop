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
	"time"

	sharedconfig "arkloop/services/shared/config"
	sharedent "arkloop/services/shared/entitlement"
	"arkloop/services/shared/objectstore"
	"arkloop/services/shared/rollout"
	"arkloop/services/shared/runlimit"
	"arkloop/services/shared/skillstore"
	sharedtoolruntime "arkloop/services/shared/toolruntime"
	promptinjection "arkloop/services/worker/internal/app/promptinjection"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/mcp"
	"arkloop/services/worker/internal/memory"
	notebookprovider "arkloop/services/worker/internal/memory/notebook"
	"arkloop/services/worker/internal/memory/nowledge"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/queue"
	"arkloop/services/worker/internal/routing"
	workerruntime "arkloop/services/worker/internal/runtime"
	"arkloop/services/worker/internal/securitycap"
	"arkloop/services/worker/internal/subagentctl"
	"arkloop/services/worker/internal/toolprovider"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/channel_qq"
	"arkloop/services/worker/internal/tools/builtin/channel_telegram"
	"arkloop/services/worker/internal/tools/builtin/sandbox"
	conversationtool "arkloop/services/worker/internal/tools/conversation"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const defaultPersistKeepLastMessagesWorker = 40

type EngineV1 struct {
	middlewares           []pipeline.RunMiddleware
	terminal              pipeline.RunHandler
	router                *routing.ProviderRouter
	routingConfigLoader   *routing.ConfigLoader
	auxGateway            llm.Gateway
	hookRuntime           *pipeline.HookRuntime
	hookRegistry          *pipeline.HookRegistry
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
	rolloutBlobStore      objectstore.BlobStore
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
	AuxGateway      llm.Gateway
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
	ArtifactStore          objectstore.Store
	RolloutBlobStore       objectstore.BlobStore // 用于创建 RolloutRecorder，非 desktop 模式下可选

	// PlatformToolExecutor: platform_manage 的执行器，nil 时跳过注入
	PlatformToolExecutor tools.Executor

	// ChannelTelegramLoader: Telegram Channel 工具取 token；nil 时不注入 telegram_react/reply
	ChannelTelegramLoader channel_telegram.TokenLoader

	// ChannelQQLoader: QQ Channel 工具取 OneBot URL/token；nil 时不注入 qq_react/reply/send_file
	ChannelQQLoader channel_qq.OneBotConfigLoader

	// GroupSearchExecutor: 群聊搜索执行器；nil 时不注入 group_history_search
	GroupSearchExecutor tools.Executor
}

func serviceExternalSkillDirs(_ context.Context) []string {
	var dirs []string
	if envDirs := strings.TrimSpace(os.Getenv("ARKLOOP_EXTERNAL_SKILL_DIRS")); envDirs != "" {
		dirs = append(dirs, strings.Split(envDirs, string(os.PathListSeparator))...)
	}
	dirs = append(dirs, skillstore.WellKnownSkillDirs()...)
	return dirs
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

	promptInjection, err := promptinjection.Build(promptinjection.BuilderDeps{
		Resolver: deps.ConfigResolver,
		Store:    sharedconfig.NewPGXStore(deps.DBPool),
		AuditDB:  deps.DBPool,
	})
	if err != nil {
		return nil, fmt.Errorf("init prompt injection capability: %w", err)
	}
	cfgResolver := promptInjection.Resolver
	hookRegistry := pipeline.NewHookRegistry()
	hookRegistry.RegisterContextContributor(pipeline.NewNotebookContextContributor(notebookprovider.NewProvider(deps.DBPool)))
	hookRegistry.RegisterContextContributor(pipeline.NewImpressionContextContributor(pipeline.NewPgxImpressionStore(deps.DBPool)))
	if nowledgeProvider := resolveNowledgeProvider(context.Background(), deps.ConfigResolver); nowledgeProvider != nil {
		linkRepo := data.ExternalThreadLinksRepository{}
		hookRegistry.RegisterContextContributor(pipeline.NewNowledgeContextContributor(nowledgeProvider))
		_ = hookRegistry.SetThreadPersistenceProvider(pipeline.NewNowledgeThreadPersistenceProvider(
			nowledgeProvider,
			pgxExternalThreadLinks{repo: linkRepo, pool: deps.DBPool},
		))
	}
	hookRegistry.RegisterAfterThreadPersistHook(pipeline.NewLegacyMemoryDistillObserver(
		pipeline.NewPgxMemorySnapshotStore(deps.DBPool),
		deps.DBPool,
		deps.ConfigResolver,
		pipeline.NewPgxImpressionStore(deps.DBPool),
		newPgxImpressionRefresh(deps),
	))
	hookRegistry.RegisterAfterThreadPersistHook(pipeline.NewContextCompactMaintenanceObserver(deps.JobQueue))

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
	//   ThreadPersist   — 包裹 ChannelDelivery：thread persist 依赖最终助手输出
	//   ChannelDelivery — 包裹 handler：run 结束后立刻停 typing 并执行渠道投递
	middlewares := buildPipeline(deps, runsRepo, eventsRepo, messagesRepo, resolver, releaseSlot, promptInjection, baseAllowlistSet)

	terminal := pipeline.NewAgentLoopHandler(runsRepo, eventsRepo, messagesRepo, deps.RunLimiterRDB, deps.JobQueue, usageRepo, creditsRepo, resolver)

	return &EngineV1{
		middlewares:           middlewares,
		terminal:              terminal,
		router:                deps.Router,
		routingConfigLoader:   deps.RoutingConfigLoader,
		auxGateway:            deps.AuxGateway,
		hookRuntime:           pipeline.NewHookRuntime(hookRegistry, pipeline.NewDefaultHookResultApplier()),
		hookRegistry:          hookRegistry,
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
		rolloutBlobStore:      deps.RolloutBlobStore,
	}, nil
}

type pgxExternalThreadLinks struct {
	repo data.ExternalThreadLinksRepository
	pool *pgxpool.Pool
}

func (s pgxExternalThreadLinks) Get(ctx context.Context, accountID, threadID uuid.UUID, provider string) (string, bool, error) {
	return s.repo.Get(ctx, s.pool, accountID, threadID, provider)
}

func (s pgxExternalThreadLinks) Upsert(ctx context.Context, accountID, threadID uuid.UUID, provider, externalThreadID string) error {
	return s.repo.Upsert(ctx, s.pool, accountID, threadID, provider, externalThreadID)
}

func resolveNowledgeProvider(ctx context.Context, resolver sharedconfig.Resolver) *nowledge.Client {
	providerName := strings.TrimSpace(os.Getenv("ARKLOOP_MEMORY_PROVIDER"))
	if providerName == "" && resolver != nil {
		baseURL, _ := resolver.Resolve(ctx, "nowledge.base_url", sharedconfig.Scope{})
		if strings.TrimSpace(baseURL) != "" {
			providerName = "nowledge"
		}
	}
	if providerName != "nowledge" {
		return nil
	}
	cfg := nowledge.Config{
		BaseURL:          strings.TrimSpace(os.Getenv("ARKLOOP_NOWLEDGE_BASE_URL")),
		APIKey:           strings.TrimSpace(os.Getenv("ARKLOOP_NOWLEDGE_API_KEY")),
		RequestTimeoutMs: 30000,
	}
	if resolver != nil {
		if cfg.BaseURL == "" {
			if value, err := resolver.Resolve(ctx, "nowledge.base_url", sharedconfig.Scope{}); err == nil {
				cfg.BaseURL = strings.TrimSpace(value)
			}
		}
		if cfg.APIKey == "" {
			if value, err := resolver.Resolve(ctx, "nowledge.api_key", sharedconfig.Scope{}); err == nil {
				cfg.APIKey = strings.TrimSpace(value)
			}
		}
		if value, err := resolver.Resolve(ctx, "nowledge.request_timeout_ms", sharedconfig.Scope{}); err == nil {
			if parsed, parseErr := strconv.Atoi(strings.TrimSpace(value)); parseErr == nil && parsed > 0 {
				cfg.RequestTimeoutMs = parsed
			}
		}
	}
	return nowledge.NewClient(cfg)
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
	var tracer pipeline.Tracer
	accountSettingsRepo := data.NewAccountSettingsRepository(pool)
	if enabled, traceErr := accountSettingsRepo.PipelineTraceEnabled(ctx, run.AccountID); traceErr != nil {
		slog.WarnContext(ctx, "pipeline trace setting load failed", "account_id", run.AccountID.String(), "err", traceErr.Error())
	} else if enabled {
		tracer = pipeline.NewBufTracer(run.ID, run.AccountID, data.NewRunPipelineEventsRepository(pool))
	}
	var promptCacheDebugEnabled bool
	if debugEnabled, debugErr := accountSettingsRepo.PromptCacheDebugEnabled(ctx, run.AccountID); debugErr != nil {
		slog.WarnContext(ctx, "prompt cache debug setting load failed", "account_id", run.AccountID.String(), "err", debugErr.Error())
	} else {
		promptCacheDebugEnabled = debugEnabled
	}
	rc := &pipeline.RunContext{
		Run:                     run,
		DB:                      pool,
		RunStatusDB:             data.RunsRepository{},
		Pool:                    pool,
		MemoryServiceDB:         pool,
		MemorySnapshotStore:     pipeline.NewPgxMemorySnapshotStore(pool),
		DirectPool:              directPool,
		BroadcastRDB:            e.broadcastRDB,
		TraceID:                 traceID,
		Tracer:                  tracer,
		Emitter:                 events.NewEmitter(traceID),
		Router:                  e.router,
		Runtime:                 &runtimeSnapshot,
		HookRuntime:             e.hookRuntime,
		HookRegistry:            e.hookRegistry,
		UserID:                  run.CreatedByUserID,
		JobPayload:              cloneMap(input.JobPayload),
		ProfileRef:              derefString(run.ProfileRef),
		WorkspaceRef:            derefString(run.WorkspaceRef),
		ExecutorBuilder:         e.executorRegistry,
		MemoryProvider:          nil,
		PendingMemoryWrites:     memory.NewPendingWriteBuffer(),
		ToolBudget:              map[string]any{},
		PerToolSoftLimits:       tools.DefaultPerToolSoftLimits(),
		LlmRetryMaxAttempts:     e.llmRetryMaxAttempts,
		LlmRetryBaseDelayMs:     e.llmRetryBaseDelayMs,
		PromptCacheDebugEnabled: promptCacheDebugEnabled,
	}
	if e.rolloutBlobStore != nil {
		recorder := rollout.NewRecorder(e.rolloutBlobStore, run.ID)
		recorder.Start(ctx)
		rc.RolloutRecorder = recorder
		rc.ResponseDraftStore = e.rolloutBlobStore
		defer func() { _ = recorder.Close(context.Background()) }()
	}

	registry := sharedconfig.DefaultRegistry()
	platformScope := sharedconfig.Scope{}
	rc.ThreadMessageHistoryLimit = 0
	persistPct := resolveNonNegativeInt(ctx, e.configResolver, registry, "context.compact.persist_trigger_context_pct", platformScope, 0)
	if persistPct > 100 {
		persistPct = 100
	}
	targetPct := resolveNonNegativeInt(ctx, e.configResolver, registry, "context.compact.target_context_pct", platformScope, 65)
	if targetPct > 100 {
		targetPct = 100
	}
	if targetPct <= 0 {
		targetPct = 65
	}
	compactEnabled := resolveBool(ctx, e.configResolver, registry, "context.compact.enabled", platformScope, false)
	rc.ContextCompact = pipeline.ContextCompactSettings{
		Enabled:                     compactEnabled,
		MaxMessages:                 resolveNonNegativeInt(ctx, e.configResolver, registry, "context.compact.max_messages", platformScope, 0),
		MaxUserMessageTokens:        resolveNonNegativeInt(ctx, e.configResolver, registry, "context.compact.max_user_message_tokens", platformScope, 0),
		MaxTotalTextTokens:          resolveNonNegativeInt(ctx, e.configResolver, registry, "context.compact.max_total_text_tokens", platformScope, 0),
		MaxUserTextBytes:            resolveNonNegativeInt(ctx, e.configResolver, registry, "context.compact.max_user_text_bytes", platformScope, 0),
		MaxTotalTextBytes:           resolveNonNegativeInt(ctx, e.configResolver, registry, "context.compact.max_total_text_bytes", platformScope, 0),
		PersistEnabled:              compactEnabled,
		PersistTriggerApproxTokens:  0,
		PersistTriggerContextPct:    persistPct,
		FallbackContextWindowTokens: resolvePositiveInt(ctx, e.configResolver, registry, "context.compact.fallback_context_window_tokens", platformScope, 128000),
		TargetContextPct:            targetPct,
		PersistKeepLastMessages:     defaultPersistKeepLastMessagesWorker,
		PersistKeepTailPct:          0,
		CompactZoneBudgetPct:        resolveNonNegativeInt(ctx, e.configResolver, registry, "context.compact.compact_zone_budget_pct", platformScope, 0),
		MicrocompactKeepRecentTools: 0,
	}
	rc.AgentReasoningIterationsLimit = resolveNonNegativeInt(ctx, e.configResolver, registry, "limit.agent_reasoning_iterations", platformScope, 0)
	rc.ToolContinuationBudgetLimit = resolvePositiveInt(ctx, e.configResolver, registry, "limit.tool_continuation_budget", platformScope, 32)
	rc.MaxParallelTasks = resolvePositiveInt(ctx, e.configResolver, registry, "limit.max_parallel_tasks", sharedconfig.Scope{}, 32)
	rc.RunWallClockTimeout = time.Duration(resolvePositiveInt(ctx, e.configResolver, registry, "limit.run_wall_clock_timeout_ms", platformScope, 900000)) * time.Millisecond
	rc.PausedInputTimeout = time.Duration(resolvePositiveInt(ctx, e.configResolver, registry, "limit.paused_input_timeout_ms", platformScope, 300000)) * time.Millisecond
	rc.IdleHeartbeatInterval = time.Duration(resolvePositiveInt(ctx, e.configResolver, registry, "limit.idle_heartbeat_interval_ms", platformScope, 15000)) * time.Millisecond
	rc.CreditPerUSD = resolvePositiveInt(ctx, e.configResolver, registry, "credit.per_usd", sharedconfig.Scope{}, 1000)
	rc.LlmMaxResponseBytes = resolvePositiveInt(ctx, e.configResolver, registry, "llm.max_response_bytes", sharedconfig.Scope{}, 16384)
	rc.ReasoningIterations = rc.AgentReasoningIterationsLimit
	rc.ToolContinuationBudget = rc.ToolContinuationBudgetLimit
	if e.memoryProviderFactory != nil {
		rc.MemoryProvider = e.memoryProviderFactory.Resolve(runtimeSnapshot)
	}

	if e.jobQueue != nil && e.broadcastRDB != nil {
		subAgentLimits := subagentctl.SubAgentLimits{
			MaxDepth:                     resolveNonNegativeInt(ctx, e.configResolver, registry, "limit.subagent_max_depth", platformScope, 5),
			MaxActivePerThread:           resolveNonNegativeInt(ctx, e.configResolver, registry, "limit.subagent_max_active_per_thread", platformScope, 20),
			MaxParallelChildrenPerThread: resolveNonNegativeInt(ctx, e.configResolver, registry, "limit.subagent_max_parallel_children_per_thread", platformScope, 5),
			MaxDescendantsPerThread:      resolveNonNegativeInt(ctx, e.configResolver, registry, "limit.subagent_max_descendants_per_thread", platformScope, 50),
			MaxPendingPerThread:          resolveNonNegativeInt(ctx, e.configResolver, registry, "limit.subagent_max_pending_per_thread", platformScope, 20),
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
	defer pipeline.FlushTracer(rc.Tracer)

	handler := pipeline.Build(e.middlewares, e.terminal)
	err = handler(ctx, rc)

	// run 结束后清理 sandbox session（不阻塞返回结果）
	if cleaner, ok := rc.ToolExecutors["exec_command"].(interface {
		CleanupRun(context.Context, string, string) error
	}); ok {
		go func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			terminalStatus := ""
			if ctx.Err() != nil {
				terminalStatus = "cancelled"
			}
			if cleanupErr := cleaner.CleanupRun(cleanupCtx, run.ID.String(), terminalStatus); cleanupErr != nil {
				slog.Warn("shell cleanup failed", "run_id", run.ID.String(), "error", cleanupErr.Error())
			}
		}()
	}
	if cleaner, ok := rc.ToolExecutors["spawn_acp"].(interface {
		CleanupRun(context.Context, string, string) error
	}); ok {
		go func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			terminalStatus := ""
			if ctx.Err() != nil {
				terminalStatus = "cancelled"
			}
			if cleanupErr := cleaner.CleanupRun(cleanupCtx, run.ID.String(), terminalStatus); cleanupErr != nil {
				slog.Warn("acp cleanup failed", "run_id", run.ID.String(), "error", cleanupErr.Error())
			}
		}()
	}
	if cleaner, ok := rc.ToolExecutors["read"].(interface{ CleanupRun(string) }); ok {
		cleaner.CleanupRun(run.ID.String())
	}
	if runtimeSnapshot.SandboxBaseURL != "" {
		accountID := run.AccountID.String()
		go sandbox.CleanupSession(runtimeSnapshot.SandboxBaseURL, runtimeSnapshot.SandboxAuthToken, run.ID.String(), accountID)
	}
	go tools.CleanupPersistedToolOutputs(run.ThreadID.String())
	tools.CleanupRunDisk(run.ID.String())

	return err
}

type ContextCompactMaintenanceInput struct {
	RouteID             string
	UpperBoundThreadSeq int64
	ContextWindowTokens int
	TriggerContextPct   int
	TargetContextPct    int
	JobPayload          map[string]any
}

func (e *EngineV1) ExecuteContextCompactMaintenance(
	ctx context.Context,
	pool *pgxpool.Pool,
	run data.Run,
	traceID string,
	input ContextCompactMaintenanceInput,
) error {
	if pool == nil {
		return fmt.Errorf("pool must not be nil")
	}
	if run.AccountID == uuid.Nil || run.ThreadID == uuid.Nil || run.ID == uuid.Nil {
		return fmt.Errorf("run identity must not be empty")
	}
	if input.UpperBoundThreadSeq <= 0 {
		return nil
	}

	upperBoundMessageID, ok, err := lookupThreadMessageIDBySeq(ctx, pool, run.AccountID, run.ThreadID, input.UpperBoundThreadSeq)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	runsRepo := data.RunsRepository{}
	eventsRepo := data.RunEventsRepository{}
	messagesRepo := data.MessagesRepository{}
	loadPayload := map[string]any{
		"channel_delivery":       map[string]any{"source": "context_compact_maintain"},
		"thread_tail_message_id": upperBoundMessageID.String(),
	}
	loaded, err := pipeline.LoadRunInputs(ctx, pool, run, loadPayload, runsRepo, eventsRepo, messagesRepo, nil, nil, 0)
	if err != nil {
		return err
	}
	if loaded == nil {
		return nil
	}
	if loaded.InputJSON == nil {
		loaded.InputJSON = map[string]any{}
	}
	if cleanedRouteID := strings.TrimSpace(input.RouteID); cleanedRouteID != "" {
		loaded.InputJSON["route_id"] = cleanedRouteID
	}

	registry := sharedconfig.DefaultRegistry()
	platformScope := sharedconfig.Scope{}
	llmMaxResponseBytes := resolvePositiveInt(ctx, e.configResolver, registry, "llm.max_response_bytes", sharedconfig.Scope{}, 16384)
	selectedRoute, gateway, contextWindowTokens, err := e.resolveCompactMaintenanceRoute(ctx, run.AccountID, loaded.InputJSON, input.ContextWindowTokens, llmMaxResponseBytes)
	if err != nil {
		return err
	}

	triggerPct := input.TriggerContextPct
	if triggerPct <= 0 {
		triggerPct = resolveNonNegativeInt(ctx, e.configResolver, registry, "context.compact.persist_trigger_context_pct", platformScope, 85)
	}
	if triggerPct > 100 {
		triggerPct = 100
	}
	targetPct := input.TargetContextPct
	if targetPct <= 0 {
		targetPct = resolveNonNegativeInt(ctx, e.configResolver, registry, "context.compact.target_context_pct", platformScope, 65)
	}
	if targetPct > 100 {
		targetPct = 100
	}
	if targetPct <= 0 {
		targetPct = 65
	}
	fallbackWindowTokens := input.ContextWindowTokens
	if fallbackWindowTokens <= 0 {
		fallbackWindowTokens = resolvePositiveInt(ctx, e.configResolver, registry, "context.compact.fallback_context_window_tokens", platformScope, 128000)
	}

	rc := &pipeline.RunContext{
		Run:                   run,
		DB:                    pool,
		RunStatusDB:           data.RunsRepository{},
		Pool:                  pool,
		MemoryServiceDB:       pool,
		MemorySnapshotStore:   pipeline.NewPgxMemorySnapshotStore(pool),
		DirectPool:            chooseDirectPool(e.directPool, pool),
		BroadcastRDB:          e.broadcastRDB,
		TraceID:               strings.TrimSpace(traceID),
		Emitter:               events.NewEmitter(traceID),
		Router:                e.router,
		JobPayload:            cloneMap(input.JobPayload),
		InputJSON:             loaded.InputJSON,
		Messages:              loaded.Messages,
		ThreadMessageIDs:      loaded.ThreadMessageIDs,
		ThreadContextFrontier: append([]pipeline.FrontierNode(nil), loaded.ThreadContextFrontier...),
		Gateway:               gateway,
		SelectedRoute:         selectedRoute,
		ContextWindowTokens:   contextWindowTokens,
		LlmMaxResponseBytes:   llmMaxResponseBytes,
	}
	rc.ContextCompact = pipeline.ContextCompactSettings{
		Enabled:                     false,
		PersistEnabled:              true,
		PersistTriggerApproxTokens:  0,
		PersistTriggerContextPct:    triggerPct,
		FallbackContextWindowTokens: fallbackWindowTokens,
		TargetContextPct:            targetPct,
		PersistKeepLastMessages:     defaultPersistKeepLastMessagesWorker,
		PersistKeepTailPct:          0,
		CompactZoneBudgetPct:        resolveNonNegativeInt(ctx, e.configResolver, registry, "context.compact.compact_zone_budget_pct", platformScope, 0),
		MicrocompactKeepRecentTools: 0,
	}

	return pipeline.ExecuteContextCompactMaintenanceJob(ctx, rc, &upperBoundMessageID, eventsRepo)
}

func (e *EngineV1) resolveCompactMaintenanceRoute(
	ctx context.Context,
	accountID uuid.UUID,
	inputJSON map[string]any,
	contextWindowTokens int,
	llmMaxResponseBytes int,
) (*routing.SelectedProviderRoute, llm.Gateway, int, error) {
	activeRouter := e.router
	if activeRouter == nil {
		return nil, nil, 0, fmt.Errorf("router must not be nil")
	}
	if e.routingConfigLoader != nil {
		if loaded, err := e.routingConfigLoader.Load(ctx, &accountID); err == nil && len(loaded.Routes) > 0 {
			activeRouter = routing.NewProviderRouter(loaded)
		}
	}

	decision := activeRouter.Decide(inputJSON, false, false)
	if decision.Denied != nil {
		return nil, nil, 0, fmt.Errorf("%s: %s", decision.Denied.Code, decision.Denied.Message)
	}
	if decision.Selected == nil {
		return nil, nil, 0, fmt.Errorf("route decision is empty")
	}
	selected := *decision.Selected
	if contextWindowTokens > 0 {
		advanced := cloneMap(selected.Route.AdvancedJSON)
		if advanced == nil {
			advanced = map[string]any{}
		}
		advanced["context_window_tokens"] = float64(contextWindowTokens)
		selected.Route.AdvancedJSON = advanced
	}

	windowTokens := routing.RouteContextWindowTokens(selected.Route)
	if selected.Credential.ProviderKind == routing.ProviderKindStub {
		if e.auxGateway == nil {
			return nil, nil, 0, fmt.Errorf("aux gateway must not be nil for stub route")
		}
		return &selected, e.auxGateway, windowTokens, nil
	}
	resolved, err := pipeline.ResolveGatewayConfigFromSelectedRoute(selected, false, llmMaxResponseBytes)
	if err != nil {
		return nil, nil, 0, err
	}
	gateway, err := llm.NewGatewayFromResolvedConfig(resolved)
	if err != nil {
		return nil, nil, 0, err
	}
	return &selected, gateway, windowTokens, nil
}

func chooseDirectPool(directPool *pgxpool.Pool, fallback *pgxpool.Pool) *pgxpool.Pool {
	if directPool != nil {
		return directPool
	}
	return fallback
}

func lookupThreadMessageIDBySeq(
	ctx context.Context,
	pool *pgxpool.Pool,
	accountID uuid.UUID,
	threadID uuid.UUID,
	threadSeq int64,
) (uuid.UUID, bool, error) {
	if pool == nil || accountID == uuid.Nil || threadID == uuid.Nil || threadSeq <= 0 {
		return uuid.Nil, false, nil
	}
	var messageID uuid.UUID
	err := pool.QueryRow(
		ctx,
		`SELECT id
		   FROM messages
		  WHERE account_id = $1
		    AND thread_id = $2
		    AND thread_seq = $3
		    AND deleted_at IS NULL
		  LIMIT 1`,
		accountID,
		threadID,
		threadSeq,
	).Scan(&messageID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, false, err
	}
	return messageID, messageID != uuid.Nil, nil
}

// 以下辅助函数共享三层回退优先级：
//  1. resolver.Resolve（运行时动态配置，来自数据库）
//  2. registry.Default（编译时静态默认值，来自 DefaultRegistry）
//  3. lastResort（调用方硬编码兜底）
//
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
	releaseSlot func(ctx context.Context, run data.Run),
	promptInjection securitycap.Runtime,
	baseAllowlistSet map[string]struct{},
) []pipeline.RunMiddleware {
	var mws []pipeline.RunMiddleware
	mws = append(mws, buildBaseLayer(runsRepo, eventsRepo, messagesRepo, deps.RunControlHub, deps.MessageAttachmentStore, deps.RolloutBlobStore, resolver, releaseSlot)...)
	mws = append(mws, buildAgentConfigLayer(deps, runsRepo, eventsRepo, baseAllowlistSet, releaseSlot)...)
	mws = append(mws, buildChannelLayer(deps, messagesRepo, eventsRepo)...)
	mws = append(mws, pipeline.NewScheduledJobPrepareMiddleware())
	mws = append(mws, buildCapabilityLayer(deps, promptInjection, eventsRepo)...)
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
	rolloutStore objectstore.BlobStore,
	resolver *sharedent.Resolver,
	releaseSlot func(ctx context.Context, run data.Run),
) []pipeline.RunMiddleware {
	return []pipeline.RunMiddleware{
		pipeline.NewCancelGuardMiddleware(runsRepo, eventsRepo, runControlHub),
		pipeline.NewInputLoaderMiddleware(runsRepo, eventsRepo, messagesRepo, attachmentStore, rolloutStore),
		pipeline.NewSubAgentCallbackMiddleware(),
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
			func(*pipeline.RunContext) mcp.DiscoveryQueryer { return deps.DBPool },
			eventsRepo,
			deps.ToolExecutors,
			deps.AllLlmToolSpecs,
			baseAllowlistSet,
			deps.ToolRegistry,
		),
		pipeline.NewToolProviderMiddleware(deps.ToolProviderCache),
		pipeline.NewPersonaResolutionMiddleware(deps.PersonaRegistryGetter, deps.DBPool, runsRepo, eventsRepo, releaseSlot),
	}
}

func buildChannelLayer(deps EngineV1Deps, messagesRepo data.MessagesRepository, eventsRepo data.RunEventsRepository) []pipeline.RunMiddleware {
	return []pipeline.RunMiddleware{
		pipeline.NewChannelContextMiddleware(deps.DBPool),
		pipeline.NewHeartbeatScheduleMiddleware(deps.DBPool),
		pipeline.NewChannelAdminTagMiddleware(deps.DBPool),
		pipeline.NewChannelTelegramGroupUserMergeMiddleware(),
		pipeline.NewChannelTelegramToolsMiddleware(nil, nil, pipeline.ChannelTelegramToolsDeps{
			TokenLoader:        deps.ChannelTelegramLoader,
			ArtifactStore:      deps.ArtifactStore,
			GroupSearchExec:    deps.GroupSearchExecutor,
			GroupSearchLlmSpec: conversationtool.GroupSearchLlmSpec,
		}),
		pipeline.NewStickerToolMiddleware(deps.DBPool),
		pipeline.NewStickerInjectMiddleware(deps.DBPool),
		pipeline.NewChannelQQToolsMiddleware(pipeline.ChannelQQToolsDeps{
			ConfigLoader:    deps.ChannelQQLoader,
			GroupSearchExec: deps.GroupSearchExecutor,
			GroupSearchSpec: conversationtool.GroupSearchLlmSpec,
		}),
		pipeline.NewChannelEndReplyMiddleware(),
	}
}

func buildCapabilityLayer(
	deps EngineV1Deps,
	promptInjection securitycap.Runtime,
	eventsRepo data.RunEventsRepository,
) []pipeline.RunMiddleware {
	memoryMW := pipeline.NewMemoryMiddleware(
		nil,
		pipeline.NewPgxMemorySnapshotStore(deps.DBPool),
		deps.DBPool,
		deps.ConfigResolver,
		pipeline.NewPgxImpressionStore(deps.DBPool),
		newPgxImpressionRefresh(deps),
	)
	promptHookMW := pipeline.NewPromptHookMiddleware()
	mws := []pipeline.RunMiddleware{
		pipeline.NewSubAgentContextMiddleware(subagentctl.NewSnapshotStorage()),
		pipeline.NewSkillContextMiddleware(pipeline.SkillContextConfig{
			Resolve: func(ctx context.Context, accountID uuid.UUID, profileRef, workspaceRef string) ([]skillstore.ResolvedSkill, error) {
				return data.NewSkillsRepository(deps.DBPool).ResolveEnabledSkills(ctx, accountID, profileRef, workspaceRef)
			},
			ExternalDirs: serviceExternalSkillDirs,
		}),
		traceMemoryInjectionMiddleware(func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
			return promptHookMW(ctx, rc, func(ctx context.Context, rc *pipeline.RunContext) error {
				return memoryMW(ctx, rc, next)
			})
		}),
		pipeline.NewRuntimeContextMiddleware(),
	}
	mws = append(mws, promptInjection.Middlewares(eventsRepo)...)
	return mws
}

func traceMemoryInjectionMiddleware(inner pipeline.RunMiddleware) pipeline.RunMiddleware {
	if inner == nil {
		return nil
	}
	return func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		before := rc.MaterializedSystemPrompt()
		err := inner(ctx, rc, next)
		if rc != nil && rc.Tracer != nil {
			delta := rc.MaterializedSystemPrompt()
			delta = strings.TrimPrefix(delta, before)
			rc.Tracer.Event("memory_injection", "memory_injection.completed", map[string]any{
				"memory_injected":   strings.Contains(delta, "<memory>"),
				"notebook_injected": strings.Contains(delta, "<notebook>"),
				"injection_len":     len(delta),
			})
		}
		return err
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
		pipeline.NewChannelGroupContextTrimMiddleware(pipeline.GroupContextTrimDeps{
			Pool:            deps.DBPool,
			MessagesRepo:    messagesRepo,
			EventsRepo:      eventsRepo,
			EmitDebugEvents: deps.EmitDebugEvents,
		}),
		pipeline.NewTitleSummarizerMiddleware(deps.DBPool, deps.RunLimiterRDB, deps.AuxGateway, deps.EmitDebugEvents, deps.RoutingConfigLoader),
		pipeline.NewContextCompactMiddleware(deps.DBPool, messagesRepo, eventsRepo, deps.AuxGateway, deps.EmitDebugEvents, deps.RoutingConfigLoader),
	}
}

func buildToolFinalizeLayer(deps EngineV1Deps) []pipeline.RunMiddleware {
	return []pipeline.RunMiddleware{
		pipeline.NewImpressionPrepareMiddleware(pipeline.NewPgxImpressionStore(deps.DBPool), deps.DBPool, deps.AuxGateway, deps.EmitDebugEvents, deps.RoutingConfigLoader),
		pipeline.NewStickerPrepareMiddleware(deps.DBPool, deps.MessageAttachmentStore),
		pipeline.NewHeartbeatPrepareMiddleware(),
		pipeline.NewConditionalToolsMiddleware(),
		pipeline.NewToolDescriptionOverrideMiddleware(deps.ToolDescriptionOverridesRepo),
		pipeline.NewPlatformMiddleware(deps.PlatformToolExecutor),
		pipeline.NewToolBuildMiddleware(),
		pipeline.NewToolLoopDetectionMiddleware(),
		pipeline.NewResultSummarizerMiddleware(deps.DBPool, deps.AuxGateway, deps.EmitDebugEvents, 0, deps.RoutingConfigLoader),
	}
}

func buildDeliveryLayer(deps EngineV1Deps) []pipeline.RunMiddleware {
	return []pipeline.RunMiddleware{
		pipeline.NewThreadPersistHookMiddleware(),
		pipeline.NewChannelDeliveryMiddlewareWithOptions(deps.DBPool, pipeline.ChannelDeliveryMiddlewareOptions{
			StickerStore: deps.MessageAttachmentStore,
		}),
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

func newPgxImpressionRefresh(deps EngineV1Deps) pipeline.ImpressionRefreshFunc {
	if deps.DBPool == nil || deps.JobQueue == nil {
		return nil
	}
	return pipeline.NewImpressionRefreshFunc(pipeline.ImpressionRefreshDeps{
		ExecSQL: func(ctx context.Context, sql string, args ...any) error {
			_, err := deps.DBPool.Exec(ctx, sql, args...)
			return err
		},
		QueryRowScan: func(ctx context.Context, sql string, args []any, dest ...any) error {
			return deps.DBPool.QueryRow(ctx, sql, args...).Scan(dest...)
		},
		EnqueueRun: func(ctx context.Context, accountID, runID uuid.UUID, traceID, jobType string, payload map[string]any) error {
			_, err := deps.JobQueue.EnqueueRun(ctx, accountID, runID, traceID, jobType, payload, nil)
			return err
		},
	})
}
