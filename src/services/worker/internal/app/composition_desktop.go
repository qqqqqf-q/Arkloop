//go:build desktop

package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/shared/desktop"
	sharedencryption "arkloop/services/shared/encryption"
	"arkloop/services/shared/eventbus"
	sharedexec "arkloop/services/shared/executionconfig"
	"arkloop/services/shared/objectstore"
	"arkloop/services/shared/onebotclient"
	"arkloop/services/shared/rollout"
	"arkloop/services/shared/telegrambot"
	sharedtoolruntime "arkloop/services/shared/toolruntime"
	promptinjection "arkloop/services/worker/internal/app/promptinjection"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/environmentbindings"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/mcp"
	"arkloop/services/worker/internal/memory"
	localmemory "arkloop/services/worker/internal/memory/local"
	"arkloop/services/worker/internal/memory/nowledge"
	"arkloop/services/worker/internal/memory/openviking"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/queue"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/runtime"
	"arkloop/services/worker/internal/securitycap"
	"arkloop/services/worker/internal/subagentctl"
	"arkloop/services/worker/internal/toolprovider"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin"
	"arkloop/services/worker/internal/tools/builtin/acptool"
	"arkloop/services/worker/internal/tools/builtin/read"
	sandboxbuiltin "arkloop/services/worker/internal/tools/builtin/sandbox"
	conversationtool "arkloop/services/worker/internal/tools/conversation"
	"arkloop/services/worker/internal/tools/localshell"
	memorytool "arkloop/services/worker/internal/tools/memory"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type desktopTelegramTokenLoader struct {
	db data.DesktopDB
}

func (d *desktopTelegramTokenLoader) BotToken(ctx context.Context, channelID uuid.UUID) (string, error) {
	if d.db == nil {
		return "", fmt.Errorf("telegram channel tools: db unavailable")
	}
	rec, err := loadDesktopDeliveryChannel(ctx, d.db, channelID)
	if err != nil {
		return "", err
	}
	if rec == nil {
		return "", fmt.Errorf("telegram channel tools: channel not found")
	}
	return strings.TrimSpace(rec.Token), nil
}

type desktopQQOneBotConfigLoader struct {
	db data.DesktopDB
}

func (d *desktopQQOneBotConfigLoader) OneBotConfig(ctx context.Context, channelID uuid.UUID) (string, string, error) {
	if d.db == nil {
		return "", "", fmt.Errorf("qq channel tools: db unavailable")
	}
	rec, err := loadDesktopQQDeliveryChannel(ctx, d.db, channelID)
	if err != nil {
		return "", "", err
	}
	if rec == nil {
		return "", "", fmt.Errorf("qq channel tools: channel not found")
	}
	return rec.OneBotHTTPURL, rec.OneBotToken, nil
}

// DesktopEngine executes LLM agent runs backed by SQLite.
type DesktopEngine struct {
	db                     data.DesktopDB
	bus                    eventbus.EventBus
	auxRouter              *routing.ProviderRouter
	auxGateway             llm.Gateway
	emitDebugEvents        bool
	toolRegistry           *tools.Registry
	toolExecutors          map[string]tools.Executor
	allLlmSpecs            []llm.ToolSpec
	baseAllowlist          map[string]struct{}
	executorRegistry       pipeline.AgentExecutorBuilder
	personaRegistry        func() *personas.Registry
	notebookProvider       memory.MemoryProvider
	memProvider            memory.MemoryProvider
	useOV                  bool
	useVM                  bool
	skillLayout            pipeline.SkillLayoutResolver
	runtimeSnapshot        *sharedtoolruntime.RuntimeSnapshot
	jobQueue               queue.JobQueue
	routingLoader          *routing.ConfigLoader
	messageAttachmentStore objectstore.Store
	rolloutStore           objectstore.BlobStore
	promptInjection        securitycap.Runtime
	groupSearchExec        tools.Executor
	mcpPool                *mcp.Pool
	mcpDiscoveryCache      *mcp.DiscoveryCache
	shellExecutor          *runtime.DynamicShellExecutor
	hookRuntime            *pipeline.HookRuntime
	hookRegistry           *pipeline.HookRegistry
}

const defaultDesktopStageEventMs = 250

var desktopObservedEventNames = map[string]string{
	"input_loader":               "run.input_loader",
	"heartbeat_schedule":         "run.heartbeat_schedule",
	"tool_provider_bindings":     "run.tool_provider_bindings",
	"spawn_agent":                "run.spawn_agent",
	"persona_resolution":         "run.persona_resolution",
	"channel_context":            "run.channel_context",
	"channel_admin_tag":          "run.channel_admin_tag",
	"channel_group_user_merge":   "run.channel_group_user_merge",
	"channel_group_context_trim": "run.channel_group_context_trim",
	"channel_telegram_tools":     "run.channel_telegram_tools",
	"sub_agent_context":          "run.sub_agent_context",
	"skill_context":              "run.skill_context",
	"memory_injection":           "run.memory_injection",
	"runtime_context":            "run.runtime_context",
}

// ComposeDesktopEngine assembles a DesktopEngine from environment configuration.
// execRegistry is the agent executor builder (e.g., executor.DefaultExecutorRegistry()).
func ComposeDesktopEngine(ctx context.Context, db data.DesktopDB, bus eventbus.EventBus, execRegistry pipeline.AgentExecutorBuilder, jobQueue queue.JobQueue) (*DesktopEngine, error) {
	// Router is loaded dynamically per-run in desktopRouting middleware
	// so that credentials configured after startup are picked up immediately.
	auxRouter := routing.NewProviderRouter(routing.DefaultRoutingConfig())

	auxCfg, err := llm.AuxGatewayConfigFromEnv()
	if err != nil {
		return nil, fmt.Errorf("stub gateway config: %w", err)
	}
	auxGateway := llm.NewAuxGateway(auxCfg)

	toolRegistry := tools.NewRegistry()
	for _, spec := range builtin.AgentSpecs() {
		if err := toolRegistry.Register(spec); err != nil {
			slog.WarnContext(ctx, "desktop: skip tool registration", "name", spec.Name, "err", err)
		}
	}
	isolationMode := strings.TrimSpace(os.Getenv("ARKLOOP_DESKTOP_ISOLATION"))
	useVM := isolationMode == "vm" && desktop.GetSandboxAddr() != ""
	skillLayout := desktopSkillLayoutResolver(useVM)

	// DynamicShellExecutor chooses local or sandbox at runtime; specs are identical, register once
	for _, spec := range localshell.AgentSpecs() {
		if err := toolRegistry.Register(spec); err != nil {
			slog.WarnContext(ctx, "desktop: skip tool registration", "name", spec.Name, "err", err)
		}
	}
	for _, spec := range conversationtool.AgentSpecs() {
		if err := toolRegistry.Register(spec); err != nil {
			slog.WarnContext(ctx, "desktop: skip tool registration", "name", spec.Name, "err", err)
		}
	}

	skillStore, err := openDesktopSkillStore(ctx)
	if err != nil {
		slog.WarnContext(ctx, "desktop: skill store init failed", "err", err.Error())
	}
	executors := builtin.Executors(nil, nil, nil, skillStore)
	for _, name := range []string{
		acptool.AgentSpec.Name,
		acptool.SpawnACPAgentSpec.Name,
	} {
		if exec, ok := executors[name].(acptool.ToolExecutor); ok {
			executors[name] = acptool.DesktopExecutorWithInject(exec, db)
		}
	}

	sandboxAddr := desktop.GetSandboxAddr()
	authToken := strings.TrimSpace(os.Getenv("ARKLOOP_DESKTOP_TOKEN"))
	slog.Debug("composition_desktop", "sandboxAddr", sandboxAddr, "authToken", authToken)
	shellExec := runtime.NewDynamicShellExecutor(sandboxAddr, authToken)

	// 已有持久化或用户已选模式时不覆盖；否则按当前 sandbox 可用性设默认。
	cur := strings.TrimSpace(desktop.GetExecutionMode())
	if cur != "vm" && cur != "local" {
		if sandboxAddr != "" {
			desktop.SetExecutionMode("vm")
			slog.Debug("composition_desktop: sandbox available, setting mode to vm")
		} else {
			desktop.SetExecutionMode("local")
			slog.Debug("composition_desktop: no sandbox, setting mode to local")
		}
	}

	// Bind shell tools to DynamicShellExecutor; local and VM backends share the same protocol.
	executors[localshell.ExecCommandAgentSpec.Name] = shellExec
	executors[localshell.ContinueProcessAgentSpec.Name] = shellExec
	executors[localshell.TerminateProcessAgentSpec.Name] = shellExec
	executors[localshell.ResizeProcessAgentSpec.Name] = shellExec

	var runtimeSnapshot *sharedtoolruntime.RuntimeSnapshot
	if sandboxAddr != "" {
		runtimeSnapshot = &sharedtoolruntime.RuntimeSnapshot{
			SandboxBaseURL:   "http://" + sandboxAddr,
			SandboxAuthToken: authToken,
			ACPHostKind:      "sandbox",
		}
		slog.Info("desktop: shell execution available (local + VM)", "sandbox_addr", sandboxAddr)
	} else {
		runtimeSnapshot = &sharedtoolruntime.RuntimeSnapshot{
			ACPHostKind: "local",
		}
		slog.Info("desktop: shell execution available (local only, sandbox not available)")
	}

	convExec := conversationtool.NewToolExecutor(db, data.MessagesRepository{})
	for _, spec := range conversationtool.AgentSpecs() {
		executors[spec.Name] = convExec
	}

	groupSearchExec := conversationtool.NewGroupSearchExecutor(db, nil)
	if err := toolRegistry.Register(conversationtool.GroupSearchAgentSpec); err != nil {
		return nil, err
	}
	executors[conversationtool.GroupSearchAgentSpec.Name] = groupSearchExec

	memEnabled := strings.TrimSpace(os.Getenv("ARKLOOP_MEMORY_ENABLED")) != "false"
	memoryProviderName := strings.TrimSpace(os.Getenv("ARKLOOP_MEMORY_PROVIDER"))
	ovURL := strings.TrimSpace(os.Getenv("ARKLOOP_OPENVIKING_BASE_URL"))
	ovKey := strings.TrimSpace(os.Getenv("ARKLOOP_OPENVIKING_ROOT_API_KEY"))
	nowledgeCfg := nowledge.LoadConfigFromEnv()

	var notebookProvider memory.MemoryProvider
	var memProvider memory.MemoryProvider
	useOV := false
	if memEnabled {
		notebookProvider = localmemory.NewProvider(db)
		slog.Info("desktop: notebook enabled")
	}
	if memEnabled && memoryProviderName == "nowledge" {
		nowledgeCfg = nowledge.ResolveDesktopConfig(nowledgeCfg)
		memProvider = nowledge.NewProvider(nowledgeCfg)
		if memProvider != nil {
			useOV = true
			desktop.SetMemoryRuntime("nowledge")
			slog.Info("desktop: using Nowledge memory provider", "url", nowledgeCfg.BaseURL)
		}
	} else if memEnabled && ovURL != "" {
		memProvider = openviking.NewProvider(openviking.Config{BaseURL: ovURL, RootAPIKey: ovKey})
		useOV = true
		desktop.SetMemoryRuntime("openviking")
		slog.Info("desktop: using OpenViking memory provider", "url", ovURL)
	} else if memEnabled {
		desktop.SetMemoryRuntime("notebook")
		slog.Info("desktop: using notebook-only memory mode")
	} else {
		desktop.SetMemoryRuntime("")
		slog.Info("desktop: memory disabled")
	}
	if notebookProvider != nil {
		memExec := memorytool.NewToolExecutor(notebookProvider, db, nil)
		notebookSpecs := memorytool.NotebookAgentSpecs()
		for _, spec := range notebookSpecs {
			executors[spec.Name] = memExec
		}
		for _, spec := range notebookSpecs {
			if err := toolRegistry.Register(spec); err != nil {
				slog.WarnContext(ctx, "desktop: skip notebook tool registration", "name", spec.Name, "err", err)
			}
		}
	}

	if useOV && memProvider != nil {
		memExec := memorytool.NewToolExecutor(memProvider, db, nil)
		for _, spec := range memorytool.MemoryAgentSpecs() {
			executors[spec.Name] = memExec
		}
		for _, spec := range memorytool.MemoryAgentSpecs() {
			if err := toolRegistry.Register(spec); err != nil {
				slog.WarnContext(ctx, "desktop: skip memory tool registration", "name", spec.Name, "err", err)
			}
		}
	}

	artifactStore, err := openDesktopArtifactStore(ctx)
	if err != nil {
		slog.WarnContext(ctx, "desktop: artifact store init failed, skipping persisted artifact tools", "err", err.Error())
	}

	var messageAttachmentStore objectstore.Store
	if mas, err := openDesktopMessageAttachmentStore(ctx); err != nil {
		slog.WarnContext(ctx, "desktop: message attachment store init failed", "err", err.Error())
	} else {
		messageAttachmentStore = mas
	}
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
	if rs, err := openDesktopRolloutStore(ctx); err != nil {
		slog.WarnContext(ctx, "desktop: rollout store init failed", "err", err.Error())
	} else {
		rolloutStore = rs
	}

	promptInjection, err := promptinjection.Build(promptinjection.BuilderDeps{
		Store:   sharedconfig.NewPGXStoreQuerier(db),
		AuditDB: db,
	})
	if err != nil {
		return nil, fmt.Errorf("desktop: init prompt injection capability: %w", err)
	}
	hookRegistry := pipeline.NewHookRegistry()
	if notebookProvider != nil {
		if reader, ok := notebookProvider.(interface {
			GetSnapshot(ctx context.Context, accountID, userID uuid.UUID, agentIDStr string) (string, error)
		}); ok {
			hookRegistry.RegisterContextContributor(pipeline.NewNotebookContextContributor(reader))
		}
	}
	desktopImpStore := pipeline.NewDesktopImpressionStore(db)
	for _, exec := range executors {
		if memExec, ok := exec.(interface {
			ConfigureImpression(store pipeline.ImpressionStore, refresh pipeline.ImpressionRefreshFunc, resolver sharedconfig.Resolver)
		}); ok {
			memExec.ConfigureImpression(desktopImpStore, newDesktopImpressionRefresh(db, jobQueue), promptInjection.Resolver)
		}
	}
	hookRegistry.RegisterContextContributor(pipeline.NewImpressionContextContributor(desktopImpStore))
	if typed, ok := memProvider.(*nowledge.Client); ok {
		linkRepo := data.ExternalThreadLinksRepository{}
		linkStore := desktopExternalThreadLinks{repo: linkRepo, db: db}
		hookRegistry.RegisterContextContributor(pipeline.NewNowledgeContextContributor(typed))
		_ = hookRegistry.SetThreadPersistenceProvider(pipeline.NewNowledgeThreadPersistenceProvider(typed, linkStore))
	}
	hookRegistry.RegisterAfterThreadPersistHook(pipeline.NewLegacyMemoryDistillObserver(
		pipeline.NewDesktopMemorySnapshotStore(db),
		db,
		promptInjection.Resolver,
		desktopImpStore,
		newDesktopImpressionRefresh(db, jobQueue),
	))

	// Use localshell specs for LLM; DynamicShellExecutor routes to correct backend at runtime
	shellLlmSpecs := localshell.LlmSpecs()
	allLlmSpecs := append(builtin.LlmSpecs(), shellLlmSpecs...)
	allLlmSpecs = append(allLlmSpecs, conversationtool.LlmSpecs()...)
	allLlmSpecs = append(allLlmSpecs, conversationtool.GroupSearchLlmSpec)
	if notebookProvider != nil {
		allLlmSpecs = append(allLlmSpecs, memorytool.NotebookLlmSpecs()...)
	}
	if useOV && memProvider != nil {
		allLlmSpecs = append(allLlmSpecs, memorytool.MemoryLlmSpecs()...)
	}
	allLlmSpecs, artifactToolsRegistered, err := registerStoredArtifactTools(toolRegistry, executors, allLlmSpecs, artifactStore)
	if err != nil {
		return nil, fmt.Errorf("register desktop artifact tools: %w", err)
	}
	if artifactToolsRegistered {
		slog.InfoContext(ctx, "desktop: stored artifact tools registered", "tools", []string{"create_artifact", "document_write"})
	}

	envSnap, err := sharedtoolruntime.BuildRuntimeSnapshot(ctx, sharedtoolruntime.SnapshotInput{
		HasConversationSearch:  true,
		HasGroupHistorySearch:  true,
		ArtifactStoreAvailable: artifactToolsRegistered,
		ConfigResolver:         nil,
	})
	if err != nil {
		return nil, fmt.Errorf("desktop: env runtime snapshot: %w", err)
	}
	mergedRT := (*runtimeSnapshot).MergeBuiltinToolNamesFrom(envSnap)
	if notebookProvider != nil {
		mergedRT = mergedRT.WithMergedBuiltinToolNames(
			"notebook_read", "notebook_write", "notebook_edit", "notebook_forget",
		)
	}
	if useOV && memProvider != nil {
		if _, ok := memProvider.(*nowledge.Provider); ok {
			mergedRT = mergedRT.WithMergedBuiltinToolNames(
				"memory_search", "memory_read", "memory_write", "memory_forget", "memory_thread_search", "memory_thread_fetch", "memory_connections", "memory_timeline", "memory_context", "memory_status",
			)
		} else {
			mergedRT = mergedRT.WithMergedBuiltinToolNames(
				"memory_search", "memory_read", "memory_write", "memory_edit", "memory_forget",
			)
		}
	}
	runtimeSnapshot = &mergedRT

	baseAllowlist := make(map[string]struct{})
	for _, name := range toolRegistry.ListNames() {
		baseAllowlist[name] = struct{}{}
	}

	// 仅保留有绑定 executor 的工具
	filtered := make(map[string]struct{})
	for name := range baseAllowlist {
		if executors[name] != nil {
			filtered[name] = struct{}{}
		}
	}

	// 尝试从 personas 目录加载
	personaGetter := loadPersonaRegistryFromFS()

	routingLoader := routing.NewDesktopSQLiteRoutingLoader(
		func(ctx context.Context) (routing.ProviderRoutingConfig, error) {
			return loadDesktopRoutingConfig(ctx, db)
		},
		routing.DefaultRoutingConfig(),
	)

	mcpPool := mcp.NewPool()
	mcpDiscoveryCache := mcp.NewDiscoveryCache(30*time.Second, mcpPool)

	if err := cleanupOrphanSkillRuntimes(ctx, db); err != nil {
		slog.WarnContext(ctx, "desktop: orphan skill runtime cleanup failed", "err", err.Error())
	}
	if err := recoverOrphanRuns(ctx, db); err != nil {
		slog.WarnContext(ctx, "desktop: orphan run recovery failed", "err", err.Error())
	}

	return &DesktopEngine{
		db:                     db,
		bus:                    bus,
		auxRouter:              auxRouter,
		auxGateway:             auxGateway,
		emitDebugEvents:        auxCfg.EmitDebugEvents,
		toolRegistry:           toolRegistry,
		toolExecutors:          executors,
		allLlmSpecs:            allLlmSpecs,
		baseAllowlist:          filtered,
		executorRegistry:       execRegistry,
		personaRegistry:        personaGetter,
		notebookProvider:       notebookProvider,
		memProvider:            memProvider,
		useOV:                  useOV,
		useVM:                  useVM,
		skillLayout:            skillLayout,
		runtimeSnapshot:        runtimeSnapshot,
		jobQueue:               jobQueue,
		routingLoader:          routingLoader,
		messageAttachmentStore: messageAttachmentStore,
		rolloutStore:           rolloutStore,
		promptInjection:        promptInjection,
		groupSearchExec:        groupSearchExec,
		mcpPool:                mcpPool,
		mcpDiscoveryCache:      mcpDiscoveryCache,
		shellExecutor:          shellExec,
		hookRuntime:            pipeline.NewHookRuntime(hookRegistry, pipeline.NewDefaultHookResultApplier()),
		hookRegistry:           hookRegistry,
	}, nil
}

