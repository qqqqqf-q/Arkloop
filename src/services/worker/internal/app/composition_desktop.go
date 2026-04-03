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
	"strings"
	"sync"
	"time"

	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/shared/desktop"
	sharedencryption "arkloop/services/shared/encryption"
	"arkloop/services/shared/eventbus"
	sharedexec "arkloop/services/shared/executionconfig"
	"arkloop/services/shared/objectstore"
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

	// Bind both tool names to DynamicShellExecutor (they share the same names)
	executors[localshell.ExecCommandAgentSpec.Name] = shellExec
	executors[localshell.WriteStdinAgentSpec.Name] = shellExec

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

	memEnabled := strings.TrimSpace(os.Getenv("ARKLOOP_MEMORY_ENABLED")) != "false"
	ovURL := strings.TrimSpace(os.Getenv("ARKLOOP_OPENVIKING_BASE_URL"))
	ovKey := strings.TrimSpace(os.Getenv("ARKLOOP_OPENVIKING_ROOT_API_KEY"))

	var notebookProvider memory.MemoryProvider
	var memProvider memory.MemoryProvider
	useOV := false
	if memEnabled {
		notebookProvider = localmemory.NewProvider(db)
		slog.Info("desktop: notebook enabled")
	}
	if memEnabled && ovURL != "" {
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

	// Use localshell specs for LLM; DynamicShellExecutor routes to correct backend at runtime
	shellLlmSpecs := localshell.LlmSpecs()
	allLlmSpecs := append(builtin.LlmSpecs(), shellLlmSpecs...)
	allLlmSpecs = append(allLlmSpecs, conversationtool.LlmSpecs()...)
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
		mergedRT = mergedRT.WithMergedBuiltinToolNames(
			"memory_search", "memory_read", "memory_write", "memory_edit", "memory_forget",
		)
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

	if err := cleanupOrphanSkillRuntimes(ctx, db); err != nil {
		slog.WarnContext(ctx, "desktop: orphan skill runtime cleanup failed", "err", err.Error())
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
	}, nil
}