type desktopExternalThreadLinks struct {
	repo data.ExternalThreadLinksRepository
	db   data.DesktopDB
}

func (s desktopExternalThreadLinks) Get(ctx context.Context, accountID, threadID uuid.UUID, provider string) (string, bool, error) {
	return s.repo.Get(ctx, s.db, accountID, threadID, provider)
}

func (s desktopExternalThreadLinks) Upsert(ctx context.Context, accountID, threadID uuid.UUID, provider, externalThreadID string) error {
	return s.repo.Upsert(ctx, s.db, accountID, threadID, provider, externalThreadID)
}

func loadPersonaRegistryFromFS() func() *personas.Registry {
	dirs := make([]string, 0, 4)
	if root, err := personas.BuiltinPersonasRoot(); err == nil && strings.TrimSpace(root) != "" {
		dirs = append(dirs, root)
	}
	dirs = append(dirs, "personas", "src/personas", "../personas")
	seen := make(map[string]struct{}, len(dirs))
	var resolvedDir string
	for _, dir := range dirs {
		cleaned := filepath.Clean(strings.TrimSpace(dir))
		if cleaned == "" {
			continue
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		reg, err := personas.LoadRegistry(cleaned)
		if err == nil && len(reg.ListIDs()) > 0 {
			slog.Info("desktop: personas loaded from filesystem", "dir", cleaned, "count", len(reg.ListIDs()))
			resolvedDir = cleaned
			break
		}
	}
	if resolvedDir == "" {
		return nil
	}
	var (
		cached   *personas.Registry
		cachedAt time.Time
		mu       sync.Mutex
	)
	return func() *personas.Registry {
		mu.Lock()
		defer mu.Unlock()
		if cached != nil && time.Since(cachedAt) < 30*time.Second {
			return cached
		}
		reg, err := personas.LoadRegistry(resolvedDir)
		if err != nil {
			slog.Warn("desktop: persona reload failed, using cache", "dir", resolvedDir, "err", err.Error())
			return cached
		}
		cached = reg
		cachedAt = time.Now()
		return cached
	}
}

// Execute runs the agent pipeline for a single run.
func (e *DesktopEngine) Execute(ctx context.Context, run data.Run, traceID string, jobPayload map[string]any) error {
	traceID = strings.TrimSpace(traceID)
	emitter := events.NewEmitter(traceID)

	resolvedRun, err := resolveDesktopRunBindings(ctx, e.db, run)
	if err != nil {
		return fmt.Errorf("resolve environment bindings: %w", err)
	}
	run = resolvedRun

	subAgentsEnabled := desktopSubAgentSchemaAvailable(ctx, e.db)
	if subAgentsEnabled {
		if err := subagentctl.MarkRunning(ctx, e.db, run.ID); err != nil {
			return fmt.Errorf("mark sub_agent running: %w", err)
		}
	}

	runsRepo := data.DesktopRunsRepository{}
	eventsRepo := data.DesktopRunEventsRepository{}
	var tracer pipeline.Tracer
	accountSettingsRepo := data.NewAccountSettingsRepository(e.db)
	if enabled, traceErr := accountSettingsRepo.PipelineTraceEnabled(ctx, run.AccountID); traceErr != nil {
		slog.WarnContext(ctx, "desktop pipeline trace setting load failed", "account_id", run.AccountID.String(), "err", traceErr.Error())
	} else if enabled {
		tracer = pipeline.NewBufTracer(run.ID, run.AccountID, data.NewRunPipelineEventsRepository(e.db))
	}
	var promptCacheDebugEnabled bool
	if debugEnabled, debugErr := accountSettingsRepo.PromptCacheDebugEnabled(ctx, run.AccountID); debugErr != nil {
		slog.WarnContext(ctx, "desktop prompt cache debug setting load failed", "account_id", run.AccountID.String(), "err", debugErr.Error())
	} else {
		promptCacheDebugEnabled = debugEnabled
	}

	runRuntime := *e.runtimeSnapshot
	runRuntime.DesktopExecutionMode = strings.TrimSpace(desktop.GetExecutionMode())

	rc := &pipeline.RunContext{
		Run:                 run,
		DB:                  e.db,
		RunStatusDB:         runsRepo,
		Pool:                nil,
		MemoryServiceDB:     e.db,
		MemorySnapshotStore: pipeline.NewDesktopMemorySnapshotStore(e.db),
		EventBus:            e.bus,
		TraceID:             traceID,
		Tracer:              tracer,
		Emitter:             emitter,
		Router:              e.auxRouter,
		Runtime:             &runRuntime,
		HookRuntime:         e.hookRuntime,
		HookRegistry:        e.hookRegistry,

		ExecutorBuilder:     e.executorRegistry,
		ToolBudget:          map[string]any{},
		PerToolSoftLimits:   tools.DefaultPerToolSoftLimits(),
		PendingMemoryWrites: memory.NewPendingWriteBuffer(),

		LlmRetryMaxAttempts: 10,
		LlmRetryBaseDelayMs: 1000,

		PromptCacheDebugEnabled: promptCacheDebugEnabled,

		ThreadMessageHistoryLimit:     0,
		AgentReasoningIterationsLimit: 0,
		ToolContinuationBudgetLimit:   32,
		MaxParallelTasks:              4,
		RunWallClockTimeout:           15 * time.Minute,
		PausedInputTimeout:            5 * time.Minute,
		IdleHeartbeatInterval:         15 * time.Second,
		CreditPerUSD:                  1000,
		LlmMaxResponseBytes:           16384,

		UserID:       run.CreatedByUserID,
		ProfileRef:   derefStr(run.ProfileRef),
		WorkspaceRef: derefStr(run.WorkspaceRef),
		JobPayload:   cloneDesktopMap(jobPayload),
	}
	if e.rolloutStore != nil {
		recorder := rollout.NewRecorder(e.rolloutStore, run.ID)
		recorder.Start(ctx)
		rc.RolloutRecorder = recorder
		rc.ResponseDraftStore = e.rolloutStore
		defer recorder.Close(context.Background())
	}
	if !e.useVM {
		defer func() {
			if err := cleanupDesktopSkillRuntime(run.ID); err != nil {
				slog.WarnContext(ctx, "desktop: cleanup skill runtime failed", "run_id", run.ID.String(), "err", err.Error())
			}
		}()
	}

	if e.jobQueue != nil && subAgentsEnabled {
		rc.SubAgentControl = subagentctl.NewService(e.db, nil, e.jobQueue, run, traceID, subagentctl.SubAgentLimits{}, subagentctl.BackpressureConfig{}, e.rolloutStore)
	}
	defer pipeline.FlushTracer(rc.Tracer)

	// pipeline 限制规范化
	limits := sharedexec.NormalizePlatformLimits(sharedexec.PlatformLimits{
		AgentReasoningIterations: rc.AgentReasoningIterationsLimit,
		ToolContinuationBudget:   rc.ToolContinuationBudgetLimit,
	})
	rc.AgentReasoningIterationsLimit = limits.AgentReasoningIterations
	rc.ToolContinuationBudgetLimit = limits.ToolContinuationBudget
	rc.ReasoningIterations = limits.AgentReasoningIterations
	rc.ToolContinuationBudget = limits.ToolContinuationBudget

	cc, err := resolveDesktopContextCompact(ctx, e.db)
	if err != nil {
		return err
	}
	rc.ContextCompact = cc

	if e.useOV && e.memProvider != nil {
		rc.MemoryProvider = e.memProvider
	}

	var memMiddleware pipeline.RunMiddleware
	impStore := pipeline.NewDesktopImpressionStore(e.db)
	impRefresh := newDesktopImpressionRefresh(e.db, e.jobQueue)
	if e.useOV {
		memoryMW := pipeline.NewMemoryMiddleware(
			e.memProvider,
			pipeline.NewDesktopMemorySnapshotStore(e.db),
			e.db,
			e.promptInjection.Resolver,
			impStore,
			impRefresh,
		)
		memMiddleware = memoryMW
	} else {
		memMiddleware = func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
			return next(ctx, rc)
		}
	}
	promptHookMW := pipeline.NewPromptHookMiddleware()
	baseMemMiddleware := memMiddleware
	memMiddleware = traceDesktopMemoryInjection(func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		return promptHookMW(ctx, rc, func(ctx context.Context, rc *pipeline.RunContext) error {
			return baseMemMiddleware(ctx, rc, next)
		})
	})

	middlewares := []pipeline.RunMiddleware{
		desktopCancelGuard(e.db, e.bus),
		desktopObservedStage("input_loader", eventsRepo, desktopInputLoader(e.db, runsRepo, eventsRepo, e.messageAttachmentStore, e.rolloutStore)),
		desktopObservedStage("heartbeat_schedule", eventsRepo, pipeline.NewHeartbeatScheduleMiddleware(e.db)),
		pipeline.NewMCPDiscoveryMiddleware(
			e.mcpDiscoveryCache,
			func(*pipeline.RunContext) mcp.DiscoveryQueryer { return e.db },
			data.DesktopRunEventsRepository{},
			e.toolExecutors,
			e.allLlmSpecs,
			e.baseAllowlist,
			e.toolRegistry,
		),
		desktopObservedStage("tool_provider_bindings", eventsRepo, desktopToolProviderBindings(e.db)),
		desktopObservedStage("spawn_agent", eventsRepo, pipeline.NewSpawnAgentMiddleware()),
		desktopObservedStage("persona_resolution", eventsRepo, desktopPersonaResolution(e.db, e.personaRegistry, runsRepo, eventsRepo)),
		desktopObservedStage("channel_context", eventsRepo, desktopChannelContext(e.db)),
		desktopObservedStage("channel_admin_tag", eventsRepo, pipeline.NewChannelAdminTagMiddleware(e.db)),
		desktopObservedStage("channel_group_user_merge", eventsRepo, pipeline.NewChannelTelegramGroupUserMergeMiddleware()),
		desktopObservedStage("channel_telegram_tools", eventsRepo, pipeline.NewChannelTelegramToolsMiddleware(nil, nil, pipeline.ChannelTelegramToolsDeps{
			TokenLoader:        &desktopTelegramTokenLoader{db: e.db},
			GroupSearchExec:    e.groupSearchExec,
			GroupSearchLlmSpec: conversationtool.GroupSearchLlmSpec,
		})),
		desktopObservedStage("channel_qq_tools", eventsRepo, pipeline.NewChannelQQToolsMiddleware(pipeline.ChannelQQToolsDeps{
			ConfigLoader:    &desktopQQOneBotConfigLoader{db: e.db},
			GroupSearchExec: e.groupSearchExec,
			GroupSearchSpec: conversationtool.GroupSearchLlmSpec,
		})),
		desktopObservedStage("channel_end_reply", eventsRepo, pipeline.NewChannelEndReplyMiddleware()),
		pipeline.NewScheduledJobPrepareMiddleware(),
		desktopObservedStage("sub_agent_context", eventsRepo, desktopSubAgentContext(e.db, subagentctl.NewSnapshotStorage())),
		desktopObservedStage("skill_context", eventsRepo, pipeline.NewSkillContextMiddleware(pipeline.SkillContextConfig{
			Resolve:        desktopSkillResolver(e.db),
			Prepare:        desktopSkillPreparer(e.useVM),
			LayoutResolver: e.skillLayout,
			ExternalDirs:   desktopExternalSkillDirs(e.db),
		})),
	}
	middlewares = append(middlewares, desktopCapabilityMiddlewares(memMiddleware, e.promptInjection, eventsRepo)...)
	middlewares = append(middlewares,
		desktopRouting(e.auxRouter, e.auxGateway, e.emitDebugEvents, e.db, runsRepo, eventsRepo),
		desktopObservedStage("channel_group_context_trim", eventsRepo, pipeline.NewChannelGroupContextTrimMiddleware(pipeline.GroupContextTrimDeps{
			Pool:            e.db,
			MessagesRepo:    data.MessagesRepository{},
			EventsRepo:      data.DesktopRunEventsRepository{},
			EmitDebugEvents: e.emitDebugEvents,
		})),
		pipeline.NewTitleSummarizerMiddleware(e.db, nil, e.auxGateway, e.emitDebugEvents, e.routingLoader),
		pipeline.NewContextCompactMiddleware(e.db, data.MessagesRepository{}, data.DesktopRunEventsRepository{}, e.auxGateway, e.emitDebugEvents, e.routingLoader),
		pipeline.NewImpressionPrepareMiddleware(impStore, e.db, e.auxGateway, e.emitDebugEvents, e.routingLoader),
		pipeline.NewHeartbeatPrepareMiddleware(),
		pipeline.NewConditionalToolsMiddleware(),
		pipeline.NewToolBuildMiddleware(),
		pipeline.NewResultSummarizerMiddleware(nil, e.auxGateway, e.emitDebugEvents, 0, e.routingLoader),
		pipeline.NewThreadPersistHookMiddleware(),
		desktopChannelDelivery(e.db),
	)
	terminal := desktopAgentLoop(e.db, e.bus, e.jobQueue, runsRepo, eventsRepo, e.shellExecutor, e.runtimeSnapshot)
	handler := pipeline.Build(middlewares, terminal)

	return handler(ctx, rc)
}

func traceDesktopMemoryInjection(inner pipeline.RunMiddleware) pipeline.RunMiddleware {
	if inner == nil {
		return nil
	}
	return func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		before := rc.MaterializedSystemPrompt()
		err := inner(ctx, rc, next)
		if rc != nil && rc.Tracer != nil {
			delta := rc.MaterializedSystemPrompt()
			if strings.HasPrefix(delta, before) {
				delta = delta[len(before):]
			}
			rc.Tracer.Event("memory_injection", "memory_injection.completed", map[string]any{
				"memory_injected":   strings.Contains(delta, "<memory>"),
				"notebook_injected": strings.Contains(delta, "<notebook>"),
				"injection_len":     len(delta),
			})
		}
		return err
	}
}

func desktopCapabilityMiddlewares(
	memMiddleware pipeline.RunMiddleware,
	promptInjection securitycap.Runtime,
	eventsRepo data.RunEventStore,
) []pipeline.RunMiddleware {
	middlewares := []pipeline.RunMiddleware{
		desktopObservedStage("memory_injection", eventsRepo, memMiddleware),
		desktopObservedStage("runtime_context", eventsRepo, pipeline.NewRuntimeContextMiddleware()),
	}
	return append(middlewares, promptInjection.Middlewares(eventsRepo)...)
}

func desktopObservedStage(stage string, eventsRepo data.RunEventStore, inner pipeline.RunMiddleware) pipeline.RunMiddleware {
	if inner == nil {
		return nil
	}
	stage = strings.TrimSpace(stage)
	if stage == "" {
		return inner
	}
	eventType, ok := desktopObservedEventNames[stage]
	if !ok {
		return inner
	}
	return func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		startedAt := time.Now()
		emitted := false
		emit := func(status string, stageErr error) {
			if emitted {
				return
			}
			emitted = true
			emitDesktopStageEvent(ctx, rc, eventsRepo, eventType, time.Since(startedAt).Milliseconds(), status, stageErr)
		}
		err := inner(ctx, rc, func(ctx context.Context, rc *pipeline.RunContext) error {
			emit("completed", nil)
			return next(ctx, rc)
		})
		if !emitted {
			status := "completed"
			if err != nil {
				status = "failed"
			}
			emit(status, err)
		}
		return err
	}
}

func emitDesktopStageEvent(
	ctx context.Context,
	rc *pipeline.RunContext,
	eventsRepo data.RunEventStore,
	eventType string,
	durationMs int64,
	status string,
	stageErr error,
) {
	if rc == nil || eventsRepo == nil {
		return
	}
	thresholdMs := loadDesktopStageEventMs()
	if stageErr == nil && durationMs < thresholdMs {
		return
	}
	db := rc.DB
	if db == nil {
		return
	}
	dataJSON := buildDesktopStageEventData(durationMs, thresholdMs, status, stageErr)
	ev := rc.Emitter.Emit(eventType, dataJSON, nil, nil)
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		slog.WarnContext(ctx, "desktop stage event tx begin failed", "event_type", eventType, "error", err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := eventsRepo.AppendRunEvent(ctx, tx, rc.Run.ID, ev); err != nil {
		slog.WarnContext(ctx, "desktop stage event append failed", "event_type", eventType, "error", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		slog.WarnContext(ctx, "desktop stage event commit failed", "event_type", eventType, "error", err)
		return
	}
	if rc.EventBus != nil {
		_ = rc.EventBus.Publish(ctx, fmt.Sprintf("run_events:%s", rc.Run.ID.String()), "")
	}
}

func buildDesktopStageEventData(durationMs int64, thresholdMs int64, status string, stageErr error) map[string]any {
	payload := map[string]any{
		"duration_ms":  durationMs,
		"threshold_ms": thresholdMs,
		"status":       strings.TrimSpace(status),
	}
	if stageErr != nil {
		payload["error_message"] = trimDesktopStageError(stageErr.Error())
	}
	return payload
}

func loadDesktopStageEventMs() int64 {
	raw := strings.TrimSpace(os.Getenv("ARKLOOP_DESKTOP_STAGE_EVENT_MS"))
	if raw == "" {
		return defaultDesktopStageEventMs
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return defaultDesktopStageEventMs
	}
	return value
}

func trimDesktopStageError(message string) string {
	message = strings.TrimSpace(message)
	if len(message) <= 200 {
		return message
	}
	return message[:200]
}

func desktopObservedEventName(stage string) (string, bool) {
	stage = strings.TrimSpace(stage)
	if stage == "" {
		return "", false
	}
	eventType, ok := desktopObservedEventNames[stage]
	return eventType, ok
}

func desktopObservedEventTypes() []string {
	items := make([]string, 0, len(desktopObservedEventNames))
	for _, eventType := range desktopObservedEventNames {
		items = append(items, eventType)
	}
	slices.Sort(items)
	return items
}

func resolveDesktopRunBindings(ctx context.Context, db data.DesktopDB, run data.Run) (data.Run, error) {
	if db == nil {
		return run, fmt.Errorf("desktop db must not be nil")
	}
	return environmentbindings.ResolveAndPersistRun(ctx, db, run)
}

// --------------- desktop middleware ---------------

func newDesktopImpressionRefresh(db data.DesktopDB, jq queue.JobQueue) pipeline.ImpressionRefreshFunc {
	if db == nil || jq == nil {
		return nil
	}
	return pipeline.NewImpressionRefreshFunc(pipeline.ImpressionRefreshDeps{
		ExecSQL: func(ctx context.Context, sql string, args ...any) error {
			_, err := db.Exec(ctx, sql, args...)
			return err
		},
		QueryRowScan: func(ctx context.Context, sql string, args []any, dest ...any) error {
			return db.QueryRow(ctx, sql, args...).Scan(dest...)
		},
		EnqueueRun: func(ctx context.Context, accountID, runID uuid.UUID, traceID, jobType string, payload map[string]any) error {
			_, err := jq.EnqueueRun(ctx, accountID, runID, traceID, jobType, payload, nil)
			return err
		},
	})
}

// desktopMemoryInjection reads the saved notebook block from
// user_notebook_snapshots and appends it to the run's system prompt.
func desktopMemoryInjection(db data.DesktopDB) pipeline.RunMiddleware {
	return func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		if rc.UserID == nil || db == nil {
			return next(ctx, rc)
		}
		rc.RemovePromptSegment("memory.notebook_snapshot")
		provider := localmemory.NewProvider(db)
		block, err := provider.GetSnapshot(ctx, rc.Run.AccountID, *rc.UserID, pipeline.StableAgentID(rc))
		if err == nil && strings.TrimSpace(block) != "" {
			rc.UpsertPromptSegment(pipeline.PromptSegment{
				Name:          "memory.notebook_snapshot",
				Target:        pipeline.PromptTargetSystemPrefix,
				Role:          "system",
				Text:          strings.TrimSpace(block),
				Stability:     pipeline.PromptStabilitySessionPrefix,
				CacheEligible: true,
			})
		}
		// Ignore ErrNoRows / any DB errors — no memory is a valid state.
		return next(ctx, rc)
	}
}

func desktopToolProviderBindings(db data.DesktopDB) pipeline.RunMiddleware {
	return func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		if db == nil || rc == nil {
			return next(ctx, rc)
		}
		platformCfgs, err := toolprovider.LoadDesktopActiveToolProviders(ctx, db)
		if err != nil {
			slog.WarnContext(ctx, "desktop: failed to load tool providers, skipping", "err", err)
			return next(ctx, rc)
		}
		if len(platformCfgs) == 0 {
			return next(ctx, rc)
		}
		if rc.ActiveToolProviderByGroup == nil {
			rc.ActiveToolProviderByGroup = map[string]string{}
		}
		if rc.ActiveToolProviderConfigsByGroup == nil {
			rc.ActiveToolProviderConfigsByGroup = map[string]sharedtoolruntime.ProviderConfig{}
		}
		apply := func(cfg toolprovider.ActiveProviderConfig) {
			g := strings.TrimSpace(cfg.GroupName)
			pn := strings.TrimSpace(cfg.ProviderName)
			if g == "" || pn == "" {
				return
			}
			exec := pipeline.BuildProviderExecutor(cfg)
			rc.ActiveToolProviderByGroup[g] = pn
			rc.ActiveToolProviderConfigsByGroup[g] = toolprovider.ToRuntimeProviderConfig(cfg)
			if exec != nil {
				rc.ToolExecutors[pn] = exec
			}
		}
		for _, cfg := range platformCfgs {
			apply(cfg)
		}
		return next(ctx, rc)
	}
}

// desktopCancelGuard provides Desktop wait/poll hooks using SQLite run_events.
func desktopCancelGuard(db data.DesktopDB, bus eventbus.EventBus) pipeline.RunMiddleware {
	return func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		execCtx, cancel := context.WithCancel(ctx)
		rc.CancelFunc = cancel

		done := make(chan struct{})
		wakeInput := make(chan struct{}, 1)
		var sub eventbus.Subscription
		if bus != nil && rc != nil && rc.Run.ID != uuid.Nil {
			if subscribed, err := bus.Subscribe(execCtx, fmt.Sprintf("run_events:%s", rc.Run.ID.String())); err == nil {
				sub = subscribed
			}
		}
		go func() {
			defer close(done)
			if sub == nil {
				<-execCtx.Done()
				return
			}
			defer sub.Close()
			for {
				select {
				case <-execCtx.Done():
					return
				case _, ok := <-sub.Channel():
					if !ok {
						return
					}
					select {
					case wakeInput <- struct{}{}:
					default:
					}
				}
			}
		}()
		rc.ListenDone = done

		var mu sync.Mutex
		var lastSeq int64
		loadNextInput := func(ctx context.Context) (string, bool) {
			if db == nil || rc == nil || rc.Run.ID == uuid.Nil {
				return "", false
			}
			mu.Lock()
			sinceSeq := lastSeq
			mu.Unlock()
			content, seq, ok := fetchLatestDesktopInput(ctx, db, rc.Run.ID, sinceSeq)
			if !ok {
				return "", false
			}
			mu.Lock()
			if seq > lastSeq {
				lastSeq = seq
			}
			mu.Unlock()
			return content, true
		}
		rc.WaitForInput = func(ctx context.Context) (string, bool) {
			for {
				if content, ok := loadNextInput(ctx); ok {
					return content, true
				}
				timer := time.NewTimer(250 * time.Millisecond)
				select {
				case <-ctx.Done():
					stopDesktopTimer(timer)
					return "", false
				case <-wakeInput:
					stopDesktopTimer(timer)
				case <-timer.C:
				}
			}
		}
		rc.PollSteeringInput = func(ctx context.Context) (string, bool) {
			return loadNextInput(ctx)
		}
		defer func() {
			cancel()
			<-done
		}()
		return next(execCtx, rc)
	}
}

func fetchLatestDesktopInput(ctx context.Context, db data.DesktopDB, runID uuid.UUID, sinceSeq int64) (string, int64, bool) {
	if db == nil || runID == uuid.Nil {
		return "", 0, false
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return "", 0, false
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	var rawJSON []byte
	var seq int64
	err = tx.QueryRow(
		ctx,
		`SELECT data_json, seq
			 FROM run_events
			 WHERE run_id = $1
		   AND type = $2
		   AND seq > $3
		 ORDER BY seq ASC
		 LIMIT 1`,
		runID,
		pipeline.EventTypeInputProvided,
		sinceSeq,
	).Scan(&rawJSON, &seq)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", 0, false
		}
		return "", 0, false
	}

	var payload map[string]any
	if err := json.Unmarshal(rawJSON, &payload); err != nil {
		return "", 0, false
	}
	content, _ := payload["content"].(string)
	return content, seq, true
}

func stopDesktopTimer(timer *time.Timer) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

// desktopInputLoader loads run input and thread messages from SQLite.
func desktopInputLoader(
	db data.DesktopDB,
	runsRepo data.DesktopRunsRepository,
	eventsRepo data.DesktopRunEventsRepository,
	attachmentStore pipeline.MessageAttachmentStore,
	rolloutStore objectstore.BlobStore,
) pipeline.RunMiddleware {
	return func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		loaded, err := pipeline.LoadRunInputs(ctx, db, rc.Run, rc.JobPayload, runsRepo, eventsRepo, data.MessagesRepository{}, attachmentStore, rolloutStore, rc.ThreadMessageHistoryLimit)
		if err != nil {
			if pipeline.IsResumeUnavailableError(err) {
				if pipeline.IsRuntimeRecoveryJob(rc.JobPayload) {
					return desktopWriteTerminalEvent(ctx, db, rc.Run, rc.Emitter, data.DesktopRunsRepository{}, data.DesktopRunEventsRepository{},
						"run.interrupted", "worker.recovery_unavailable", "runtime recovery state is unavailable", nil)
				}
				return desktopWriteFailure(ctx, db, rc.Run, rc.Emitter, data.DesktopRunsRepository{}, data.DesktopRunEventsRepository{}, pipeline.ResumeUnavailableErrorClass, "resume context is unavailable", nil)
			}
			return err
		}
		rc.InputJSON = loaded.InputJSON
		if wd, ok := loaded.InputJSON["work_dir"].(string); ok && strings.TrimSpace(wd) != "" {
			rc.WorkDir = strings.TrimSpace(wd)
		}
		rc.Messages = loaded.Messages
		rc.ThreadMessageIDs = loaded.ThreadMessageIDs
		rc.ThreadContextFrontier = append([]pipeline.FrontierNode(nil), loaded.ThreadContextFrontier...)
		if rc.Tracer != nil {
			rc.Tracer.Event("input_loader", "input_loader.loaded", map[string]any{
				"run_kind":      strings.TrimSpace(desktopStringValue(loaded.InputJSON["run_kind"])),
				"message_count": len(rc.Messages),
				"history_limit": rc.ThreadMessageHistoryLimit,
			})
		}

		return next(ctx, rc)
	}
}

func desktopStringValue(value any) string {
	if raw, ok := value.(string); ok {
		return raw
	}
	return ""
}

// desktopToolInit sets tool specs, executors, allowlist and registry on RunContext
// (replaces MCPDiscoveryMiddleware for desktop).
func desktopToolInit(
	executors map[string]tools.Executor,
	llmSpecs []llm.ToolSpec,
	allowlist map[string]struct{},
	registry *tools.Registry,
) pipeline.RunMiddleware {
	return func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		rc.ToolExecutors = pipeline.CopyToolExecutors(executors)
		rc.ToolSpecs = append([]llm.ToolSpec{}, llmSpecs...)
		rc.AllowlistSet = pipeline.CopyStringSet(allowlist)
		rc.ToolRegistry = registry
		return next(ctx, rc)
	}
}

type desktopChannelIdentityRecord struct {
	UserID *uuid.UUID
}

type desktopDeliveryChannelRecord struct {
	ChannelType string
	Token       string
	ConfigJSON  []byte
}

func desktopChannelContext(db data.DesktopDB) pipeline.RunMiddleware {
	return func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		if rc == nil {
			return next(ctx, rc)
		}
		rawDelivery, ok := rc.JobPayload["channel_delivery"].(map[string]any)
		if !ok || len(rawDelivery) == 0 {
			rawDelivery, ok = rc.InputJSON["channel_delivery"].(map[string]any)
		}
		if !ok || len(rawDelivery) == 0 {
			return next(ctx, rc)
		}
		channelCtx, err := pipeline.ParseChannelContextPayload(rawDelivery)
		if err != nil {
			return err
		}
		if db != nil && channelCtx.SenderChannelIdentityID != uuid.Nil {
			identity, err := loadDesktopChannelIdentity(ctx, db, channelCtx.SenderChannelIdentityID)
			if err != nil {
				return err
			}
			if identity != nil {
				channelCtx.SenderUserID = identity.UserID
			}
		}
		// channel 场景下 bot 的 memory 归属于 channel owner
		if db != nil && channelCtx.SenderUserID == nil && channelCtx.ChannelID != uuid.Nil {
			ownerID, err := loadDesktopChannelOwner(ctx, db, channelCtx.ChannelID)
			if err != nil {
				return err
			}
			channelCtx.SenderUserID = ownerID
		}
		if db != nil && channelCtx.ChannelID != uuid.Nil && channelCtx.ChannelType == "telegram" {
			configJSON, err := loadDesktopChannelConfigJSON(ctx, db, channelCtx.ChannelID)
			if err == nil && len(configJSON) > 0 {
				ux := pipeline.ParseTelegramChannelUX(configJSON)
				channelCtx.BotDisplayName = ux.BotFirstName
				channelCtx.BotUsername = ux.BotUsername
			}
		}
		rc.ChannelContext = channelCtx
		rc.ChannelToolSurface = pipeline.NewChannelToolSurfaceFromContext(channelCtx)
		if channelCtx.SenderUserID != nil {
			rc.UserID = channelCtx.SenderUserID
		}
		return next(ctx, rc)
	}
}

func desktopOutboxThreadPtr(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func desktopChannelDelivery(db data.DesktopDB) pipeline.RunMiddleware {
	client := telegrambot.NewClient(os.Getenv("ARKLOOP_TELEGRAM_BOT_API_BASE_URL"), nil)
	discordClient := &http.Client{Timeout: 10 * time.Second}

	return func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		var preloaded *desktopDeliveryChannelRecord
		var qqChannel *desktopQQDeliveryChannelRecord
		var ux pipeline.TelegramChannelUX
		channelType := desktopNormalizedChannelType(rc)
		if db != nil && rc != nil && rc.ChannelContext != nil && (channelType == "telegram" || channelType == "discord") {
			ch, prefetchErr := loadDesktopDeliveryChannel(ctx, db, rc.ChannelContext.ChannelID)
			if prefetchErr != nil {
				slog.WarnContext(ctx, "desktop channel delivery prefetch failed", "run_id", rc.Run.ID, "err", prefetchErr.Error())
			} else if ch != nil {
				preloaded = ch
				if channelType == "telegram" {
					ux = pipeline.ParseTelegramChannelUX(ch.ConfigJSON)
				}
			}
		}
		if db != nil && rc != nil && rc.ChannelContext != nil && channelType == "qq" {
			ch, prefetchErr := loadDesktopQQDeliveryChannel(ctx, db, rc.ChannelContext.ChannelID)
			if prefetchErr != nil {
				slog.WarnContext(ctx, "desktop qq channel delivery prefetch failed", "run_id", rc.Run.ID, "err", prefetchErr.Error())
			} else {
				qqChannel = ch
			}
		}

		streamMidCount := 0
		var streamFlush func(context.Context, string) error
		if preloaded != nil && db != nil && rc != nil && rc.ChannelContext != nil && channelType == "telegram" &&
			!rc.HeartbeatRun &&
			strings.TrimSpace(preloaded.Token) != "" {
			sender := pipeline.NewTelegramChannelSenderWithClient(client, preloaded.Token, 50*time.Millisecond)
			streamFlush = func(ctx2 context.Context, text string) error {
				replyTo := desktopTelegramReplyReference(rc)
				ids, sendErr := sender.SendText(ctx2, pipeline.ChannelDeliveryTarget{
					ChannelType:  rc.ChannelContext.ChannelType,
					Conversation: rc.ChannelContext.Conversation,
					ReplyTo:      replyTo,
				}, text)
				if sendErr != nil {
					return sendErr
				}
				if err := recordDesktopChannelDelivery(
					ctx2,
					db,
					rc.Run.ID,
					rc.Run.ThreadID,
					rc.ChannelContext.ChannelID,
					rc.ChannelContext.ChannelType,
					rc.ChannelContext.Conversation.Target,
					replyTo,
					rc.ChannelContext.Conversation.ThreadID,
					ids,
				); err != nil {
					return err
				}
				if err := persistDesktopStreamChunkMessage(ctx2, db, rc.Run, text); err != nil {
					slog.WarnContext(ctx2, "desktop: persist stream chunk message failed", "run_id", rc.Run.ID, "err", err.Error())
				}
				streamMidCount++
				return nil
			}
			rc.TelegramToolBoundaryFlush = streamFlush

			if pipeline.ShouldShowTelegramProgress(rc) {
				tracker := pipeline.NewTelegramProgressTracker(client, preloaded.Token, pipeline.ChannelDeliveryTarget{
					ChannelType:  rc.ChannelContext.ChannelType,
					Conversation: rc.ChannelContext.Conversation,
				}, desktopTelegramReplyReference(rc))
				rc.TelegramProgressTracker = tracker
			}
		}

		var stopTyping context.CancelFunc
		if preloaded != nil && ux.TypingIndicator && strings.TrimSpace(preloaded.Token) != "" && !pipeline.IsHeartbeatRunContext(rc) {
			stopTyping = pipeline.StartTelegramTypingRefresh(ctx, client, preloaded.Token, rc.ChannelContext.Conversation.Target)
		}

		err := next(ctx, rc)
		if rc != nil {
			rc.TelegramToolBoundaryFlush = nil
			if rc.TelegramProgressTracker != nil {
				rc.TelegramProgressTracker.Finalize(ctx)
			}
		}
		if stopTyping != nil {
			stopTyping()
		}

		if err != nil || rc == nil || rc.ChannelContext == nil {
			return err
		}
		channelType = desktopNormalizedChannelType(rc)
		if db == nil || (channelType != "telegram" && channelType != "discord" && channelType != "qq") {
			return err
		}
		if rc.ChannelOutputDelivered {
			return err
		}
		finalOutput := strings.TrimSpace(rc.FinalAssistantOutput)
		finalOutputs := pipelineNormalizedAssistantOutputs(rc.FinalAssistantOutputs, finalOutput)
		if pipeline.ShouldSuppressHeartbeatOutput(rc, finalOutput) {
			return err
		}

		fullOut := finalOutput
		remainder := strings.TrimSpace(rc.TelegramStreamDeliveryRemainder)
		notice := strings.TrimSpace(rc.ChannelTerminalNotice)
		if fullOut == "" && remainder == "" && streamMidCount == 0 && notice == "" {
			return err
		}

		output := fullOut
		if streamFlush != nil {
			if remainder != "" {
				output = remainder
			} else if streamMidCount > 0 {
				output = ""
			} else {
				output = fullOut
			}
		}
		if strings.TrimSpace(output) == "" && notice != "" {
			output = notice
		}
		if streamFlush != nil && streamMidCount > 0 {
			finalOutputs = nil
		}

		channel := preloaded
		var lookupErr error
		if channel == nil && channelType != "qq" {
			channel, lookupErr = loadDesktopDeliveryChannel(ctx, db, rc.ChannelContext.ChannelID)
		}
		if lookupErr != nil {
			recordDesktopChannelDeliveryFailure(db, rc.Run.ID, lookupErr)
			slog.WarnContext(ctx, "desktop channel delivery lookup failed", "run_id", rc.Run.ID, "err", lookupErr.Error())
			return err
		}
		if channel == nil && channelType != "qq" {
			recordDesktopChannelDeliveryFailure(db, rc.Run.ID, fmt.Errorf("channel not found or inactive"))
			return err
		}
		outboxRepo := data.ChannelDeliveryOutboxRepository{}
		switch channelType {
		case "telegram":
			uxSend := pipeline.ParseTelegramChannelUX(channel.ConfigJSON)
			payload := data.OutboxPayload{
				Outputs:          finalOutputs,
				PlatformChatID:   rc.ChannelContext.Conversation.Target,
				ReplyToMessageID: desktopTelegramReplyReferenceMessageID(rc),
				PlatformThreadID: rc.ChannelContext.Conversation.ThreadID,
				ConversationType: rc.ChannelContext.ConversationType,
			}
			tx, txErr := db.BeginTx(ctx, pgx.TxOptions{})
			if txErr != nil {
				recordDesktopChannelDeliveryFailure(db, rc.Run.ID, txErr)
				slog.WarnContext(ctx, "desktop telegram outbox tx begin failed", "run_id", rc.Run.ID, "err", txErr.Error())
				return err
			}
			outboxRec, insertErr := outboxRepo.InsertPending(ctx, tx, rc.Run.ID, rc.ChannelContext.ChannelID, desktopOutboxThreadPtr(rc.Run.ThreadID), channelType, data.OutboxKindMessage, payload)
			if insertErr != nil {
				_ = tx.Rollback(ctx)
				recordDesktopChannelDeliveryFailure(db, rc.Run.ID, insertErr)
				slog.WarnContext(ctx, "desktop telegram outbox insert failed", "run_id", rc.Run.ID, "err", insertErr.Error())
				return err
			}
			if cmtErr := tx.Commit(ctx); cmtErr != nil {
				recordDesktopChannelDeliveryFailure(db, rc.Run.ID, cmtErr)
				slog.WarnContext(ctx, "desktop telegram outbox tx commit failed", "run_id", rc.Run.ID, "err", cmtErr.Error())
				return err
			}
			if tryErr := tryDeliverDesktopTelegramOutbox(ctx, db, rc, client, channel, outboxRec, payload, outboxRepo); tryErr != nil {
				return err
			}
			if strings.TrimSpace(uxSend.ReactionEmoji) != "" {
				pipeline.MaybeTelegramInboundReaction(ctx, client, channel.Token, rc, uxSend.ReactionEmoji)
			}
		case "discord":
			payload := data.OutboxPayload{
				Outputs:          []string{output},
				PlatformChatID:   rc.ChannelContext.Conversation.Target,
				ReplyToMessageID: desktopDiscordReplyReferenceMessageID(rc),
				PlatformThreadID: rc.ChannelContext.Conversation.ThreadID,
				ConversationType: rc.ChannelContext.ConversationType,
			}
			tx, txErr := db.BeginTx(ctx, pgx.TxOptions{})
			if txErr != nil {
				recordDesktopChannelDeliveryFailure(db, rc.Run.ID, txErr)
				slog.WarnContext(ctx, "desktop discord outbox tx begin failed", "run_id", rc.Run.ID, "err", txErr.Error())
				return err
			}
			threadID := rc.Run.ThreadID
			outboxRec, insertErr := outboxRepo.InsertPending(ctx, tx, rc.Run.ID, rc.ChannelContext.ChannelID, desktopOutboxThreadPtr(threadID), channelType, data.OutboxKindMessage, payload)
			if insertErr != nil {
				_ = tx.Rollback(ctx)
				recordDesktopChannelDeliveryFailure(db, rc.Run.ID, insertErr)
				slog.WarnContext(ctx, "desktop discord outbox insert failed", "run_id", rc.Run.ID, "err", insertErr.Error())
				return err
			}
			if cmtErr := tx.Commit(ctx); cmtErr != nil {
				recordDesktopChannelDeliveryFailure(db, rc.Run.ID, cmtErr)
				slog.WarnContext(ctx, "desktop discord outbox tx commit failed", "run_id", rc.Run.ID, "err", cmtErr.Error())
				return err
			}
			if tryErr := tryDeliverDesktopDiscordOutbox(ctx, db, rc, discordClient, channel, outboxRec, payload, outboxRepo); tryErr != nil {
				return err
			}
		case "qq":
			qCh := qqChannel
			if qCh == nil {
				var lookupErr error
				qCh, lookupErr = loadDesktopQQDeliveryChannel(ctx, db, rc.ChannelContext.ChannelID)
				if lookupErr != nil {
					recordDesktopChannelDeliveryFailure(db, rc.Run.ID, lookupErr)
					slog.WarnContext(ctx, "desktop qq channel delivery lookup failed", "run_id", rc.Run.ID, "err", lookupErr.Error())
					return err
				}
			}
			if qCh == nil {
				recordDesktopChannelDeliveryFailure(db, rc.Run.ID, fmt.Errorf("qq channel config not found"))
				return err
			}
			metadata := map[string]any{}
			if rc.ChannelContext.ConversationType == "group" {
				metadata["message_type"] = "group"
			}
			payload := data.OutboxPayload{
				Outputs:          []string{output},
				PlatformChatID:   rc.ChannelContext.Conversation.Target,
				PlatformThreadID: rc.ChannelContext.Conversation.ThreadID,
				ConversationType: rc.ChannelContext.ConversationType,
				Metadata:         metadata,
			}
			tx, txErr := db.BeginTx(ctx, pgx.TxOptions{})
			if txErr != nil {
				recordDesktopChannelDeliveryFailure(db, rc.Run.ID, txErr)
				slog.WarnContext(ctx, "desktop qq outbox tx begin failed", "run_id", rc.Run.ID, "err", txErr.Error())
				return err
			}
			threadID := rc.Run.ThreadID
			outboxRec, insertErr := outboxRepo.InsertPending(ctx, tx, rc.Run.ID, rc.ChannelContext.ChannelID, desktopOutboxThreadPtr(threadID), channelType, data.OutboxKindMessage, payload)
			if insertErr != nil {
				_ = tx.Rollback(ctx)
				recordDesktopChannelDeliveryFailure(db, rc.Run.ID, insertErr)
				slog.WarnContext(ctx, "desktop qq outbox insert failed", "run_id", rc.Run.ID, "err", insertErr.Error())
				return err
			}
			if cmtErr := tx.Commit(ctx); cmtErr != nil {
				recordDesktopChannelDeliveryFailure(db, rc.Run.ID, cmtErr)
				slog.WarnContext(ctx, "desktop qq outbox tx commit failed", "run_id", rc.Run.ID, "err", cmtErr.Error())
				return err
			}
			if tryErr := tryDeliverDesktopOneBotOutbox(ctx, db, rc, qCh, outboxRec, payload, outboxRepo); tryErr != nil {
				return err
			}
		}
		return err
	}
}