func loadPersonaRegistryFromFS() func() *personas.Registry {
	dirs := make([]string, 0, 4)
	if root, err := personas.BuiltinPersonasRoot(); err == nil && strings.TrimSpace(root) != "" {
		dirs = append(dirs, root)
	}
	dirs = append(dirs, "personas", "src/personas", "../personas")
	seen := make(map[string]struct{}, len(dirs))
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
			return func() *personas.Registry { return reg }
		}
	}
	return nil
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

	rc := &pipeline.RunContext{
		Run:                 run,
		DB:                  e.db,
		RunStatusDB:         runsRepo,
		Pool:                nil,
		MemoryServiceDB:     e.db,
		MemorySnapshotStore: pipeline.NewDesktopMemorySnapshotStore(e.db),
		EventBus:            e.bus,
		TraceID:             traceID,
		Emitter:             emitter,
		Router:              e.auxRouter,
		Runtime:             e.runtimeSnapshot,

		ExecutorBuilder:     e.executorRegistry,
		ToolBudget:          map[string]any{},
		PerToolSoftLimits:   tools.DefaultPerToolSoftLimits(),
		PendingMemoryWrites: memory.NewPendingWriteBuffer(),

		LlmRetryMaxAttempts: 10,
		LlmRetryBaseDelayMs: 1000,

		ThreadMessageHistoryLimit:     200,
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
	if e.useOV {
		notebookMW := desktopMemoryInjection(e.db)
		memoryMW := pipeline.NewMemoryMiddleware(
			e.memProvider,
			pipeline.NewDesktopMemorySnapshotStore(e.db),
			e.db,
			e.promptInjection.Resolver,
		)
		memMiddleware = func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
			return notebookMW(ctx, rc, func(ctx context.Context, rc *pipeline.RunContext) error {
				return memoryMW(ctx, rc, next)
			})
		}
	} else {
		// Local SQLite: lightweight snapshot injection
		memMiddleware = desktopMemoryInjection(e.db)
	}

	middlewares := []pipeline.RunMiddleware{
		desktopCancelGuard(e.db, e.bus),
		desktopInputLoader(e.db, runsRepo, eventsRepo, e.messageAttachmentStore, e.rolloutStore),
		pipeline.NewHeartbeatScheduleMiddleware(e.db),
		pipeline.NewMCPDiscoveryMiddleware(
			nil,
			func(*pipeline.RunContext) mcp.DiscoveryQueryer { return e.db },
			e.toolExecutors,
			e.allLlmSpecs,
			e.baseAllowlist,
			e.toolRegistry,
		),
		desktopToolProviderBindings(e.db),
		pipeline.NewSpawnAgentMiddleware(),
		desktopPersonaResolution(e.db, e.personaRegistry, runsRepo, eventsRepo),
		desktopChannelContext(e.db),
		pipeline.NewChannelAdminTagMiddleware(e.db),
		pipeline.NewChannelTelegramGroupUserMergeMiddleware(),
		pipeline.NewChannelGroupContextTrimMiddleware(),
		pipeline.NewChannelTelegramToolsMiddleware(&desktopTelegramTokenLoader{db: e.db}, nil),
		desktopSubAgentContext(e.db, subagentctl.NewSnapshotStorage()),
		pipeline.NewSkillContextMiddleware(pipeline.SkillContextConfig{
			Resolve:        desktopSkillResolver(e.db),
			Prepare:        desktopSkillPreparer(e.useVM),
			LayoutResolver: e.skillLayout,
			ExternalDirs:   desktopExternalSkillDirs(e.db),
		}),
	}
	middlewares = append(middlewares, desktopCapabilityMiddlewares(memMiddleware, e.promptInjection, eventsRepo)...)
	middlewares = append(middlewares,
		desktopRouting(e.auxRouter, e.auxGateway, e.emitDebugEvents, e.db, runsRepo, eventsRepo),
		pipeline.NewTitleSummarizerMiddleware(e.db, nil, e.auxGateway, e.emitDebugEvents, e.routingLoader),
		pipeline.NewContextCompactMiddleware(e.db, data.MessagesRepository{}, data.DesktopRunEventsRepository{}, e.auxGateway, e.emitDebugEvents, e.routingLoader),
		pipeline.NewHeartbeatPrepareMiddleware(),
		pipeline.NewConditionalToolsMiddleware(),
		pipeline.NewToolBuildMiddleware(),
		pipeline.NewResultSummarizerMiddleware(nil, e.auxGateway, e.emitDebugEvents, 0, e.routingLoader),
		desktopChannelDelivery(e.db),
	)
	terminal := desktopAgentLoop(e.db, e.bus, e.jobQueue, runsRepo, eventsRepo)
	handler := pipeline.Build(middlewares, terminal)

	return handler(ctx, rc)
}

func desktopCapabilityMiddlewares(
	memMiddleware pipeline.RunMiddleware,
	promptInjection securitycap.Runtime,
	eventsRepo data.RunEventStore,
) []pipeline.RunMiddleware {
	middlewares := []pipeline.RunMiddleware{
		memMiddleware,
		pipeline.NewRuntimeContextMiddleware(),
	}
	return append(middlewares, promptInjection.Middlewares(eventsRepo)...)
}

func resolveDesktopRunBindings(ctx context.Context, db data.DesktopDB, run data.Run) (data.Run, error) {
	if db == nil {
		return run, fmt.Errorf("desktop db must not be nil")
	}
	return environmentbindings.ResolveAndPersistRun(ctx, db, run)
}

// --------------- desktop middleware ---------------