func desktopNormalizedChannelType(rc *pipeline.RunContext) string {
	if rc == nil || rc.ChannelContext == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(rc.ChannelContext.ChannelType))
}

func deliverDesktopTelegramChannelOutput(
	ctx context.Context,
	db data.DesktopDB,
	rc *pipeline.RunContext,
	client *telegrambot.Client,
	channel *desktopDeliveryChannelRecord,
	output string,
) error {
	if strings.TrimSpace(output) == "" {
		return nil
	}
	sender := pipeline.NewTelegramChannelSenderWithClient(client, channel.Token, 50*time.Millisecond)
	replyTo := desktopTelegramReplyReference(rc)
	messageIDs, err := sender.SendText(ctx, pipeline.ChannelDeliveryTarget{
		ChannelType:  rc.ChannelContext.ChannelType,
		Conversation: rc.ChannelContext.Conversation,
		ReplyTo:      replyTo,
	}, output)
	if err != nil {
		return err
	}
	return recordDesktopChannelDelivery(
		ctx,
		db,
		rc.Run.ID,
		rc.Run.ThreadID,
		rc.ChannelContext.ChannelID,
		rc.ChannelContext.ChannelType,
		rc.ChannelContext.Conversation.Target,
		replyTo,
		rc.ChannelContext.Conversation.ThreadID,
		messageIDs,
	)
}

func deliverDesktopTelegramChannelOutputs(
	ctx context.Context,
	db data.DesktopDB,
	rc *pipeline.RunContext,
	client *telegrambot.Client,
	channel *desktopDeliveryChannelRecord,
	output string,
	outputs []string,
) error {
	if strings.TrimSpace(output) == "" {
		return nil
	}
	if len(outputs) <= 1 {
		return deliverDesktopTelegramChannelOutput(ctx, db, rc, client, channel, output)
	}
	sender := pipeline.NewTelegramChannelSenderWithClient(client, channel.Token, 50*time.Millisecond)
	replyTo := desktopTelegramReplyReference(rc)
	for i, item := range outputs {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		ref := replyTo
		if i > 0 {
			ref = nil
		}
		messageIDs, err := sender.SendText(ctx, pipeline.ChannelDeliveryTarget{
			ChannelType:  rc.ChannelContext.ChannelType,
			Conversation: rc.ChannelContext.Conversation,
			ReplyTo:      ref,
		}, trimmed)
		if err != nil {
			return err
		}
		if err := recordDesktopChannelDelivery(
			ctx,
			db,
			rc.Run.ID,
			rc.Run.ThreadID,
			rc.ChannelContext.ChannelID,
			rc.ChannelContext.ChannelType,
			rc.ChannelContext.Conversation.Target,
			ref,
			rc.ChannelContext.Conversation.ThreadID,
			messageIDs,
		); err != nil {
			return err
		}
	}
	return nil
}

func deliverDesktopDiscordChannelOutput(
	ctx context.Context,
	db data.DesktopDB,
	rc *pipeline.RunContext,
	client pipeline.DiscordHTTPDoer,
	channel *desktopDeliveryChannelRecord,
	output string,
) error {
	if strings.TrimSpace(output) == "" {
		return nil
	}
	replyTo := desktopDiscordReplyReference(rc)
	sender := pipeline.NewDiscordChannelSenderWithClient(client, os.Getenv("ARKLOOP_DISCORD_API_BASE_URL"), channel.Token, 50*time.Millisecond)
	messageIDs, err := sender.SendText(ctx, pipeline.ChannelDeliveryTarget{
		ChannelType:  rc.ChannelContext.ChannelType,
		Conversation: rc.ChannelContext.Conversation,
		ReplyTo:      replyTo,
	}, output)
	if err != nil {
		return err
	}
	return recordDesktopChannelDelivery(
		ctx,
		db,
		rc.Run.ID,
		rc.Run.ThreadID,
		rc.ChannelContext.ChannelID,
		rc.ChannelContext.ChannelType,
		rc.ChannelContext.Conversation.Target,
		replyTo,
		rc.ChannelContext.Conversation.ThreadID,
		messageIDs,
	)
}

type desktopQQDeliveryChannelRecord struct {
	OneBotHTTPURL string
	OneBotToken   string
}

func loadDesktopQQDeliveryChannel(ctx context.Context, db data.DesktopDB, channelID uuid.UUID) (*desktopQQDeliveryChannelRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("db must not be nil")
	}
	var configRaw []byte
	err := db.QueryRow(
		ctx,
		`SELECT COALESCE(c.config_json, '{}')
		   FROM channels c
		  WHERE c.id = $1
		    AND c.is_active = 1`,
		channelID,
	).Scan(&configRaw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("desktop qq channel lookup: %w", err)
	}
	var cfg struct {
		OneBotHTTPURL string `json:"onebot_http_url"`
		OneBotToken   string `json:"onebot_token"`
	}
	if err := json.Unmarshal(configRaw, &cfg); err != nil {
		return nil, fmt.Errorf("desktop qq channel config: %w", err)
	}
	httpURL := strings.TrimSpace(cfg.OneBotHTTPURL)
	token := strings.TrimSpace(cfg.OneBotToken)
	if httpURL == "" || token == "" {
		if addr, tk := desktop.GetOneBotHTTPEndpoint(); addr != "" {
			if httpURL == "" {
				httpURL = addr
			}
			if token == "" {
				token = tk
			}
		}
	}
	if httpURL == "" {
		httpURL = "http://127.0.0.1:3000"
	}
	return &desktopQQDeliveryChannelRecord{
		OneBotHTTPURL: httpURL,
		OneBotToken:   token,
	}, nil
}

func deliverDesktopOneBotChannelOutput(
	ctx context.Context,
	db data.DesktopDB,
	rc *pipeline.RunContext,
	channel *desktopQQDeliveryChannelRecord,
	output string,
) error {
	if strings.TrimSpace(output) == "" {
		return nil
	}
	client := onebotclient.NewClient(channel.OneBotHTTPURL, channel.OneBotToken, nil)
	sender := pipeline.NewOneBotChannelSender(client, 50*time.Millisecond)

	metadata := map[string]any{}
	if rc.ChannelContext.ConversationType == "group" {
		metadata["message_type"] = "group"
	}

	messageIDs, err := sender.SendText(ctx, pipeline.ChannelDeliveryTarget{
		ChannelType:  rc.ChannelContext.ChannelType,
		Conversation: rc.ChannelContext.Conversation,
		Metadata:     metadata,
	}, output)
	if err != nil {
		return err
	}
	return recordDesktopChannelDelivery(
		ctx,
		db,
		rc.Run.ID,
		rc.Run.ThreadID,
		rc.ChannelContext.ChannelID,
		rc.ChannelContext.ChannelType,
		rc.ChannelContext.Conversation.Target,
		nil,
		rc.ChannelContext.Conversation.ThreadID,
		messageIDs,
	)
}

func pipelineNormalizedAssistantOutputs(outputs []string, fallback string) []string {
	normalized := make([]string, 0, len(outputs))
	for _, item := range outputs {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}
	if len(normalized) > 0 {
		return normalized
	}
	if trimmed := strings.TrimSpace(fallback); trimmed != "" {
		return []string{trimmed}
	}
	return nil
}

func desktopDiscordReplyReference(rc *pipeline.RunContext) *pipeline.ChannelMessageRef {
	if rc == nil || rc.ChannelContext == nil {
		return nil
	}
	if rc.ChannelContext.TriggerMessage != nil && strings.TrimSpace(rc.ChannelContext.TriggerMessage.MessageID) != "" {
		return rc.ChannelContext.TriggerMessage
	}
	if strings.TrimSpace(rc.ChannelContext.InboundMessage.MessageID) == "" {
		return nil
	}
	ref := rc.ChannelContext.InboundMessage
	return &ref
}

func desktopTelegramReplyReference(rc *pipeline.RunContext) *pipeline.ChannelMessageRef {
	if rc == nil || rc.ChannelContext == nil {
		return nil
	}
	if rc.ChannelReplyOverride != nil {
		return rc.ChannelReplyOverride
	}
	return nil
}

func desktopTelegramReplyReferenceMessageID(rc *pipeline.RunContext) string {
	ref := desktopTelegramReplyReference(rc)
	if ref == nil {
		return ""
	}
	return strings.TrimSpace(ref.MessageID)
}

func desktopDiscordReplyReferenceMessageID(rc *pipeline.RunContext) string {
	ref := desktopDiscordReplyReference(rc)
	if ref == nil {
		return ""
	}
	return strings.TrimSpace(ref.MessageID)
}

func desktopOneBotReplyReference(rc *pipeline.RunContext) *pipeline.ChannelMessageRef {
	if rc == nil || rc.ChannelContext == nil {
		return nil
	}
	if rc.HeartbeatRun {
		return nil
	}
	if isPrivateChannelConversation(rc.ChannelContext.ConversationType) {
		return nil
	}
	if rc.ChannelContext.TriggerMessage != nil && strings.TrimSpace(rc.ChannelContext.TriggerMessage.MessageID) != "" {
		return rc.ChannelContext.TriggerMessage
	}
	if strings.TrimSpace(rc.ChannelContext.InboundMessage.MessageID) == "" {
		return nil
	}
	ref := rc.ChannelContext.InboundMessage
	return &ref
}

func isPrivateChannelConversation(ct string) bool {
	switch strings.ToLower(strings.TrimSpace(ct)) {
	case "private", "dm":
		return true
	default:
		return false
	}
}

func loadDesktopChannelIdentity(ctx context.Context, db data.DesktopDB, identityID uuid.UUID) (*desktopChannelIdentityRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("db must not be nil")
	}
	var item desktopChannelIdentityRecord
	err := db.QueryRow(
		ctx,
		`SELECT user_id
		   FROM channel_identities
		  WHERE id = $1`,
		identityID,
	).Scan(&item.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("desktop channel identity lookup: %w", err)
	}
	return &item, nil
}

func loadDesktopChannelOwner(ctx context.Context, db data.DesktopDB, channelID uuid.UUID) (*uuid.UUID, error) {
	if db == nil {
		return nil, nil
	}
	var ownerUserID *uuid.UUID
	err := db.QueryRow(ctx,
		`SELECT owner_user_id FROM channels WHERE id = $1`,
		channelID,
	).Scan(&ownerUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("desktop channel owner lookup: %w", err)
	}
	return ownerUserID, nil
}

func loadDesktopChannelConfigJSON(ctx context.Context, db data.DesktopDB, channelID uuid.UUID) ([]byte, error) {
	if db == nil {
		return nil, nil
	}
	var configJSON []byte
	err := db.QueryRow(ctx,
		`SELECT COALESCE(config_json, '{}') FROM channels WHERE id = $1`,
		channelID,
	).Scan(&configJSON)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("desktop channel config lookup: %w", err)
	}
	return configJSON, nil
}

func loadDesktopDeliveryChannel(ctx context.Context, db data.DesktopDB, channelID uuid.UUID) (*desktopDeliveryChannelRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("db must not be nil")
	}
	var (
		channelType    string
		encryptedValue *string
		keyVersion     *int
		configRaw      []byte
	)
	err := db.QueryRow(
		ctx,
		`SELECT c.channel_type, s.encrypted_value, s.key_version, COALESCE(c.config_json, '{}')
		   FROM channels c
		   LEFT JOIN secrets s ON s.id = c.credentials_id
		  WHERE c.id = $1
		    AND c.is_active = 1`,
		channelID,
	).Scan(&channelType, &encryptedValue, &keyVersion, &configRaw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("desktop channel lookup: %w", err)
	}
	if encryptedValue == nil || strings.TrimSpace(*encryptedValue) == "" || keyVersion == nil {
		return nil, fmt.Errorf("desktop channel lookup: missing channel token")
	}
	keyRing, err := desktop.LoadEncryptionKeyRing(desktop.KeyRingOptions{})
	if err != nil {
		return nil, fmt.Errorf("desktop channel lookup: load encryption key: %w", err)
	}
	token, err := decryptDesktopCiphertext(keyRing, *encryptedValue, *keyVersion)
	if err != nil {
		return nil, fmt.Errorf("desktop channel lookup: decrypt token: %w", err)
	}
	return &desktopDeliveryChannelRecord{ChannelType: channelType, Token: token, ConfigJSON: configRaw}, nil
}

func recordDesktopChannelDeliveryFailure(db data.DesktopDB, runID uuid.UUID, err error) {
	if db == nil || runID == uuid.Nil || err == nil {
		return
	}
	tx, txErr := db.BeginTx(context.Background(), pgx.TxOptions{})
	if txErr != nil {
		return
	}
	defer func() { _ = tx.Rollback(context.Background()) }() //nolint:errcheck

	repo := data.DesktopRunEventsRepository{}
	if _, appendErr := repo.AppendEvent(context.Background(), tx, runID, "run.channel_delivery_failed", map[string]any{
		"error": err.Error(),
	}, nil, nil); appendErr != nil {
		return
	}
	if err := tx.Commit(context.Background()); err != nil {
		slog.Error("desktop_channel_delivery_failure_commit_failed", "run_id", runID, "err", err)
	}
}

func recordDesktopChannelDelivery(
	ctx context.Context,
	db data.DesktopDB,
	runID uuid.UUID,
	threadID uuid.UUID,
	channelID uuid.UUID,
	channelType string,
	platformChatID string,
	replyTo *pipeline.ChannelMessageRef,
	platformThreadID *string,
	platformMessageIDs []string,
) error {
	if db == nil || channelID == uuid.Nil || strings.TrimSpace(platformChatID) == "" || len(platformMessageIDs) == 0 {
		return nil
	}
	tx, txErr := db.BeginTx(ctx, pgx.TxOptions{})
	if txErr != nil {
		return txErr
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	var runRef *uuid.UUID
	if runID != uuid.Nil {
		runRef = &runID
	}
	var threadRef *uuid.UUID
	if threadID != uuid.Nil {
		threadRef = &threadID
	}
	deliveryRepo := data.ChannelDeliveryRepository{}
	ledgerRepo := data.ChannelMessageLedgerRepository{}
	for _, platformMessageID := range platformMessageIDs {
		if err := deliveryRepo.RecordDelivery(
			ctx,
			tx,
			runID,
			threadID,
			channelID,
			platformChatID,
			platformMessageID,
		); err != nil {
			return err
		}
		if err := ledgerRepo.Record(ctx, tx, data.ChannelMessageLedgerRecordInput{
			ChannelID:               channelID,
			ChannelType:             channelType,
			Direction:               data.ChannelMessageDirectionOutbound,
			ThreadID:                threadRef,
			RunID:                   runRef,
			PlatformConversationID:  platformChatID,
			PlatformMessageID:       platformMessageID,
			PlatformParentMessageID: channelMessageIDPtr(replyTo),
			PlatformThreadID:        platformThreadID,
		}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func channelMessageIDPtr(ref *pipeline.ChannelMessageRef) *string {
	if ref == nil || strings.TrimSpace(ref.MessageID) == "" {
		return nil
	}
	value := strings.TrimSpace(ref.MessageID)
	return &value
}

func tryDeliverDesktopTelegramOutbox(
	ctx context.Context,
	db data.DesktopDB,
	rc *pipeline.RunContext,
	client *telegrambot.Client,
	channel *desktopDeliveryChannelRecord,
	outboxRec *data.ChannelDeliveryOutboxRecord,
	payload data.OutboxPayload,
	outboxRepo data.ChannelDeliveryOutboxRepository,
) error {
	sender := pipeline.NewTelegramChannelSenderWithClient(client, channel.Token, 50*time.Millisecond)
	replyTo := desktopTelegramReplyReference(rc)
	for i := outboxRec.SegmentsSent; i < len(payload.Outputs); i++ {
		trimmed := strings.TrimSpace(payload.Outputs[i])
		if trimmed == "" {
			if err := outboxRepo.UpdateProgress(ctx, db, outboxRec.ID, i+1); err != nil {
				return err
			}
			continue
		}
		ref := replyTo
		if i > 0 {
			ref = nil
		}
		messageIDs, err := sender.SendText(ctx, pipeline.ChannelDeliveryTarget{
			ChannelType:  rc.ChannelContext.ChannelType,
			Conversation: rc.ChannelContext.Conversation,
			ReplyTo:      ref,
		}, trimmed)
		if err != nil {
			return handleDesktopInlineOutboxFailure(ctx, db, outboxRec, err, outboxRepo)
		}
		if err := recordDesktopChannelDelivery(
			ctx,
			db,
			rc.Run.ID,
			rc.Run.ThreadID,
			rc.ChannelContext.ChannelID,
			rc.ChannelContext.ChannelType,
			rc.ChannelContext.Conversation.Target,
			ref,
			rc.ChannelContext.Conversation.ThreadID,
			messageIDs,
		); err != nil {
			return handleDesktopInlineOutboxFailure(ctx, db, outboxRec, err, outboxRepo)
		}
		if err := outboxRepo.UpdateProgress(ctx, db, outboxRec.ID, i+1); err != nil {
			return err
		}
		outboxRec.SegmentsSent = i + 1
	}
	return outboxRepo.UpdateSent(ctx, db, outboxRec.ID)
}

func tryDeliverDesktopDiscordOutbox(
	ctx context.Context,
	db data.DesktopDB,
	rc *pipeline.RunContext,
	client pipeline.DiscordHTTPDoer,
	channel *desktopDeliveryChannelRecord,
	outboxRec *data.ChannelDeliveryOutboxRecord,
	payload data.OutboxPayload,
	outboxRepo data.ChannelDeliveryOutboxRepository,
) error {
	sender := pipeline.NewDiscordChannelSenderWithClient(client, os.Getenv("ARKLOOP_DISCORD_API_BASE_URL"), channel.Token, 50*time.Millisecond)
	replyTo := desktopDiscordReplyReference(rc)
	for i := outboxRec.SegmentsSent; i < len(payload.Outputs); i++ {
		trimmed := strings.TrimSpace(payload.Outputs[i])
		if trimmed == "" {
			if err := outboxRepo.UpdateProgress(ctx, db, outboxRec.ID, i+1); err != nil {
				return err
			}
			continue
		}
		ref := replyTo
		if i > 0 {
			ref = nil
		}
		messageIDs, err := sender.SendText(ctx, pipeline.ChannelDeliveryTarget{
			ChannelType:  rc.ChannelContext.ChannelType,
			Conversation: rc.ChannelContext.Conversation,
			ReplyTo:      ref,
		}, trimmed)
		if err != nil {
			return handleDesktopInlineOutboxFailure(ctx, db, outboxRec, err, outboxRepo)
		}
		if err := recordDesktopChannelDelivery(
			ctx,
			db,
			rc.Run.ID,
			rc.Run.ThreadID,
			rc.ChannelContext.ChannelID,
			rc.ChannelContext.ChannelType,
			rc.ChannelContext.Conversation.Target,
			ref,
			rc.ChannelContext.Conversation.ThreadID,
			messageIDs,
		); err != nil {
			return handleDesktopInlineOutboxFailure(ctx, db, outboxRec, err, outboxRepo)
		}
		if err := outboxRepo.UpdateProgress(ctx, db, outboxRec.ID, i+1); err != nil {
			return err
		}
		outboxRec.SegmentsSent = i + 1
	}
	return outboxRepo.UpdateSent(ctx, db, outboxRec.ID)
}

func tryDeliverDesktopOneBotOutbox(
	ctx context.Context,
	db data.DesktopDB,
	rc *pipeline.RunContext,
	qCh *desktopQQDeliveryChannelRecord,
	outboxRec *data.ChannelDeliveryOutboxRecord,
	payload data.OutboxPayload,
	outboxRepo data.ChannelDeliveryOutboxRepository,
) error {
	client := onebotclient.NewClient(qCh.OneBotHTTPURL, qCh.OneBotToken, nil)
	sender := pipeline.NewOneBotChannelSender(client, 50*time.Millisecond)
	replyTo := desktopOneBotReplyReference(rc)
	metadata := payload.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	for i := outboxRec.SegmentsSent; i < len(payload.Outputs); i++ {
		trimmed := strings.TrimSpace(payload.Outputs[i])
		if trimmed == "" {
			if err := outboxRepo.UpdateProgress(ctx, db, outboxRec.ID, i+1); err != nil {
				return err
			}
			continue
		}
		ref := replyTo
		if i > 0 {
			ref = nil
		}
		messageIDs, err := sender.SendText(ctx, pipeline.ChannelDeliveryTarget{
			ChannelType:  rc.ChannelContext.ChannelType,
			Conversation: rc.ChannelContext.Conversation,
			ReplyTo:      ref,
			Metadata:     metadata,
		}, trimmed)
		if err != nil {
			return handleDesktopInlineOutboxFailure(ctx, db, outboxRec, err, outboxRepo)
		}
		if err := recordDesktopChannelDelivery(
			ctx,
			db,
			rc.Run.ID,
			rc.Run.ThreadID,
			rc.ChannelContext.ChannelID,
			rc.ChannelContext.ChannelType,
			rc.ChannelContext.Conversation.Target,
			ref,
			rc.ChannelContext.Conversation.ThreadID,
			messageIDs,
		); err != nil {
			return handleDesktopInlineOutboxFailure(ctx, db, outboxRec, err, outboxRepo)
		}
		if err := outboxRepo.UpdateProgress(ctx, db, outboxRec.ID, i+1); err != nil {
			return err
		}
		outboxRec.SegmentsSent = i + 1
	}
	return outboxRepo.UpdateSent(ctx, db, outboxRec.ID)
}

func handleDesktopInlineOutboxFailure(
	ctx context.Context,
	db data.DesktopDB,
	outboxRec *data.ChannelDeliveryOutboxRecord,
	err error,
	outboxRepo data.ChannelDeliveryOutboxRepository,
) error {
	attempts := outboxRec.Attempts + 1
	nextRetry := time.Now().UTC().Add(data.OutboxBackoffDelay(attempts))
	if attempts >= data.OutboxMaxAttempts {
		if deadErr := outboxRepo.MarkDead(ctx, db, outboxRec.ID, err.Error()); deadErr != nil {
			slog.ErrorContext(ctx, "desktop channel delivery outbox mark dead failed",
				"outbox_id", outboxRec.ID, "run_id", outboxRec.RunID, "err", deadErr)
			return fmt.Errorf("mark dead: %w", errors.Join(err, deadErr))
		}
		slog.WarnContext(ctx, "desktop channel delivery outbox dead",
			"outbox_id", outboxRec.ID, "run_id", outboxRec.RunID, "attempts", attempts, "err", err.Error())
		return err
	}
	if updateErr := outboxRepo.UpdateFailure(ctx, db, outboxRec.ID, attempts, err.Error(), nextRetry); updateErr != nil {
		slog.ErrorContext(ctx, "desktop channel delivery outbox update failure failed",
			"outbox_id", outboxRec.ID, "run_id", outboxRec.RunID, "attempts", attempts, "err", updateErr)
		return fmt.Errorf("update failure: %w", errors.Join(err, updateErr))
	}
	return fmt.Errorf("%w; will retry via drain", err)
}

const (
	outboxCleanupEveryRounds = 360
	outboxSentRetention      = 7 * 24 * time.Hour
	outboxDeadRetention      = 30 * 24 * time.Hour
)

func desktopSubAgentContext(db data.DesktopDB, storage *subagentctl.SnapshotStorage) pipeline.RunMiddleware {
	return func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		if storage == nil || rc == nil || db == nil || rc.Run.ParentRunID == nil {
			return next(ctx, rc)
		}
		if !desktopSubAgentSchemaAvailable(ctx, db) {
			return next(ctx, rc)
		}
		snapshot, err := storage.LoadByCurrentRun(ctx, db, rc.Run.ID)
		if err != nil {
			return err
		}
		if snapshot == nil {
			return next(ctx, rc)
		}
		routing := snapshot.EffectiveRouting()
		if routeID := strings.TrimSpace(routing.RouteID); routeID != "" {
			if _, ok := rc.InputJSON["route_id"]; !ok {
				rc.InputJSON["route_id"] = routeID
			}
		}
		if model := strings.TrimSpace(routing.Model); model != "" {
			if _, ok := rc.InputJSON["model"]; !ok {
				rc.InputJSON["model"] = model
			}
		}
		if len(snapshot.Runtime.ToolAllowlist) > 0 {
			rc.AllowlistSet = desktopIntersectAllowlist(rc.AllowlistSet, snapshot.Runtime.ToolAllowlist, rc.ToolRegistry)
		}
		if len(snapshot.Runtime.ToolDenylist) > 0 {
			for _, denied := range snapshot.Runtime.ToolDenylist {
				pipeline.RemoveToolOrGroup(rc.AllowlistSet, rc.ToolRegistry, denied)
			}
			rc.ToolDenylist = desktopMergeToolNames(rc.ToolDenylist, snapshot.Runtime.ToolDenylist)
		}
		return next(ctx, rc)
	}
}

func desktopIntersectAllowlist(current map[string]struct{}, parent []string, registry *tools.Registry) map[string]struct{} {
	resolved := map[string]struct{}{}
	if len(current) == 0 || len(parent) == 0 {
		return resolved
	}
	parentSet := make(map[string]struct{}, len(parent))
	for _, item := range parent {
		cleaned := strings.TrimSpace(item)
		if cleaned == "" {
			continue
		}
		parentSet[cleaned] = struct{}{}
	}
	for name := range current {
		if pipeline.ToolAllowed(parentSet, registry, name) {
			resolved[name] = struct{}{}
		}
	}
	return resolved
}

func desktopMergeToolNames(left []string, right []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(left)+len(right))
	for _, group := range [][]string{left, right} {
		for _, item := range group {
			cleaned := strings.TrimSpace(item)
			if cleaned == "" {
				continue
			}
			if _, ok := seen[cleaned]; ok {
				continue
			}
			seen[cleaned] = struct{}{}
			result = append(result, cleaned)
		}
	}
	return result
}

// desktopPersonaResolution resolves persona from desktop DB or filesystem.
func desktopPersonaResolution(
	db data.DesktopDB,
	getBaseRegistry func() *personas.Registry,
	runsRepo data.DesktopRunsRepository,
	eventsRepo data.DesktopRunEventsRepository,
) pipeline.RunMiddleware {
	return func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		var dbDefs []personas.Definition
		if db != nil {
			var err error
			dbDefs, err = personas.LoadPersonasFromDesktopDB(ctx, db)
			if err != nil {
				slog.WarnContext(ctx, "desktop: persona db load failed, trying filesystem", "err", err)
				dbDefs = nil
			}
		}

		var registry *personas.Registry
		if getBaseRegistry != nil {
			if base := getBaseRegistry(); base != nil {
				registry = personas.MergeRegistry(base, dbDefs)
			}
		}
		if registry == nil {
			registry = personas.NewRegistry()
			for _, def := range dbDefs {
				registry.Set(def)
			}
		}

		if len(registry.ListIDs()) == 0 && getBaseRegistry != nil {
			if base := getBaseRegistry(); base != nil {
				registry = base
			}
		}

		resolution := personas.ResolvePersona(rc.InputJSON, registry)
		if resolution.Error != nil {
			return desktopWriteFailure(ctx, db, rc.Run, rc.Emitter, runsRepo, eventsRepo,
				resolution.Error.ErrorClass, resolution.Error.Message, resolution.Error.Details)
		}

		rc.ToolBudget = map[string]any{}
		rc.PerToolSoftLimits = tools.DefaultPerToolSoftLimits()
		rc.ToolDenylist = nil
		rc.StreamThinking = true
		rc.SummarizerDefinition = nil
		rc.TitleSummarizer = nil
		rc.ResultSummarizer = nil
		rc.PersonaDefinition = resolution.Definition

		limits := sharedexec.NormalizePlatformLimits(sharedexec.PlatformLimits{
			AgentReasoningIterations: rc.AgentReasoningIterationsLimit,
			ToolContinuationBudget:   rc.ToolContinuationBudgetLimit,
		})

		var agentConfig *pipeline.ResolvedAgentConfig
		if resolution.Definition != nil {
			agentConfig = &pipeline.ResolvedAgentConfig{
				Model:              resolution.Definition.Model,
				PromptCacheControl: resolution.Definition.PromptCacheControl,
				ReasoningMode:      resolution.Definition.ReasoningMode,
			}
		}

		profile := sharedexec.ResolveEffectiveProfile(
			limits,
			toDesktopAgentConfigProfile(agentConfig),
			toDesktopPersonaProfile(resolution.Definition),
		)

		rc.AgentConfig = agentConfig
		rc.ResetPromptAssembly()
		cacheSystemPrompt := agentConfig != nil && agentConfig.PromptCacheControl == "system_prompt"
		rc.UpsertPromptSegment(pipeline.PromptSegment{
			Name:          "persona.system_prompt",
			Target:        pipeline.PromptTargetSystemPrefix,
			Role:          "system",
			Text:          profile.SystemPrompt,
			Stability:     pipeline.PromptStabilityStablePrefix,
			CacheEligible: cacheSystemPrompt,
		})
		rc.ReasoningIterations = profile.ReasoningIterations
		rc.ToolContinuationBudget = profile.ToolContinuationBudget
		rc.MaxOutputTokens = profile.MaxOutputTokens
		rc.Temperature = profile.Temperature
		rc.TopP = profile.TopP
		rc.ReasoningMode = profile.ReasoningMode
		if override := normalizeDesktopRunReasoningMode(rc.InputJSON["reasoning_mode"]); override != "" {
			rc.ReasoningMode = override
			if agentConfig != nil {
				agentConfig.ReasoningMode = override
			}
		}
		rc.ToolTimeoutMs = profile.ToolTimeoutMs
		rc.ToolBudget = profile.ToolBudget
		rc.PerToolSoftLimits = tools.CopyPerToolSoftLimits(profile.PerToolSoftLimits)
		rc.MaxCostMicros = profile.MaxCostMicros
		rc.MaxTotalOutputTokens = profile.MaxTotalOutputTokens
		rc.PreferredCredentialName = profile.PreferredCredentialName

		if resolution.Definition != nil {
			def := resolution.Definition
			rc.StreamThinking = def.StreamThinking
			rc.ToolDenylist = append([]string(nil), def.ToolDenylist...)
			if len(def.ToolAllowlist) > 0 {
				narrowed := make(map[string]struct{}, len(def.ToolAllowlist))
				for _, name := range def.ToolAllowlist {
					if pipeline.ToolAllowed(rc.AllowlistSet, rc.ToolRegistry, name) {
						narrowed[name] = struct{}{}
					}
				}
				rc.AllowlistSet = narrowed
			}
			for _, name := range def.ToolDenylist {
				pipeline.RemoveToolOrGroup(rc.AllowlistSet, rc.ToolRegistry, name)
			}
		}

		if registry != nil {
			if summarizerDef, ok := registry.Get(personas.SystemSummarizerPersonaID); ok {
				summaryClone := summarizerDef
				rc.SummarizerDefinition = &summaryClone
				rc.TitleSummarizer = summarizerDef.TitleSummarizer
				rc.ResultSummarizer = summarizerDef.ResultSummarizer
			}
		}
		if rc.TitleSummarizer == nil || rc.ResultSummarizer == nil {
			fallback := personas.DefaultSystemSummarizerDefinition()
			rc.SummarizerDefinition = &fallback
			if rc.TitleSummarizer == nil {
				rc.TitleSummarizer = fallback.TitleSummarizer
			}
			if rc.ResultSummarizer == nil {
				rc.ResultSummarizer = fallback.ResultSummarizer
			}
		}

		return next(ctx, rc)
	}
}

func toDesktopAgentConfigProfile(ac *pipeline.ResolvedAgentConfig) *sharedexec.AgentConfigProfile {
	if ac == nil {
		return nil
	}
	return &sharedexec.AgentConfigProfile{
		Temperature:     ac.Temperature,
		MaxOutputTokens: ac.MaxOutputTokens,
		TopP:            ac.TopP,
		ReasoningMode:   ac.ReasoningMode,
	}
}

func toDesktopPersonaProfile(def *personas.Definition) *sharedexec.PersonaProfile {
	if def == nil {
		return nil
	}
	promptMD := strings.TrimSpace(def.PromptMD)
	if s := strings.TrimSpace(def.RoleSoulMD); s != "" {
		promptMD = s + "\n\n" + promptMD
	}
	if s := strings.TrimSpace(def.RolePromptMD); s != "" {
		promptMD = promptMD + "\n\n" + s
	}
	return &sharedexec.PersonaProfile{
		SoulMD:                  def.SoulMD,
		PromptMD:                strings.TrimSpace(promptMD),
		PreferredCredentialName: def.PreferredCredential,
		Budgets: sharedexec.RequestedBudgets{
			ReasoningIterations:    def.Budgets.ReasoningIterations,
			ToolContinuationBudget: def.Budgets.ToolContinuationBudget,
			MaxOutputTokens:        def.Budgets.MaxOutputTokens,
			ToolTimeoutMs:          def.Budgets.ToolTimeoutMs,
			ToolBudget:             def.Budgets.ToolBudget,
			PerToolSoftLimits:      def.Budgets.PerToolSoftLimits,
			Temperature:            def.Budgets.Temperature,
			TopP:                   def.Budgets.TopP,
		},
	}
}

// desktopRouting selects the LLM provider route from env config.
func desktopRouting(
	fallbackRouter *routing.ProviderRouter,
	auxGateway llm.Gateway,
	emitDebugEvents bool,
	db data.DesktopDB,
	runsRepo data.DesktopRunsRepository,
	eventsRepo data.DesktopRunEventsRepository,
) pipeline.RunMiddleware {
	return func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		router := fallbackRouter
		if dbCfg, err := loadDesktopRoutingConfig(ctx, db); err == nil {
			router = routing.NewProviderRouter(dbCfg)
		}
		cfg := router.Config()

		var decision routing.ProviderRouteDecision
		if _, hasRouteID := rc.InputJSON["route_id"]; hasRouteID {
			decision = router.Decide(rc.InputJSON, false, false)
		} else {
			// user model override takes priority over persona default
			selector := ""
			if modelOverride, ok := rc.InputJSON["model"].(string); ok && strings.TrimSpace(modelOverride) != "" {
				selector = strings.TrimSpace(modelOverride)
			} else if rc.AgentConfig != nil && rc.AgentConfig.Model != nil {
				selector = strings.TrimSpace(*rc.AgentConfig.Model)
			}
			if selector != "" {
				credName, modelName, exact := splitDesktopModelSelector(selector)
				if exact {
					if route, cred, ok := cfg.GetHighestPriorityRouteByCredentialAndModel(credName, modelName, rc.InputJSON); ok {
						decision = routing.ProviderRouteDecision{
							Selected: &routing.SelectedProviderRoute{Route: route, Credential: cred},
						}
					}
				} else {
					if route, cred, ok := cfg.GetHighestPriorityRouteByModel(selector, rc.InputJSON); ok {
						decision = routing.ProviderRouteDecision{
							Selected: &routing.SelectedProviderRoute{Route: route, Credential: cred},
						}
					}
				}
			}
			if decision.Selected == nil && decision.Denied == nil {
				if rc.PreferredCredentialName != "" {
					if route, cred, ok := cfg.GetHighestPriorityRouteByCredentialName(rc.PreferredCredentialName, rc.InputJSON); ok {
						decision = routing.ProviderRouteDecision{
							Selected: &routing.SelectedProviderRoute{Route: route, Credential: cred},
						}
					}
				}
			}
			if decision.Selected == nil && decision.Denied == nil {
				decision = router.Decide(rc.InputJSON, false, false)
			}
		}

		if decision.Denied != nil {
			return desktopWriteFailure(ctx, db, rc.Run, rc.Emitter, runsRepo, eventsRepo,
				decision.Denied.ErrorClass, decision.Denied.Message, nil)
		}
		if decision.Selected == nil {
			return desktopWriteFailure(ctx, db, rc.Run, rc.Emitter, runsRepo, eventsRepo,
				"internal.error", "route decision is empty", nil)
		}

		gateway, err := desktopGatewayFromRoute(*decision.Selected, auxGateway, emitDebugEvents, rc.LlmMaxResponseBytes)
		if err != nil {
			return desktopWriteFailure(ctx, db, rc.Run, rc.Emitter, runsRepo, eventsRepo,
				"internal.error", "gateway initialization failed", nil)
		}

		resolveGateway := func(_ context.Context, routeID string) (llm.Gateway, *routing.SelectedProviderRoute, error) {
			cleaned := strings.TrimSpace(routeID)
			if cleaned == "" {
				return rc.Gateway, rc.SelectedRoute, nil
			}
			d := router.Decide(map[string]any{"route_id": cleaned}, false, false)
			if d.Selected == nil {
				return nil, nil, fmt.Errorf("route not found: %s", cleaned)
			}
			gw, gwErr := desktopGatewayFromRoute(*d.Selected, auxGateway, emitDebugEvents, rc.LlmMaxResponseBytes)
			if gwErr != nil {
				return nil, nil, gwErr
			}
			return gw, d.Selected, nil
		}

		rc.Gateway = gateway
		rc.SelectedRoute = decision.Selected
		if rc.Temperature == nil {
			rc.Temperature = routing.RouteDefaultTemperature(decision.Selected.Route)
		}
		rc.ResolveGatewayForRouteID = resolveGateway
		rc.ResolveGatewayForAgentName = func(ctx context.Context, name string) (llm.Gateway, *routing.SelectedProviderRoute, error) {
			cleanedSelector := strings.TrimSpace(name)
			if cleanedSelector == "" {
				return resolveGateway(ctx, "")
			}
			selected, resolveErr := resolveDesktopSelectedRouteBySelector(cfg, cleanedSelector)
			if resolveErr != nil {
				return nil, nil, resolveErr
			}
			gw, gwErr := desktopGatewayFromRoute(*selected, auxGateway, emitDebugEvents, rc.LlmMaxResponseBytes)
			if gwErr != nil {
				return nil, nil, gwErr
			}
			return gw, selected, nil
		}

		rc.RoutingByokEnabled = false
		if rc.Tracer != nil && rc.SelectedRoute != nil {
			rc.Tracer.Event("routing", "routing.selected", map[string]any{
				"model":          rc.SelectedRoute.Route.Model,
				"provider":       string(rc.SelectedRoute.Credential.ProviderKind),
				"byok":           false,
				"context_window": routing.RouteContextWindowTokens(rc.SelectedRoute.Route),
			})
		}

		return next(ctx, rc)
	}
}