// desktopMemoryInjection reads the saved notebook block from
// user_notebook_snapshots and appends it to the run's system prompt.
func desktopMemoryInjection(db data.DesktopDB) pipeline.RunMiddleware {
	return func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		if rc.UserID == nil || db == nil {
			return next(ctx, rc)
		}
		provider := localmemory.NewProvider(db)
		block, err := provider.GetSnapshot(ctx, rc.Run.AccountID, *rc.UserID, pipeline.StableAgentID(rc))
		if err == nil && strings.TrimSpace(block) != "" {
			notebookBlock := strings.TrimSpace(block)
			if strings.TrimSpace(rc.SystemPrompt) != "" {
				rc.SystemPrompt = rc.SystemPrompt + "\n\n" + notebookBlock
			} else {
				rc.SystemPrompt = notebookBlock
			}
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
	var rawJSON []byte
	var seq int64
	err := db.QueryRow(
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
		messageLimit := rc.ThreadMessageHistoryLimit
		if messageLimit <= 0 {
			messageLimit = 200
		}
		loaded, err := pipeline.LoadRunInputs(ctx, db, rc.Run, rc.JobPayload, runsRepo, eventsRepo, data.MessagesRepository{}, attachmentStore, rolloutStore, messageLimit)
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
		rc.HasActiveCompactSnapshot = loaded.HasActiveCompactSnapshot
		rc.ActiveCompactSnapshotText = loaded.ActiveCompactSnapshotText

		return next(ctx, rc)
	}
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
		if rc == nil || len(rc.JobPayload) == 0 {
			return next(ctx, rc)
		}
		rawDelivery, ok := rc.JobPayload["channel_delivery"].(map[string]any)
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
		rc.ChannelContext = channelCtx
		rc.ChannelToolSurface = pipeline.NewChannelToolSurfaceFromContext(channelCtx)
		if channelCtx.SenderUserID != nil {
			rc.UserID = channelCtx.SenderUserID
		}
		return next(ctx, rc)
	}
}