func desktopGatewayFromRoute(selected routing.SelectedProviderRoute, stub llm.Gateway, debug bool, maxBytes int) (llm.Gateway, error) {
	return pipeline.GatewayFromSelectedRoute(selected, stub, debug, maxBytes)
}

func normalizeDesktopRunReasoningMode(raw any) string {
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "auto":
		return "auto"
	case "enabled":
		return "enabled"
	case "disabled":
		return "disabled"
	case "none":
		return "none"
	case "minimal":
		return "minimal"
	case "low":
		return "low"
	case "medium":
		return "medium"
	case "high":
		return "high"
	case "xhigh", "extra_high", "extra-high", "extra high":
		return "xhigh"
	default:
		return ""
	}
}

// --------------- desktop agent loop ---------------

var desktopTerminalStatuses = map[string]string{
	"run.completed":   "completed",
	"run.failed":      "failed",
	"run.interrupted": "interrupted",
	"run.cancelled":   "cancelled",
}

func desktopAgentLoop(
	db data.DesktopDB,
	bus eventbus.EventBus,
	jobQueue queue.JobQueue,
	runsRepo data.DesktopRunsRepository,
	eventsRepo data.DesktopRunEventsRepository,
	shellExec *runtime.DynamicShellExecutor,
	runtimeSnapshot *sharedtoolruntime.RuntimeSnapshot,
) pipeline.RunHandler {
	return func(ctx context.Context, rc *pipeline.RunContext) error {
		selected := rc.SelectedRoute
		var projector *subagentctl.SubAgentStateProjector
		if desktopSubAgentSchemaAvailable(ctx, db) {
			projector = subagentctl.NewSubAgentStateProjector(db, nil, jobQueue)
		}

		w := &desktopEventWriter{
			db:                      db,
			bus:                     bus,
			run:                     rc.Run,
			traceID:                 rc.TraceID,
			model:                   selected.Route.Model,
			runsRepo:                runsRepo,
			eventsRepo:              eventsRepo,
			projector:               projector,
			usageRepo:               data.UsageRecordsRepository{},
			responseDraftStore:      rc.ResponseDraftStore,
			telegramBoundaryFlush:   rc.TelegramToolBoundaryFlush,
			telegramProgressTracker: rc.TelegramProgressTracker,
			heartbeatRun:            pipeline.IsHeartbeatRunContext(rc),
		}
		personaID := ""
		if rc.PersonaDefinition != nil {
			personaID = rc.PersonaDefinition.ID
		}

		routeData := selected.ToRunEventDataJSON()
		routeSelected := rc.Emitter.Emit("run.route.selected", routeData, nil, nil)
		if err := w.append(ctx, rc.Run.ID, routeSelected, personaID); err != nil {
			return err
		}

		executorType := "agent.simple"
		var executorConfig map[string]any
		if rc.PersonaDefinition != nil {
			if rc.PersonaDefinition.ExecutorType != "" {
				executorType = rc.PersonaDefinition.ExecutorType
			}
			executorConfig = rc.PersonaDefinition.ExecutorConfig
		}

		exec, err := rc.ExecutorBuilder.Build(executorType, executorConfig)
		if err != nil {
			failed := rc.Emitter.Emit("run.failed", map[string]any{
				"error_class": "internal.error",
				"message":     fmt.Sprintf("build executor %q: %s", executorType, err),
			}, nil, pipeline.StringPtr("internal.error"))
			if appendErr := w.append(ctx, rc.Run.ID, failed, ""); appendErr != nil {
				slog.Error("desktop_append_run_failed",
					"run_id", rc.Run.ID.String(),
					"error", appendErr.Error(),
				)
			}
			rc.ChannelTerminalNotice = strings.TrimSpace(w.terminalUserMessage)
			return nil
		}

		execCtx, cancelExec := context.WithCancel(ctx)
		defer cancelExec()
		stopCancelWatch := startDesktopRunCancelWatcher(execCtx, db, rc.Run.ID, cancelExec)
		defer stopCancelWatch()
		defer cleanupDesktopRunTools(rc, w)

		execErr := exec.Execute(execCtx, rc, rc.Emitter, func(ev events.RunEvent) error {
			return w.append(execCtx, rc.Run.ID, ev, "")
		})
		if errors.Is(execErr, context.Canceled) {
			stopped, stopErr := w.finalizeCancelledIfRequested(ctx)
			if stopErr != nil {
				return stopErr
			}
			if stopped {
				execErr = errDesktopStopProcessing
			}
		}
		if execErr != nil && !errors.Is(execErr, errDesktopStopProcessing) {
			return execErr
		}

		if !w.completed {
			rc.ChannelTerminalNotice = strings.TrimSpace(w.terminalUserMessage)
		}
		if err := desktopPersistFinalAssistantOutput(ctx, db, rc, w, runsRepo, eventsRepo); err != nil {
			return err
		}
		rc.RunToolCallCount = w.toolCallCount
		rc.RunIterationCount = w.iterationCount
		rc.ThreadPersistReady = true
		return nil
	}
}

func cleanupDesktopRunTools(rc *pipeline.RunContext, writer *desktopEventWriter) {
	if rc == nil || writer == nil {
		return
	}
	read.CleanupRunFromExecutors(rc.ToolExecutors, rc.Run.ID.String())
	if cleaner, ok := rc.ToolExecutors[localshell.ExecCommandAgentSpec.Name].(interface {
		CleanupRun(context.Context, string, string) error
	}); ok {
		go func(runID string, terminalStatus string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := cleaner.CleanupRun(ctx, runID, terminalStatus); err != nil {
				slog.Warn("desktop shell cleanup failed", "run_id", runID, "error", err.Error())
			}
		}(rc.Run.ID.String(), writer.terminalStatus)
	}
	if cleaner, ok := rc.ToolExecutors[acptool.SpawnACPAgentSpec.Name].(interface {
		CleanupRun(context.Context, string, string) error
	}); ok {
		go func(runID string, terminalStatus string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := cleaner.CleanupRun(ctx, runID, terminalStatus); err != nil {
				slog.Warn("desktop acp cleanup failed", "run_id", runID, "error", err.Error())
			}
		}(rc.Run.ID.String(), writer.terminalStatus)
	}
	if cleaner, ok := rc.ToolExecutors[read.AgentSpec.Name].(interface{ CleanupRun(string) }); ok {
		cleaner.CleanupRun(rc.Run.ID.String())
	}
	if rc.Runtime != nil && rc.Runtime.SandboxBaseURL != "" {
		go sandboxbuiltin.CleanupSession(rc.Runtime.SandboxBaseURL, rc.Runtime.SandboxAuthToken, rc.Run.ID.String(), rc.Run.AccountID.String())
	}
	tools.CleanupPersistedToolOutputs(rc.WorkDir, rc.Run.ID.String())
}

var errDesktopStopProcessing = errors.New("desktop_stop_processing")

// desktopEventWriter writes one event per transaction to keep SQLite locks short.
type desktopEventWriter struct {
	mu sync.Mutex

	db                       data.DesktopDB
	bus                      eventbus.EventBus
	run                      data.Run
	traceID                  string
	model                    string
	runsRepo                 data.DesktopRunsRepository
	eventsRepo               data.DesktopRunEventsRepository
	projector                *subagentctl.SubAgentStateProjector
	assistantDeltas          []string
	lastTurnDeltaCount       int
	latestAssistantSeq       int64
	lastDraftFlushAt         time.Time
	responseDraftStore       objectstore.BlobStore
	assistantMessage         *llm.Message
	assistantMessageFresh    bool
	toolCallCount            int
	iterationCount           int
	completed                bool
	totalInputTokens         int64
	totalOutputTokens        int64
	totalCacheCreationTokens int64
	totalCacheReadTokens     int64
	totalCachedTokens        int64
	totalCostUSD             float64
	usageRepo                data.UsageRecordsRepository
	telegramBoundaryFlush    func(context.Context, string) error
	telegramSentOutputCount  int
	telegramProgressTracker  *pipeline.TelegramProgressTracker
	terminalUserMessage      string
	terminalStatus           string
	visibleAssistantText     string
	visibleAssistantTexts    []string
	pendingReplyOverride     string
	draftVisibleContent      string
	draftUseVisible          bool
	heartbeatRun             bool
	pendingToolCalls         []llm.ToolCall
	pendingToolResults       []desktopIntermediateMessage
	intermediateMessages     []desktopIntermediateMessage
}

type desktopIntermediateMessage struct {
	Role        string
	Content     string
	ContentJSON json.RawMessage
	ToolCallID  string
	Ordinal     int64
}

type pendingDesktopTelegramProgressCall struct {
	CallID   string
	ToolName string
	ArgsJSON string
}

func (w *desktopEventWriter) telegramStreamRemainder() string {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.telegramBoundaryFlush == nil {
		return ""
	}
	unsent := w.visibleAssistantTexts[w.telegramSentOutputCount:]
	if len(unsent) == 0 {
		return ""
	}
	return strings.TrimSpace(strings.Join(unsent, "\n"))
}

func (w *desktopEventWriter) pendingTelegramFlushChunk() string {
	if w.telegramBoundaryFlush == nil {
		return ""
	}
	unsent := w.visibleAssistantTexts[w.telegramSentOutputCount:]
	if len(unsent) == 0 {
		return ""
	}
	return strings.TrimSpace(strings.Join(unsent, "\n"))
}

func (w *desktopEventWriter) telegramUnsentOutputs() []string {
	if w.telegramSentOutputCount >= len(w.visibleAssistantTexts) {
		return nil
	}
	out := make([]string, 0, len(w.visibleAssistantTexts)-w.telegramSentOutputCount)
	for _, item := range w.visibleAssistantTexts[w.telegramSentOutputCount:] {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func (w *desktopEventWriter) flushTelegramBoundaryAndProgress(
	ctx context.Context,
	flushChunk string,
	progressCall *pendingDesktopTelegramProgressCall,
) error {
	if flushChunk != "" && w.telegramBoundaryFlush != nil {
		if err := w.telegramBoundaryFlush(ctx, flushChunk); err != nil {
			return err
		}
		w.telegramSentOutputCount = len(w.visibleAssistantTexts)
	}
	if progressCall != nil && w.telegramProgressTracker != nil {
		w.telegramProgressTracker.OnToolCall(ctx, progressCall.CallID, progressCall.ToolName, progressCall.ArgsJSON)
	}
	return nil
}

func (w *desktopEventWriter) append(ctx context.Context, runID uuid.UUID, ev events.RunEvent, personaID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	tx, err := w.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	if err := w.runsRepo.LockRunRow(ctx, tx, runID); err != nil {
		return err
	}

	if ev.Type == "run.route.selected" {
		if err := w.runsRepo.UpdateRunMetadata(ctx, tx, runID, w.model, personaID); err != nil {
			slog.Error("desktop_update_run_metadata",
				"run_id", runID.String(),
				"error", err.Error(),
			)
		}
	}

	cancelTypes := []string{"run.cancel_requested", "run.cancelled"}
	cancelType, err := w.eventsRepo.GetLatestEventType(ctx, tx, runID, cancelTypes)
	if err != nil {
		return err
	}
	if cancelType == "run.cancelled" {
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return errDesktopStopProcessing
	}
	if cancelType == "run.cancel_requested" {
		visibleOutput, err := loadDesktopVisibleAssistantOutput(ctx, tx, runID)
		if err != nil {
			return err
		}
		w.visibleAssistantText = visibleOutput
		w.draftVisibleContent = visibleOutput
		w.draftUseVisible = true
		nextRunIDs, err := w.transitionCancelled(ctx, tx, runID)
		if err != nil {
			return err
		}
		if err := w.maybeFlushResponseDraft(ctx, true); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		w.publishRunEvents(ctx)
		w.enqueueProjectedRuns(ctx, nextRunIDs)
		return errDesktopStopProcessing
	}

	eventSeq, err := w.eventsRepo.AppendRunEvent(ctx, tx, runID, ev)
	if err != nil {
		return err
	}
	if assistantMessage, ok := desktopAssistantMessageFromEventData(ev.DataJSON); ok {
		w.assistantMessage = &assistantMessage
		w.assistantMessageFresh = true
	}
	flushChunk := ""
	var pendingProgressCall *pendingDesktopTelegramProgressCall
	if ev.Type == "llm.turn.completed" {
		w.captureAssistantTurnOutput()
		flushChunk = w.pendingTelegramFlushChunk()
	}

	if shouldAccumulateUsageForDesktopEvent(ev.Type) {
		w.accumUsage(ev.DataJSON)
	}

	if ev.Type == "run.segment.start" && w.telegramProgressTracker != nil {
		segmentID, _ := ev.DataJSON["segment_id"].(string)
		kind, _ := ev.DataJSON["kind"].(string)
		display, _ := ev.DataJSON["display"].(map[string]any)
		mode, _ := display["mode"].(string)
		label, _ := display["label"].(string)
		w.telegramProgressTracker.OnRunSegmentStart(ctx, strings.TrimSpace(segmentID), strings.TrimSpace(kind), strings.TrimSpace(mode), strings.TrimSpace(label))
	}
	if ev.Type == "run.segment.end" && w.telegramProgressTracker != nil {
		segmentID, _ := ev.DataJSON["segment_id"].(string)
		w.telegramProgressTracker.OnRunSegmentEnd(ctx, segmentID)
	}

	if ev.Type == "tool.call" {
		w.captureChannelToolCallOutput(ev.DataJSON)
		flushChunk = w.pendingTelegramFlushChunk()
		w.toolCallCount++
		w.collectToolCall(ev.DataJSON)
		if w.telegramProgressTracker != nil {
			callID, _ := ev.DataJSON["tool_call_id"].(string)
			toolName, _ := ev.DataJSON["tool_name"].(string)
			toolName = llm.CanonicalToolName(toolName)
			argsRaw, _ := json.Marshal(ev.DataJSON["arguments"])
			pendingProgressCall = &pendingDesktopTelegramProgressCall{
				CallID:   callID,
				ToolName: toolName,
				ArgsJSON: string(argsRaw),
			}
		}
	}
	if ev.Type == "llm.request" {
		w.flushPendingToolCalls()
		w.iterationCount++
	}
	if ev.Type == "tool.result" {
		w.collectToolResult(ev.DataJSON)
		if w.telegramProgressTracker != nil {
			callID, _ := ev.DataJSON["tool_call_id"].(string)
			toolName, _ := ev.DataJSON["tool_name"].(string)
			toolName = llm.CanonicalToolName(toolName)
			errorClass := ""
			if ev.ErrorClass != nil {
				errorClass = *ev.ErrorClass
			}
			w.telegramProgressTracker.OnToolResult(ctx, callID, toolName, errorClass)
		}
	}

	if ev.Type == "message.delta" {
		if w.telegramProgressTracker != nil {
			role, _ := ev.DataJSON["role"].(string)
			channel, _ := ev.DataJSON["channel"].(string)
			if delta := desktopExtractDelta(ev.DataJSON); delta != "" {
				w.telegramProgressTracker.OnMessageDelta(ctx, role, channel, delta)
			}
		}
		if channel, _ := ev.DataJSON["channel"].(string); channel == "" {
			if delta := desktopExtractDelta(ev.DataJSON); delta != "" {
				w.assistantDeltas = append(w.assistantDeltas, delta)
				w.latestAssistantSeq = eventSeq
				if err := w.maybeFlushResponseDraft(ctx, false); err != nil {
					return err
				}
			}
		}
	}

	var nextRunIDs []uuid.UUID
	if status, ok := desktopTerminalStatuses[ev.Type]; ok {
		w.flushPendingToolCalls()
		if status == "completed" {
			w.completed = true
			w.terminalUserMessage = ""
		} else {
			w.terminalUserMessage = pipeline.TerminalStatusMessage(ev.DataJSON)
			w.visibleAssistantText = strings.Join(w.assistantDeltas, "")
			if err := w.maybeFlushResponseDraft(ctx, true); err != nil {
				return err
			}
		}
		w.terminalStatus = status
		if err := w.runsRepo.UpdateRunTerminalStatus(ctx, tx, runID, data.TerminalStatusUpdate{
			Status: status, TotalInputTokens: w.totalInputTokens, TotalOutputTokens: w.totalOutputTokens, TotalCostUSD: w.totalCostUSD,
		}); err != nil {
			return err
		}
		if err := w.usageRepo.Insert(ctx, tx, w.run.AccountID, runID, w.model,
			w.totalInputTokens, w.totalOutputTokens,
			w.totalCacheCreationTokens, w.totalCacheReadTokens, w.totalCachedTokens,
			w.totalCostUSD,
		); err != nil {
			return err
		}
		if w.projector != nil {
			projection, err := w.projector.ProjectRunTerminal(ctx, tx, w.run, status, ev.DataJSON, ev.ErrorClass)
			if err != nil {
				return err
			}
			if projection.NextRunID != nil {
				nextRunIDs = append(nextRunIDs, *projection.NextRunID)
			}
		}
	} else if err := w.runsRepo.TouchRunActivity(ctx, tx, w.run.ID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	w.publishRunEvents(ctx)
	if err := w.flushTelegramBoundaryAndProgress(ctx, flushChunk, pendingProgressCall); err != nil {
		return err
	}
	w.enqueueProjectedRuns(ctx, nextRunIDs)
	return nil
}

func (w *desktopEventWriter) collectToolCall(dataJSON map[string]any) {
	callID, _ := dataJSON["tool_call_id"].(string)
	toolName, _ := dataJSON["tool_name"].(string)
	toolName = llm.CanonicalToolName(toolName)
	if callID == "" || toolName == "" {
		return
	}
	args, _ := dataJSON["arguments"].(map[string]any)
	w.pendingToolCalls = append(w.pendingToolCalls, llm.ToolCall{
		ToolCallID:    callID,
		ToolName:      toolName,
		ArgumentsJSON: args,
	})
}

func (w *desktopEventWriter) flushPendingToolCalls() {
	if len(w.pendingToolCalls) == 0 {
		w.pendingToolResults = w.pendingToolResults[:0]
		return
	}

	resolved := make(map[string]struct{}, len(w.pendingToolResults))
	for _, result := range w.pendingToolResults {
		resolved[result.ToolCallID] = struct{}{}
	}
	filteredCalls := make([]llm.ToolCall, 0, len(w.pendingToolCalls))
	keptCallIDs := make(map[string]struct{}, len(w.pendingToolCalls))
	for _, call := range w.pendingToolCalls {
		if _, ok := resolved[call.ToolCallID]; ok {
			if w.heartbeatRun && pipeline.IsHeartbeatDecisionToolName(call.ToolName) {
				continue
			}
			filteredCalls = append(filteredCalls, call)
			keptCallIDs[call.ToolCallID] = struct{}{}
		}
	}

	w.pendingToolCalls = w.pendingToolCalls[:0]
	results := w.pendingToolResults
	w.pendingToolResults = w.pendingToolResults[:0]
	filteredResults := make([]desktopIntermediateMessage, 0, len(results))
	for _, result := range results {
		if _, ok := keptCallIDs[result.ToolCallID]; ok {
			filteredResults = append(filteredResults, result)
		}
	}
	msg := w.assistantMessage
	hasVisibleParts := msg != nil && len(llm.VisibleContentParts(msg.Content)) > 0
	if len(filteredCalls) == 0 && !hasVisibleParts {
		return
	}

	if msg == nil {
		msg = &llm.Message{Role: "assistant"}
	}
	contentJSON, err := llm.BuildIntermediateAssistantContentJSON(*msg, filteredCalls)
	if err != nil {
		return
	}
	baseOrdinal := int64(len(w.intermediateMessages)) + 1
	w.intermediateMessages = append(w.intermediateMessages, desktopIntermediateMessage{
		Role:        "assistant",
		Content:     llm.VisibleMessageText(*msg),
		ContentJSON: contentJSON,
		Ordinal:     baseOrdinal,
	})
	for i := range filteredResults {
		filteredResults[i].Ordinal = baseOrdinal + 1 + int64(i)
	}
	w.intermediateMessages = append(w.intermediateMessages, filteredResults...)
}

func (w *desktopEventWriter) collectToolResult(dataJSON map[string]any) {
	toolName, _ := dataJSON["tool_name"].(string)
	toolName = llm.CanonicalToolName(toolName)
	envelope := map[string]any{
		"tool_call_id": dataJSON["tool_call_id"],
		"tool_name":    toolName,
	}
	if v, ok := dataJSON["result"]; ok {
		envelope["result"] = v
	}
	if v, ok := dataJSON["error"]; ok {
		envelope["error"] = v
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return
	}
	callID, _ := dataJSON["tool_call_id"].(string)
	w.pendingToolResults = append(w.pendingToolResults, desktopIntermediateMessage{
		Role:       "tool",
		Content:    string(raw),
		ToolCallID: callID,
	})
}

func (w *desktopEventWriter) publishRunEvents(ctx context.Context) {
	if w.bus != nil {
		channel := fmt.Sprintf("run_events:%s", w.run.ID.String())
		_ = w.bus.Publish(ctx, channel, "")
	}
}

func (w *desktopEventWriter) transitionCancelled(ctx context.Context, tx pgx.Tx, runID uuid.UUID) ([]uuid.UUID, error) {
	emitter := events.NewEmitter(w.traceID)
	cancelled := emitter.Emit("run.cancelled", map[string]any{}, nil, nil)
	if _, err := w.eventsRepo.AppendRunEvent(ctx, tx, runID, cancelled); err != nil {
		return nil, err
	}
	var nextRunIDs []uuid.UUID
	if w.projector != nil {
		projection, err := w.projector.ProjectRunTerminal(ctx, tx, w.run, data.SubAgentStatusCancelled, map[string]any{"run_id": runID.String()}, nil)
		if err != nil {
			return nil, err
		}
		if projection.NextRunID != nil {
			nextRunIDs = append(nextRunIDs, *projection.NextRunID)
		}
	}
	if err := w.runsRepo.UpdateRunTerminalStatus(ctx, tx, runID, data.TerminalStatusUpdate{
		Status: "cancelled", TotalInputTokens: w.totalInputTokens, TotalOutputTokens: w.totalOutputTokens, TotalCostUSD: w.totalCostUSD,
	}); err != nil {
		return nil, err
	}
	if err := w.usageRepo.Insert(ctx, tx, w.run.AccountID, runID, w.model,
		w.totalInputTokens, w.totalOutputTokens,
		w.totalCacheCreationTokens, w.totalCacheReadTokens, w.totalCachedTokens,
		w.totalCostUSD,
	); err != nil {
		return nil, err
	}
	w.terminalUserMessage = ""
	w.terminalStatus = "cancelled"
	return nextRunIDs, nil
}

func (w *desktopEventWriter) visibleAssistantOutput() string {
	if len(w.visibleAssistantTexts) > 0 {
		return strings.Join(w.visibleAssistantTexts, "")
	}
	if strings.TrimSpace(w.visibleAssistantText) != "" {
		return strings.TrimSpace(w.visibleAssistantText)
	}
	return strings.Join(w.assistantDeltas, "")
}

func (w *desktopEventWriter) visibleAssistantOutputs() []string {
	if len(w.visibleAssistantTexts) == 0 {
		output := strings.TrimSpace(w.visibleAssistantOutput())
		if output == "" {
			return nil
		}
		return []string{output}
	}
	out := make([]string, 0, len(w.visibleAssistantTexts))
	for _, item := range w.visibleAssistantTexts {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (w *desktopEventWriter) captureAssistantTurnOutput() {
	text := ""
	if w.assistantMessageFresh && w.assistantMessage != nil {
		text = llm.VisibleMessageText(*w.assistantMessage)
	} else if w.lastTurnDeltaCount < len(w.assistantDeltas) {
		text = strings.Join(w.assistantDeltas[w.lastTurnDeltaCount:], "")
	}
	w.lastTurnDeltaCount = len(w.assistantDeltas)
	w.assistantMessageFresh = false
	if trimmed := strings.TrimSpace(text); trimmed != "" {
		w.visibleAssistantTexts = append(w.visibleAssistantTexts, trimmed)
		w.visibleAssistantText = strings.Join(w.visibleAssistantTexts, "")
	}
}

func (w *desktopEventWriter) captureChannelToolCallOutput(dataJSON map[string]any) {
	if dataJSON == nil {
		return
	}
	toolName, _ := dataJSON["tool_name"].(string)
	args, _ := dataJSON["arguments"].(map[string]any)
	if args == nil {
		return
	}

	switch strings.TrimSpace(toolName) {
	case "telegram_reply":
		if mid, _ := args["reply_to_message_id"].(string); strings.TrimSpace(mid) != "" {
			w.pendingReplyOverride = strings.TrimSpace(mid)
		}
	default:
		return
	}
}

func (w *desktopEventWriter) maybeFlushResponseDraft(ctx context.Context, force bool) error {
	if w.responseDraftStore == nil || w.run.ID == uuid.Nil || w.run.ThreadID == uuid.Nil {
		return nil
	}
	if w.latestAssistantSeq <= 0 || len(w.assistantDeltas) == 0 {
		return nil
	}
	if !force && !w.lastDraftFlushAt.IsZero() && time.Since(w.lastDraftFlushAt) < 400*time.Millisecond {
		return nil
	}
	content := strings.Join(w.assistantDeltas, "")
	useVisible := force && w.draftUseVisible
	if useVisible {
		content = w.draftVisibleContent
	}
	if err := pipeline.WriteResponseDraft(ctx, w.responseDraftStore, w.run.ID, w.run.ThreadID, content, w.latestAssistantSeq); err != nil {
		return err
	}
	w.lastDraftFlushAt = time.Now()
	if useVisible {
		w.draftUseVisible = false
		w.draftVisibleContent = ""
	}
	return nil
}

func (w *desktopEventWriter) finalizeCancelledIfRequested(ctx context.Context) (bool, error) {
	if w == nil || w.db == nil || w.run.ID == uuid.Nil {
		return false, nil
	}

	tx, err := w.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	if err := w.runsRepo.LockRunRow(ctx, tx, w.run.ID); err != nil {
		return false, err
	}

	cancelType, err := w.eventsRepo.GetLatestEventType(ctx, tx, w.run.ID, []string{"run.cancel_requested", "run.cancelled"})
	if err != nil {
		return false, err
	}
	switch cancelType {
	case "run.cancelled":
		if err := tx.Commit(ctx); err != nil {
			return false, err
		}
		return true, nil
	case "run.cancel_requested":
		nextRunIDs, err := w.transitionCancelled(ctx, tx, w.run.ID)
		if err != nil {
			return false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return false, err
		}
		w.publishRunEvents(ctx)
		w.enqueueProjectedRuns(ctx, nextRunIDs)
		return true, nil
	default:
		return false, nil
	}
}

func (w *desktopEventWriter) enqueueProjectedRuns(ctx context.Context, runIDs []uuid.UUID) {
	for _, nextRunID := range runIDs {
		if w.projector == nil {
			continue
		}
		if err := w.projector.EnqueueRun(ctx, w.run.AccountID, nextRunID, w.traceID, nil, nil); err != nil {
			if markErr := w.projector.MarkRunFailed(context.Background(), nextRunID, "failed to enqueue child run job"); markErr != nil {
				slog.Error("desktop_mark_child_run_failed",
					"run_id", nextRunID.String(),
					"enqueue_error", err.Error(),
					"mark_error", markErr.Error(),
				)
			}
		}
	}
}

func (w *desktopEventWriter) accumUsage(dataJSON map[string]any) {
	if dataJSON == nil {
		return
	}
	usage, ok := dataJSON["usage"].(map[string]any)
	if !ok {
		return
	}
	if v, ok := toDesktopInt64(usage["input_tokens"]); ok {
		w.totalInputTokens += v
	}
	if v, ok := toDesktopInt64(usage["output_tokens"]); ok {
		w.totalOutputTokens += v
	}
	if v, ok := toDesktopInt64(usage["cache_creation_input_tokens"]); ok {
		w.totalCacheCreationTokens += v
	}
	if v, ok := toDesktopInt64(usage["cache_read_input_tokens"]); ok {
		w.totalCacheReadTokens += v
	}
	if v, ok := toDesktopInt64(usage["cached_tokens"]); ok {
		w.totalCachedTokens += v
	}
	if cost, ok := dataJSON["cost"].(map[string]any); ok {
		if v, ok := toDesktopInt64(cost["amount_micros"]); ok {
			w.totalCostUSD += float64(v) / 1_000_000.0
			return
		}
	}
	if v, ok := toDesktopFloat64(usage["cost_usd"]); ok {
		w.totalCostUSD += v
	}
}

func shouldAccumulateUsageForDesktopEvent(eventType string) bool {
	switch eventType {
	case "run.completed", "run.failed", "run.cancelled", "run.interrupted":
		return false
	default:
		return true
	}
}

func (w *desktopEventWriter) assistantOutput() string {
	if w.assistantMessage != nil {
		return llm.VisibleMessageText(*w.assistantMessage)
	}
	return strings.Join(w.assistantDeltas, "")
}

func (w *desktopEventWriter) finalAssistantMessage() llm.Message {
	if w.assistantMessage != nil {
		return *w.assistantMessage
	}
	content := strings.Join(w.assistantDeltas, "")
	if strings.TrimSpace(content) == "" {
		content = w.visibleAssistantOutput()
	}
	if strings.TrimSpace(content) == "" {
		return llm.Message{Role: "assistant"}
	}
	return llm.Message{
		Role:    "assistant",
		Content: []llm.TextPart{{Text: content}},
	}
}

// --------------- helpers ---------------

// desktopWriteFailure writes a run.failed event and terminal status via DesktopDB.
func desktopWriteFailure(
	ctx context.Context,
	db data.DesktopDB,
	run data.Run,
	emitter events.Emitter,
	runsRepo data.DesktopRunsRepository,
	eventsRepo data.DesktopRunEventsRepository,
	errorClass string,
	message string,
	details map[string]any,
) error {
	return desktopWriteTerminalEvent(ctx, db, run, emitter, runsRepo, eventsRepo, "run.failed", errorClass, message, details)
}

func desktopPersistFinalAssistantOutput(
	ctx context.Context,
	db data.DesktopDB,
	rc *pipeline.RunContext,
	w *desktopEventWriter,
	runsRepo data.DesktopRunsRepository,
	eventsRepo data.DesktopRunEventsRepository,
) error {
	if rc == nil || w == nil {
		return nil
	}

	content := w.visibleAssistantOutput()
	hasStreamedChunks := w.telegramBoundaryFlush != nil && w.telegramSentOutputCount > 0
	shouldPersistAssistantOutput := (w.completed || w.terminalStatus == "cancelled" || w.terminalStatus == "interrupted") && strings.TrimSpace(content) != ""
	if !shouldPersistAssistantOutput || pipeline.ShouldSuppressHeartbeatOutput(rc, content) {
		return nil
	}

	if w.completed && w.terminalStatus == "completed" && len(w.intermediateMessages) > 0 {
		if err := w.batchInsertIntermediateMessages(ctx, db, rc.Run.AccountID, rc.Run.ThreadID, rc.Run.ID); err != nil {
			slog.ErrorContext(ctx, "desktop: persist intermediate messages failed", "run_id", rc.Run.ID, "err", err.Error())
		}
	}

	metadata := map[string]any{
		"completion_state": "incomplete",
		"finish_reason":    w.terminalStatus,
	}
	if w.completed {
		metadata["completion_state"] = "complete"
		metadata["finish_reason"] = "completed"
	}

	var persistErr error
	if hasStreamedChunks {
		remainder := w.telegramStreamRemainder()
		if strings.TrimSpace(remainder) != "" {
			metadata["stream_chunk"] = true
			persistErr = persistDesktopStreamChunkMessageWithMetadata(ctx, db, rc.Run, remainder, metadata)
		}
	} else {
		persistErr = desktopInsertAssistantMessage(ctx, db, rc.Run, w.finalAssistantMessage(), metadata)
	}
	if persistErr != nil {
		slog.ErrorContext(ctx, "desktop: persist assistant output failed", "run_id", rc.Run.ID, "err", persistErr.Error())
		if err := desktopWriteFailure(
			ctx,
			db,
			rc.Run,
			rc.Emitter,
			runsRepo,
			eventsRepo,
			"database.write_failed",
			"assistant output persistence failed",
			map[string]any{"reason": persistErr.Error()},
		); err != nil {
			return err
		}
		w.publishRunEvents(ctx)
		return nil
	}

	if err := pipeline.DeleteResponseDraft(ctx, rc.ResponseDraftStore, rc.Run.ID); err != nil {
		slog.WarnContext(ctx, "desktop: delete response draft failed", "err", err)
	}
	if w.completed {
		rc.FinalAssistantOutput = content
		rc.FinalAssistantOutputs = w.visibleAssistantOutputs()
		rc.TelegramStreamDeliveryRemainder = w.telegramStreamRemainder()
		if hasStreamedChunks {
			rc.FinalAssistantOutputs = w.telegramUnsentOutputs()
		}
		if w.pendingReplyOverride != "" {
			rc.ChannelReplyOverride = &pipeline.ChannelMessageRef{
				MessageID: w.pendingReplyOverride,
			}
		}
	}
	rc.ThreadPersistReady = true
	return nil
}

func (w *desktopEventWriter) batchInsertIntermediateMessages(
	ctx context.Context,
	db data.DesktopDB,
	accountID, threadID, runID uuid.UUID,
) error {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	if len(w.intermediateMessages) == 0 {
		return nil
	}
	startSeq, err := data.AllocateThreadSeqRange(ctx, tx, accountID, threadID, int64(len(w.intermediateMessages)))
	if err != nil {
		return err
	}
	repo := data.MessagesRepository{}
	for i, msg := range w.intermediateMessages {
		meta := map[string]any{
			"intermediate": true,
			"run_id":       runID.String(),
		}
		if msg.ToolCallID != "" {
			meta["tool_call_id"] = msg.ToolCallID
		}
		metadataJSON, _ := json.Marshal(meta)
		var contentJSON json.RawMessage
		if msg.Role != "tool" {
			contentJSON = msg.ContentJSON
		}
		threadSeq := startSeq + int64(i)
		if msg.Ordinal > 0 {
			threadSeq = startSeq + msg.Ordinal - 1
		}
		if _, err := repo.InsertIntermediateMessage(ctx, tx, accountID, threadID, threadSeq, msg.Role, msg.Content, contentJSON, metadataJSON, time.Now().UTC()); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func desktopWriteTerminalEvent(
	ctx context.Context,
	db data.DesktopDB,
	run data.Run,
	emitter events.Emitter,
	runsRepo data.DesktopRunsRepository,
	eventsRepo data.DesktopRunEventsRepository,
	eventType string,
	errorClass string,
	message string,
	details map[string]any,
) error {
	payload := map[string]any{
		"error_class": errorClass,
		"message":     message,
	}
	if len(details) > 0 {
		payload["details"] = details
	}
	terminal := emitter.Emit(eventType, payload, nil, pipeline.StringPtr(errorClass))

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("desktop write %s: begin tx: %w", eventType, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := eventsRepo.AppendRunEvent(ctx, tx, run.ID, terminal); err != nil {
		return err
	}
	status := desktopTerminalStatusForEvent(eventType)
	if err := runsRepo.UpdateRunTerminalStatus(ctx, tx, run.ID, data.TerminalStatusUpdate{Status: status}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func desktopTerminalStatusForEvent(eventType string) string {
	switch eventType {
	case "run.completed":
		return "completed"
	case "run.cancelled":
		return "cancelled"
	case "run.interrupted":
		return "interrupted"
	case "run.failed":
		return "failed"
	default:
		return "failed"
	}
}

func startDesktopRunCancelWatcher(
	ctx context.Context,
	db data.DesktopDB,
	runID uuid.UUID,
	cancel context.CancelFunc,
) func() {
	if db == nil || runID == uuid.Nil || cancel == nil {
		return func() {}
	}
	watchCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-watchCtx.Done():
				return
			case <-ticker.C:
				cancelType, err := readDesktopCancelEvent(watchCtx, db, runID)
				if err != nil {
					if watchCtx.Err() != nil {
						return
					}
					continue
				}
				if cancelType == "run.cancel_requested" || cancelType == "run.cancelled" {
					cancel()
					return
				}
			}
		}
	}()
	return func() {
		stop()
		<-done
	}
}

func readDesktopCancelEvent(ctx context.Context, db data.DesktopDB, runID uuid.UUID) (string, error) {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck
	return (data.DesktopRunEventsRepository{}).GetLatestEventType(ctx, tx, runID, []string{"run.cancel_requested", "run.cancelled"})
}

func desktopInsertAssistantMessage(ctx context.Context, db data.DesktopDB, run data.Run, message llm.Message, metadata map[string]any) error {
	content := llm.VisibleMessageText(message)
	if db == nil || strings.TrimSpace(content) == "" {
		return nil
	}
	contentJSON, err := llm.BuildAssistantThreadContentJSON(message)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck
	if _, err := (data.MessagesRepository{}).InsertAssistantMessageWithMetadata(ctx, tx, run.AccountID, run.ThreadID, run.ID, content, contentJSON, false, metadata); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func persistDesktopStreamChunkMessage(ctx context.Context, db data.DesktopDB, run data.Run, text string) error {
	return persistDesktopStreamChunkMessageWithMetadata(ctx, db, run, text, map[string]any{"stream_chunk": true})
}

func persistDesktopStreamChunkMessageWithMetadata(ctx context.Context, db data.DesktopDB, run data.Run, text string, metadata map[string]any) error {
	if db == nil || strings.TrimSpace(text) == "" {
		return nil
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck
	if _, err := (data.MessagesRepository{}).InsertAssistantMessageWithMetadata(
		ctx, tx,
		run.AccountID, run.ThreadID, run.ID,
		text, nil, false,
		metadata,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func desktopAssistantMessageFromEventData(dataJSON map[string]any) (llm.Message, bool) {
	raw, ok := dataJSON["assistant_message"].(map[string]any)
	if !ok || raw == nil {
		return llm.Message{}, false
	}
	message, err := llm.MessageFromJSONMap(raw)
	if err != nil {
		return llm.Message{}, false
	}
	return message, true
}

func desktopExtractDelta(dataJSON map[string]any) string {
	if channel, _ := dataJSON["channel"].(string); strings.TrimSpace(channel) != "" {
		return ""
	}
	role, ok := dataJSON["role"]
	if ok && role != nil && role != "assistant" {
		return ""
	}
	delta, _ := dataJSON["content_delta"].(string)
	if strings.TrimSpace(delta) == "<end_turn>" {
		return ""
	}
	return delta
}

func loadDesktopVisibleAssistantOutput(ctx context.Context, tx pgx.Tx, runID uuid.UUID) (string, error) {
	cutoff, err := loadDesktopVisibleSeqCutoff(ctx, tx, runID)
	if err != nil {
		return "", err
	}
	query := `SELECT data_json FROM run_events WHERE run_id = $1 AND type = 'message.delta'`
	args := []any{runID}
	if cutoff > 0 {
		query += ` AND seq <= $2`
		args = append(args, cutoff)
	}
	query += ` ORDER BY seq ASC`
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var builder strings.Builder
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return "", err
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			continue
		}
		if delta := desktopExtractDelta(payload); delta != "" {
			builder.WriteString(delta)
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return builder.String(), nil
}

func loadDesktopVisibleSeqCutoff(ctx context.Context, tx pgx.Tx, runID uuid.UUID) (int64, error) {
	var raw []byte
	err := tx.QueryRow(ctx,
		`SELECT data_json FROM run_events
		 WHERE run_id = $1 AND type = 'run.cancel_requested'
		 ORDER BY seq ASC LIMIT 1`,
		runID,
	).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	if len(raw) == 0 {
		return 0, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0, nil
	}
	switch value := payload["visible_seq_cutoff"].(type) {
	case float64:
		return int64(value), nil
	case json.Number:
		return value.Int64()
	case int64:
		return value, nil
	case int:
		return int64(value), nil
	default:
		return 0, nil
	}
}

func toDesktopInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	case int64:
		return n, true
	case int:
		return int64(n), true
	}
	return 0, false
}

func toDesktopFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

func derefStr(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func desktopSubAgentSchemaAvailable(ctx context.Context, db data.DesktopDB) bool {
	if db == nil {
		return false
	}
	const requiredTables = 4
	var count int
	err := db.QueryRow(ctx,
		`SELECT COUNT(*) FROM sqlite_master
		 WHERE type = 'table'
		   AND name IN ('sub_agents', 'sub_agent_events', 'sub_agent_pending_inputs', 'sub_agent_context_snapshots')`,
	).Scan(&count)
	return err == nil && count == requiredTables
}

func cloneDesktopMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

// splitDesktopModelSelector splits "credName^modelName" into parts.
// Returns (credName, modelName, true) for exact selectors, ("", selector, false) otherwise.
func splitDesktopModelSelector(selector string) (string, string, bool) {
	parts := strings.SplitN(strings.TrimSpace(selector), "^", 2)
	if len(parts) != 2 {
		return "", strings.TrimSpace(selector), false
	}
	left := strings.TrimSpace(parts[0])
	right := strings.TrimSpace(parts[1])
	if left == "" || right == "" {
		return "", strings.TrimSpace(selector), false
	}
	return left, right, true
}

func resolveDesktopSelectedRouteBySelector(cfg routing.ProviderRoutingConfig, selector string) (*routing.SelectedProviderRoute, error) {
	credentialName, modelName, exact := splitDesktopModelSelector(selector)
	if exact {
		route, cred, ok := cfg.GetHighestPriorityRouteByCredentialAndModel(credentialName, modelName, map[string]any{})
		if !ok {
			return nil, fmt.Errorf("route not found for selector: %s", selector)
		}
		return &routing.SelectedProviderRoute{Route: route, Credential: cred}, nil
	}

	route, cred, ok := cfg.GetHighestPriorityRouteByModel(selector, map[string]any{})
	if !ok {
		return nil, fmt.Errorf("route not found for selector: %s", selector)
	}
	return &routing.SelectedProviderRoute{Route: route, Credential: cred}, nil
}

func canonicalDesktopRouteModel(providerKind routing.ProviderKind, model string) string {
	model = strings.TrimSpace(model)
	if providerKind != routing.ProviderKindGemini {
		return model
	}
	for {
		lowerModel := strings.ToLower(model)
		if !strings.HasPrefix(lowerModel, "models/") {
			return model
		}
		model = strings.TrimSpace(model[len("models/"):])
	}
}

// loadDesktopRoutingConfig builds a ProviderRoutingConfig from the SQLite
// llm_credentials, llm_routes, and secrets tables.
// All queries run inside a single read-only transaction to avoid deadlocking
// the single SQLite connection (MaxOpenConns=1).
func loadDesktopRoutingConfig(ctx context.Context, db data.DesktopDB) (routing.ProviderRoutingConfig, error) {
	keyRing, err := desktop.LoadEncryptionKeyRing(desktop.KeyRingOptions{})
	if err != nil {
		return routing.ProviderRoutingConfig{}, fmt.Errorf("load encryption key: %w", err)
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return routing.ProviderRoutingConfig{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	type credRaw struct {
		id, provider, name, advancedStr, ownerKind string
		secretID, baseURL, openAIMode              *string
	}
	credRows, err := tx.Query(ctx,
		`SELECT id, provider, name, secret_id, base_url, openai_api_mode, advanced_json, owner_kind
		 FROM llm_credentials WHERE revoked_at IS NULL`)
	if err != nil {
		return routing.ProviderRoutingConfig{}, fmt.Errorf("query llm_credentials: %w", err)
	}
	var rawCreds []credRaw
	for credRows.Next() {
		var c credRaw
		if err := credRows.Scan(&c.id, &c.provider, &c.name, &c.secretID, &c.baseURL, &c.openAIMode, &c.advancedStr, &c.ownerKind); err != nil {
			credRows.Close()
			return routing.ProviderRoutingConfig{}, fmt.Errorf("scan llm_credentials: %w", err)
		}
		rawCreds = append(rawCreds, c)
	}
	credRows.Close()

	var creds []routing.ProviderCredential
	credMap := map[string]routing.ProviderCredential{}
	for _, c := range rawCreds {
		var apiKey *string
		if c.secretID != nil && *c.secretID != "" {
			var encVal string
			var keyVer int
			if err := tx.QueryRow(ctx, `SELECT encrypted_value, key_version FROM secrets WHERE id = $1`, *c.secretID).Scan(&encVal, &keyVer); err != nil {
				slog.WarnContext(ctx, "desktop: skip credential, secret not found", "cred_id", c.id, "err", err)
				continue
			}
			plain, err := decryptDesktopCiphertext(keyRing, encVal, keyVer)
			if err != nil {
				slog.WarnContext(ctx, "desktop: skip credential, decrypt failed", "cred_id", c.id, "err", err)
				continue
			}
			apiKey = &plain
		}

		var advanced map[string]any
		if c.advancedStr != "" && c.advancedStr != "{}" {
			if err := json.Unmarshal([]byte(c.advancedStr), &advanced); err != nil {
				slog.WarnContext(ctx, "desktop: failed to parse credential advanced_json", "cred_id", c.id, "err", err)
			}
		}
		scope := routing.CredentialScopePlatform
		cred := routing.ProviderCredential{
			ID: c.id, Name: c.name, OwnerKind: scope,
			ProviderKind: routing.ProviderKind(c.provider),
			APIKeyValue:  apiKey, BaseURL: c.baseURL, OpenAIMode: c.openAIMode, AdvancedJSON: advanced,
		}
		creds = append(creds, cred)
		credMap[c.id] = cred
	}
	if len(creds) == 0 {
		return routing.ProviderRoutingConfig{}, fmt.Errorf("no active credentials found in database")
	}

	routeRows, err := tx.Query(ctx,
		`SELECT id, credential_id, model, priority, is_default, when_json, advanced_json,
		        multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read
		 FROM llm_routes ORDER BY priority DESC`)
	if err != nil {
		return routing.ProviderRoutingConfig{}, fmt.Errorf("query llm_routes: %w", err)
	}
	var routes []routing.ProviderRouteRule
	defaultRouteID := ""
	for routeRows.Next() {
		var (
			id, credentialID, model, whenStr, advancedStr string
			priority, isDefault                           int
			multiplier                                    float64
			costIn, costOut, costCW, costCR               *float64
		)
		if err := routeRows.Scan(&id, &credentialID, &model, &priority, &isDefault,
			&whenStr, &advancedStr, &multiplier, &costIn, &costOut, &costCW, &costCR); err != nil {
			routeRows.Close()
			return routing.ProviderRoutingConfig{}, fmt.Errorf("scan llm_routes: %w", err)
		}
		cred, ok := credMap[credentialID]
		if !ok {
			continue
		}
		model = canonicalDesktopRouteModel(cred.ProviderKind, model)
		var when, adv map[string]any
		if whenStr != "" && whenStr != "{}" {
			if err := json.Unmarshal([]byte(whenStr), &when); err != nil {
				slog.WarnContext(ctx, "desktop: failed to parse route when_json", "route_id", id, "err", err)
			}
		}
		if advancedStr != "" && advancedStr != "{}" {
			if err := json.Unmarshal([]byte(advancedStr), &adv); err != nil {
				slog.WarnContext(ctx, "desktop: failed to parse route advanced_json", "route_id", id, "err", err)
			}
		}
		if multiplier <= 0 {
			multiplier = 1.0
		}
		routes = append(routes, routing.ProviderRouteRule{
			ID: id, Model: model, CredentialID: credentialID,
			When: when, AdvancedJSON: adv, Multiplier: multiplier,
			CostPer1kInput: costIn, CostPer1kOutput: costOut,
			CostPer1kCacheWrite: costCW, CostPer1kCacheRead: costCR,
			Priority: priority,
		})
		if isDefault != 0 && defaultRouteID == "" {
			defaultRouteID = id
		}
	}
	routeRows.Close()
	tx.Rollback(ctx)

	if len(routes) == 0 {
		return routing.ProviderRoutingConfig{}, fmt.Errorf("no routes found in database")
	}
	if defaultRouteID == "" {
		defaultRouteID = routes[0].ID
	}

	slog.Info("desktop: loaded routing config from DB", "credentials", len(creds), "routes", len(routes), "default_route", defaultRouteID)
	return routing.ProviderRoutingConfig{
		DefaultRouteID: defaultRouteID,
		Credentials:    creds,
		Routes:         routes,
	}, nil
}

func decryptDesktopCiphertext(keyRing *sharedencryption.KeyRing, encoded string, keyVersion int) (string, error) {
	if keyRing == nil {
		return "", fmt.Errorf("desktop key ring must not be nil")
	}
	plain, err := keyRing.Decrypt(encoded, keyVersion)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func recoverOrphanRuns(ctx context.Context, db data.DesktopDB) error {
	if db == nil {
		return nil
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck
	tag, err := tx.Exec(ctx,
		`UPDATE runs SET status = 'interrupted', updated_at = datetime('now')
		 WHERE status IN ('running', 'queued')`)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if n := tag.RowsAffected(); n > 0 {
		slog.InfoContext(ctx, "desktop: recovered orphan runs", "count", n)
	}
	return nil
}