func desktopChannelDelivery(db data.DesktopDB) pipeline.RunMiddleware {
	client := telegrambot.NewClient(os.Getenv("ARKLOOP_TELEGRAM_BOT_API_BASE_URL"), nil)
	discordClient := &http.Client{Timeout: 10 * time.Second}

	return func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		var preloaded *desktopDeliveryChannelRecord
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
				streamMidCount++
				return nil
			}
			rc.TelegramToolBoundaryFlush = streamFlush
		}

		var stopTyping context.CancelFunc
		if preloaded != nil && ux.TypingIndicator && strings.TrimSpace(preloaded.Token) != "" && !pipeline.IsHeartbeatRunContext(rc) {
			stopTyping = pipeline.StartTelegramTypingRefresh(ctx, client, preloaded.Token, rc.ChannelContext.Conversation.Target)
		}

		err := next(ctx, rc)
		if rc != nil {
			rc.TelegramToolBoundaryFlush = nil
		}
		if stopTyping != nil {
			stopTyping()
		}

		if err != nil || rc == nil || rc.ChannelContext == nil {
			return err
		}
		channelType = desktopNormalizedChannelType(rc)
		if db == nil || (channelType != "telegram" && channelType != "discord") {
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
		if streamFlush != nil {
			finalOutputs = nil
		}

		channel := preloaded
		var lookupErr error
		if channel == nil {
			channel, lookupErr = loadDesktopDeliveryChannel(ctx, db, rc.ChannelContext.ChannelID)
		}
		if lookupErr != nil {
			recordDesktopChannelDeliveryFailure(db, rc.Run.ID, lookupErr)
			slog.WarnContext(ctx, "desktop channel delivery lookup failed", "run_id", rc.Run.ID, "err", lookupErr.Error())
			return err
		}
		if channel == nil {
			recordDesktopChannelDeliveryFailure(db, rc.Run.ID, fmt.Errorf("channel not found or inactive"))
			return err
		}
		switch channelType {
		case "telegram":
			uxSend := pipeline.ParseTelegramChannelUX(channel.ConfigJSON)
			if finalRecordErr := deliverDesktopTelegramChannelOutputs(ctx, db, rc, client, channel, output, finalOutputs); finalRecordErr != nil {
				recordDesktopChannelDeliveryFailure(db, rc.Run.ID, finalRecordErr)
				slog.WarnContext(ctx, "desktop telegram channel delivery failed", "run_id", rc.Run.ID, "err", finalRecordErr.Error())
				return err
			}
			if strings.TrimSpace(uxSend.ReactionEmoji) != "" {
				pipeline.MaybeTelegramInboundReaction(ctx, client, channel.Token, rc, uxSend.ReactionEmoji)
			}
		case "discord":
			if finalRecordErr := deliverDesktopDiscordChannelOutput(ctx, db, rc, discordClient, channel, output); finalRecordErr != nil {
				recordDesktopChannelDeliveryFailure(db, rc.Run.ID, finalRecordErr)
				slog.WarnContext(ctx, "desktop discord channel delivery failed", "run_id", rc.Run.ID, "err", finalRecordErr.Error())
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
	for _, item := range outputs {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		messageIDs, err := sender.SendText(ctx, pipeline.ChannelDeliveryTarget{
			ChannelType:  rc.ChannelContext.ChannelType,
			Conversation: rc.ChannelContext.Conversation,
			ReplyTo:      replyTo,
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
			replyTo,
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
	if rc.HeartbeatRun {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(rc.ChannelContext.ConversationType), "private") ||
		strings.EqualFold(strings.TrimSpace(rc.ChannelContext.ConversationType), "dm") {
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
	defer tx.Rollback(context.Background()) //nolint:errcheck

	repo := data.DesktopRunEventsRepository{}
	if _, appendErr := repo.AppendEvent(context.Background(), tx, runID, "run.channel_delivery_failed", map[string]any{
		"error": err.Error(),
	}, nil, nil); appendErr != nil {
		return
	}
	_ = tx.Commit(context.Background())
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
	defer tx.Rollback(ctx) //nolint:errcheck

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
		rc.SystemPrompt = profile.SystemPrompt
		rc.ReasoningIterations = profile.ReasoningIterations
		rc.ToolContinuationBudget = profile.ToolContinuationBudget
		rc.MaxOutputTokens = profile.MaxOutputTokens
		rc.Temperature = profile.Temperature
		rc.TopP = profile.TopP
		rc.ReasoningMode = profile.ReasoningMode
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

		return next(ctx, rc)
	}
}

func desktopGatewayFromRoute(selected routing.SelectedProviderRoute, stub llm.Gateway, debug bool, maxBytes int) (llm.Gateway, error) {
	return pipeline.GatewayFromSelectedRoute(selected, stub, debug, maxBytes)
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
) pipeline.RunHandler {
	return func(ctx context.Context, rc *pipeline.RunContext) error {
		selected := rc.SelectedRoute
		var projector *subagentctl.SubAgentStateProjector
		if desktopSubAgentSchemaAvailable(ctx, db) {
			projector = subagentctl.NewSubAgentStateProjector(db, nil, jobQueue)
		}

		w := &desktopEventWriter{
			db:                    db,
			bus:                   bus,
			run:                   rc.Run,
			traceID:               rc.TraceID,
			model:                 selected.Route.Model,
			runsRepo:              runsRepo,
			eventsRepo:            eventsRepo,
			projector:             projector,
			usageRepo:             data.UsageRecordsRepository{},
			responseDraftStore:    rc.ResponseDraftStore,
			telegramBoundaryFlush: rc.TelegramToolBoundaryFlush,
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
			_ = w.append(ctx, rc.Run.ID, failed, "")
			rc.ChannelTerminalNotice = strings.TrimSpace(w.terminalUserMessage)
			return nil
		}

		execCtx, cancelExec := context.WithCancel(ctx)
		defer cancelExec()
		stopCancelWatch := startDesktopRunCancelWatcher(execCtx, db, rc.Run.ID, cancelExec)
		defer stopCancelWatch()

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
		content := w.visibleAssistantOutput()
		shouldPersistAssistantOutput := (w.completed || w.terminalStatus == "cancelled" || w.terminalStatus == "interrupted") && strings.TrimSpace(content) != ""
		if shouldPersistAssistantOutput && !pipeline.ShouldSuppressHeartbeatOutput(rc, content) {
			metadata := map[string]any{
				"completion_state": "incomplete",
				"finish_reason":    w.terminalStatus,
			}
			if w.completed {
				metadata["completion_state"] = "complete"
				metadata["finish_reason"] = "completed"
			}
			if err := desktopInsertAssistantMessage(ctx, db, rc.Run, w.finalAssistantMessage(), metadata); err != nil {
				slog.WarnContext(ctx, "desktop: insert assistant message failed", "err", err)
			}
			if err := pipeline.DeleteResponseDraft(ctx, rc.ResponseDraftStore, rc.Run.ID); err != nil {
				slog.WarnContext(ctx, "desktop: delete response draft failed", "err", err)
			}
			if w.completed {
				rc.FinalAssistantOutput = content
				rc.FinalAssistantOutputs = w.visibleAssistantOutputs()
				rc.TelegramStreamDeliveryRemainder = w.telegramStreamRemainder()
			}
		}
		rc.RunToolCallCount = w.toolCallCount
		rc.RunIterationCount = w.iterationCount
		return nil
	}
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
	telegramFlushSentDeltas  int
	terminalUserMessage      string
	terminalStatus           string
	visibleAssistantText     string
	visibleAssistantTexts    []string
	draftVisibleContent      string
	draftUseVisible          bool
}

func (w *desktopEventWriter) telegramStreamRemainder() string {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.telegramBoundaryFlush == nil {
		return ""
	}
	if w.telegramFlushSentDeltas >= len(w.assistantDeltas) {
		return ""
	}
	return strings.TrimSpace(strings.Join(w.assistantDeltas[w.telegramFlushSentDeltas:], ""))
}

func (w *desktopEventWriter) append(ctx context.Context, runID uuid.UUID, ev events.RunEvent, personaID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	tx, err := w.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := w.runsRepo.LockRunRow(ctx, tx, runID); err != nil {
		return err
	}

	if ev.Type == "run.route.selected" && personaID != "" {
		_ = w.runsRepo.UpdateRunMetadata(ctx, tx, runID, w.model, personaID)
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
	if ev.Type == "llm.turn.completed" {
		w.captureAssistantTurnOutput()
	}

	if shouldAccumulateUsageForDesktopEvent(ev.Type) {
		w.accumUsage(ev.DataJSON)
	}

	flushChunk := ""
	if ev.Type == "tool.call" {
		if w.telegramBoundaryFlush != nil && len(w.assistantDeltas) > w.telegramFlushSentDeltas {
			flushChunk = strings.TrimSpace(strings.Join(w.assistantDeltas[w.telegramFlushSentDeltas:], ""))
		}
		w.toolCallCount++
	}
	if ev.Type == "llm.request" {
		w.iterationCount++
	}
	if ev.Type == "message.delta" {
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
			nextRunID, err := w.projector.ProjectRunTerminal(ctx, tx, w.run, status, ev.DataJSON, ev.ErrorClass)
			if err != nil {
				return err
			}
			if nextRunID != nil {
				nextRunIDs = append(nextRunIDs, *nextRunID)
			}
		}
	} else if err := w.runsRepo.TouchRunActivity(ctx, tx, w.run.ID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	w.publishRunEvents(ctx)
	if flushChunk != "" && w.telegramBoundaryFlush != nil {
		if err := w.telegramBoundaryFlush(ctx, flushChunk); err != nil {
			return err
		}
		w.telegramFlushSentDeltas = len(w.assistantDeltas)
	}
	w.enqueueProjectedRuns(ctx, nextRunIDs)
	return nil
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
		nextRunID, err := w.projector.ProjectRunTerminal(ctx, tx, w.run, data.SubAgentStatusCancelled, map[string]any{"run_id": runID.String()}, nil)
		if err != nil {
			return nil, err
		}
		if nextRunID != nil {
			nextRunIDs = append(nextRunIDs, *nextRunID)
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
	defer tx.Rollback(ctx) //nolint:errcheck

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
			_ = w.projector.MarkRunFailed(context.Background(), nextRunID, "failed to enqueue child run job")
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
	defer tx.Rollback(ctx)

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
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
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
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := (data.MessagesRepository{}).InsertAssistantMessageWithMetadata(ctx, tx, run.AccountID, run.ThreadID, run.ID, content, contentJSON, false, metadata); err != nil {
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

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return routing.ProviderRoutingConfig{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

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
			_ = json.Unmarshal([]byte(c.advancedStr), &advanced)
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
			_ = json.Unmarshal([]byte(whenStr), &when)
		}
		if advancedStr != "" && advancedStr != "{}" {
			_ = json.Unmarshal([]byte(advancedStr), &adv)
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
