//go:build desktop

package app

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"arkloop/services/shared/desktop"
	"arkloop/services/shared/eventbus"
	sharedexec "arkloop/services/shared/executionconfig"
	"arkloop/services/shared/objectstore"
	"arkloop/services/shared/telegrambot"
	sharedtoolruntime "arkloop/services/shared/toolruntime"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/environmentbindings"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/memory"
	localmemory "arkloop/services/worker/internal/memory/local"
	"arkloop/services/worker/internal/memory/openviking"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/queue"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/subagentctl"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin"
	conversationtool "arkloop/services/worker/internal/tools/conversation"
	"arkloop/services/worker/internal/tools/localshell"
	memorytool "arkloop/services/worker/internal/tools/memory"
	"arkloop/services/worker/internal/tools/sandboxshell"

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
	stubRouter             *routing.ProviderRouter
	stubGateway            llm.Gateway
	emitDebugEvents        bool
	toolRegistry           *tools.Registry
	toolExecutors          map[string]tools.Executor
	allLlmSpecs            []llm.ToolSpec
	baseAllowlist          map[string]struct{}
	executorRegistry       pipeline.AgentExecutorBuilder
	personaRegistry        func() *personas.Registry
	memProvider            memory.MemoryProvider
	useOV                  bool
	useVM                  bool
	skillLayout            pipeline.SkillLayoutResolver
	runtimeSnapshot        *sharedtoolruntime.RuntimeSnapshot
	jobQueue               queue.JobQueue
	routingLoader          *routing.ConfigLoader
	messageAttachmentStore objectstore.Store
}

// ComposeDesktopEngine assembles a DesktopEngine from environment configuration.
// execRegistry is the agent executor builder (e.g., executor.DefaultExecutorRegistry()).
func ComposeDesktopEngine(ctx context.Context, db data.DesktopDB, bus eventbus.EventBus, execRegistry pipeline.AgentExecutorBuilder, jobQueue queue.JobQueue) (*DesktopEngine, error) {
	// Router is loaded dynamically per-run in desktopRouting middleware
	// so that credentials configured after startup are picked up immediately.
	stubRouter := routing.NewProviderRouter(routing.DefaultRoutingConfig())

	stubCfg, err := llm.StubGatewayConfigFromEnv()
	if err != nil {
		return nil, fmt.Errorf("stub gateway config: %w", err)
	}
	stubGateway := llm.NewStubGateway(stubCfg)

	toolRegistry := tools.NewRegistry()
	for _, spec := range builtin.AgentSpecs() {
		if err := toolRegistry.Register(spec); err != nil {
			slog.WarnContext(ctx, "desktop: skip tool registration", "name", spec.Name, "err", err)
		}
	}
	isolationMode := strings.TrimSpace(os.Getenv("ARKLOOP_DESKTOP_ISOLATION"))
	useVM := isolationMode == "vm" && desktop.GetSandboxAddr() != ""
	skillLayout := desktopSkillLayoutResolver(useVM)

	if useVM {
		for _, spec := range sandboxshell.AgentSpecs() {
			if err := toolRegistry.Register(spec); err != nil {
				slog.WarnContext(ctx, "desktop: skip tool registration", "name", spec.Name, "err", err)
			}
		}
	} else {
		for _, spec := range localshell.AgentSpecs() {
			if err := toolRegistry.Register(spec); err != nil {
				slog.WarnContext(ctx, "desktop: skip tool registration", "name", spec.Name, "err", err)
			}
		}
	}
	for _, spec := range conversationtool.AgentSpecs() {
		if err := toolRegistry.Register(spec); err != nil {
			slog.WarnContext(ctx, "desktop: skip tool registration", "name", spec.Name, "err", err)
		}
	}

	executors := builtin.Executors(nil, nil, nil)
	var runtimeSnapshot *sharedtoolruntime.RuntimeSnapshot

	if useVM {
		sandboxAddr := desktop.GetSandboxAddr()
		authToken := strings.TrimSpace(os.Getenv("ARKLOOP_DESKTOP_TOKEN"))
		vmExec := sandboxshell.NewExecutor("http://"+sandboxAddr, authToken)
		executors[sandboxshell.ExecCommandAgentSpec.Name] = vmExec
		executors[sandboxshell.WriteStdinAgentSpec.Name] = vmExec
		runtimeSnapshot = &sharedtoolruntime.RuntimeSnapshot{
			SandboxBaseURL:   "http://" + sandboxAddr,
			SandboxAuthToken: authToken,
			ACPHostKind:      "sandbox",
		}
		slog.Info("desktop: using VM isolation for shell execution", "sandbox_addr", sandboxAddr)
	} else {
		shellExec := localshell.NewExecutor()
		executors[localshell.ExecCommandAgentSpec.Name] = shellExec
		executors[localshell.WriteStdinAgentSpec.Name] = shellExec
		runtimeSnapshot = &sharedtoolruntime.RuntimeSnapshot{
			ACPHostKind: "local",
		}
		if isolationMode == "vm" {
			slog.Warn("desktop: VM isolation requested but sandbox not available, falling back to trusted local shell")
		}
	}

	convExec := conversationtool.NewToolExecutor(db, data.MessagesRepository{})
	for _, spec := range conversationtool.AgentSpecs() {
		executors[spec.Name] = convExec
	}

	memEnabled := strings.TrimSpace(os.Getenv("ARKLOOP_MEMORY_ENABLED")) != "false"
	ovURL := strings.TrimSpace(os.Getenv("ARKLOOP_OPENVIKING_BASE_URL"))
	ovKey := strings.TrimSpace(os.Getenv("ARKLOOP_OPENVIKING_ROOT_API_KEY"))

	var memProvider memory.MemoryProvider
	useOV := false
	if memEnabled && ovURL != "" && ovKey != "" {
		memProvider = openviking.NewProvider(openviking.Config{BaseURL: ovURL, RootAPIKey: ovKey})
		useOV = true
		slog.Info("desktop: using OpenViking memory provider", "url", ovURL)
	} else if memEnabled {
		memProvider = localmemory.NewProvider(db)
		slog.Info("desktop: using local SQLite memory provider")
	} else {
		slog.Info("desktop: memory disabled")
	}

	if memProvider != nil {
		memExec := memorytool.NewToolExecutor(memProvider)
		for _, spec := range memorytool.AgentSpecs() {
			executors[spec.Name] = memExec
		}
		for _, spec := range memorytool.AgentSpecs() {
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

	var shellLlmSpecs []llm.ToolSpec
	if useVM {
		shellLlmSpecs = sandboxshell.LlmSpecs()
	} else {
		shellLlmSpecs = localshell.LlmSpecs()
	}
	allLlmSpecs := append(builtin.LlmSpecs(), shellLlmSpecs...)
	allLlmSpecs = append(allLlmSpecs, conversationtool.LlmSpecs()...)
	if memProvider != nil {
		allLlmSpecs = append(allLlmSpecs, memorytool.LlmSpecs()...)
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
	if memProvider != nil {
		mergedRT = mergedRT.WithMergedBuiltinToolNames(
			"memory_search", "memory_read", "memory_write", "memory_forget",
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

	return &DesktopEngine{
		db:                     db,
		bus:                    bus,
		stubRouter:             stubRouter,
		stubGateway:            stubGateway,
		emitDebugEvents:        stubCfg.EmitDebugEvents,
		toolRegistry:           toolRegistry,
		toolExecutors:          executors,
		allLlmSpecs:            allLlmSpecs,
		baseAllowlist:          filtered,
		executorRegistry:       execRegistry,
		personaRegistry:        personaGetter,
		memProvider:            memProvider,
		useOV:                  useOV,
		useVM:                  useVM,
		skillLayout:            skillLayout,
		runtimeSnapshot:        runtimeSnapshot,
		jobQueue:               jobQueue,
		routingLoader:          routingLoader,
		messageAttachmentStore: messageAttachmentStore,
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
		Run:      run,
		Pool:     nil,
		EventBus: e.bus,
		TraceID:  traceID,
		Emitter:  emitter,
		Router:   e.stubRouter,
		Runtime:  e.runtimeSnapshot,

		ExecutorBuilder:     e.executorRegistry,
		ToolBudget:          map[string]any{},
		PerToolSoftLimits:   tools.DefaultPerToolSoftLimits(),
		PendingMemoryWrites: memory.NewPendingWriteBuffer(),

		LlmRetryMaxAttempts: 3,
		LlmRetryBaseDelayMs: 1000,

		ThreadMessageHistoryLimit:     200,
		AgentReasoningIterationsLimit: 0,
		ToolContinuationBudgetLimit:   32,
		MaxParallelTasks:              4,
		CreditPerUSD:                  1000,
		LlmMaxResponseBytes:           16384,

		UserID:       run.CreatedByUserID,
		ProfileRef:   derefStr(run.ProfileRef),
		WorkspaceRef: derefStr(run.WorkspaceRef),
		JobPayload:   cloneDesktopMap(jobPayload),
	}
	if !e.useVM {
		defer func() {
			if err := cleanupDesktopSkillRuntime(run.ID); err != nil {
				slog.WarnContext(ctx, "desktop: cleanup skill runtime failed", "run_id", run.ID.String(), "err", err.Error())
			}
		}()
	}

	if e.jobQueue != nil && subAgentsEnabled {
		rc.SubAgentControl = subagentctl.NewService(e.db, nil, e.jobQueue, run, traceID, subagentctl.SubAgentLimits{}, subagentctl.BackpressureConfig{})
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

	var memMiddleware pipeline.RunMiddleware
	if e.useOV {
		// OpenViking: full semantic memory middleware (nil pool = no snapshot cache, nil configResolver = no billing)
		memMiddleware = pipeline.NewMemoryMiddleware(e.memProvider, nil, nil)
	} else {
		// Local SQLite: lightweight snapshot injection
		memMiddleware = desktopMemoryInjection(e.db)
	}

	middlewares := []pipeline.RunMiddleware{
		desktopCancelGuard(),
		desktopInputLoader(e.db, eventsRepo, e.messageAttachmentStore),
		desktopToolInit(e.toolExecutors, e.allLlmSpecs, e.baseAllowlist, e.toolRegistry),
		pipeline.NewSpawnAgentMiddleware(),
		desktopPersonaResolution(e.db, e.personaRegistry, runsRepo, eventsRepo),
		desktopChannelContext(e.db),
		pipeline.NewChannelGroupContextTrimMiddleware(),
		pipeline.NewChannelTelegramToolsMiddleware(&desktopTelegramTokenLoader{db: e.db}, nil),
		desktopSubAgentContext(e.db, subagentctl.NewSnapshotStorage()),
		pipeline.NewSkillContextMiddleware(pipeline.SkillContextConfig{
			Resolve:        desktopSkillResolver(e.db),
			Prepare:        desktopSkillPreparer(e.useVM),
			LayoutResolver: e.skillLayout,
			ExternalDirs:   desktopExternalSkillDirs(e.db),
		}),
		memMiddleware,
		desktopRouting(e.stubRouter, e.stubGateway, e.emitDebugEvents, e.db, runsRepo, eventsRepo),
		pipeline.NewTitleSummarizerMiddleware(e.db, nil, e.stubGateway, e.emitDebugEvents, e.routingLoader),
		pipeline.NewContextCompactMiddleware(e.db, data.MessagesRepository{}, data.DesktopRunEventsRepository{}, e.stubGateway, e.emitDebugEvents, e.routingLoader),
		pipeline.NewToolBuildMiddleware(),
		desktopChannelDelivery(e.db),
	}
	terminal := desktopAgentLoop(e.db, e.bus, e.jobQueue, runsRepo, eventsRepo)
	handler := pipeline.Build(middlewares, terminal)

	return handler(ctx, rc)
}

func resolveDesktopRunBindings(ctx context.Context, db data.DesktopDB, run data.Run) (data.Run, error) {
	if db == nil {
		return run, fmt.Errorf("desktop db must not be nil")
	}
	return environmentbindings.ResolveAndPersistRun(ctx, db, run)
}

// --------------- desktop middleware ---------------

// desktopMemoryInjection reads the saved memory_block from user_memory_snapshots
// and appends it to the run's system prompt. This is the desktop equivalent of
// NewMemoryMiddleware — lightweight and synchronous, no vector search required.
// All desktop memories are stored under agent_id="default" (user-level, persona-agnostic).
func desktopMemoryInjection(db data.DesktopDB) pipeline.RunMiddleware {
	return func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		if rc.UserID == nil {
			return next(ctx, rc)
		}
		var block string
		err := db.QueryRow(ctx,
			`SELECT memory_block FROM user_memory_snapshots
			 WHERE account_id = $1 AND user_id = $2 AND agent_id = 'default'`,
			rc.Run.AccountID.String(), rc.UserID.String(),
		).Scan(&block)
		if err == nil && strings.TrimSpace(block) != "" {
			if strings.TrimSpace(rc.SystemPrompt) != "" {
				rc.SystemPrompt = rc.SystemPrompt + "\n\n" + strings.TrimSpace(block)
			} else {
				rc.SystemPrompt = strings.TrimSpace(block)
			}
		}
		// Ignore ErrNoRows / any DB errors — no memory is a valid state.
		return next(ctx, rc)
	}
}

// desktopCancelGuard provides a cancellable context without LISTEN/NOTIFY.
func desktopCancelGuard() pipeline.RunMiddleware {
	return func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		execCtx, cancel := context.WithCancel(ctx)
		rc.CancelFunc = cancel
		done := make(chan struct{})
		close(done)
		rc.ListenDone = done
		defer cancel()
		return next(execCtx, rc)
	}
}

// desktopInputLoader loads run input and thread messages from SQLite.
func desktopInputLoader(
	db data.DesktopDB,
	eventsRepo data.DesktopRunEventsRepository,
	attachmentStore pipeline.MessageAttachmentStore,
) pipeline.RunMiddleware {
	return func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		messageLimit := rc.ThreadMessageHistoryLimit
		if messageLimit <= 0 {
			messageLimit = 200
		}

		tx, err := db.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return err
		}

		_, dataJSON, err := eventsRepo.FirstEventData(ctx, tx, rc.Run.ID)
		if err != nil {
			tx.Rollback(ctx)
			return err
		}

		inputJSON := map[string]any{
			"account_id": rc.Run.AccountID.String(),
			"thread_id":  rc.Run.ThreadID.String(),
		}
		if dataJSON != nil {
			for _, key := range []string{"route_id", "persona_id", "role", "output_route_id", "model", "work_dir"} {
				if v, ok := dataJSON[key].(string); ok && strings.TrimSpace(v) != "" {
					inputJSON[key] = strings.TrimSpace(v)
				}
			}
		}

		messagesRepo := data.MessagesRepository{}
		messages, err := messagesRepo.ListByThread(ctx, tx, rc.Run.AccountID, rc.Run.ThreadID, messageLimit)
		if err != nil {
			tx.Rollback(ctx)
			return err
		}

		// Release the read-only tx before calling next. With MaxOpenConns(1)
		// the single SQLite connection must be free for downstream middleware.
		tx.Rollback(ctx)

		rc.InputJSON = inputJSON
		if wd, ok := inputJSON["work_dir"].(string); ok && strings.TrimSpace(wd) != "" {
			rc.WorkDir = strings.TrimSpace(wd)
		}
		llmMessages := make([]llm.Message, 0, len(messages))
		ids := make([]uuid.UUID, 0, len(messages))
		for _, msg := range messages {
			if strings.TrimSpace(msg.Role) == "" {
				continue
			}
			parts, err := pipeline.BuildMessageParts(ctx, attachmentStore, msg)
			if err != nil {
				return err
			}
			llmMessages = append(llmMessages, llm.Message{
				Role:    msg.Role,
				Content: parts,
			})
			ids = append(ids, msg.ID)
		}
		rc.Messages = llmMessages
		rc.ThreadMessageIDs = ids

		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "user" && strings.TrimSpace(messages[i].Content) != "" {
				inputJSON["last_user_message"] = strings.TrimSpace(messages[i].Content)
				break
			}
		}

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
	Token      string
	ConfigJSON []byte
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

	return func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		var preloaded *desktopDeliveryChannelRecord
		var ux pipeline.TelegramChannelUX
		if db != nil && rc != nil && rc.ChannelContext != nil && rc.ChannelContext.ChannelType == "telegram" {
			ch, prefetchErr := loadDesktopDeliveryChannel(ctx, db, rc.ChannelContext.ChannelID)
			if prefetchErr != nil {
				slog.WarnContext(ctx, "desktop channel delivery prefetch failed", "run_id", rc.Run.ID, "err", prefetchErr.Error())
			} else if ch != nil {
				preloaded = ch
				ux = pipeline.ParseTelegramChannelUX(ch.ConfigJSON)
			}
		}

		streamMidCount := 0
		var streamFlush func(context.Context, string) error
		if preloaded != nil && db != nil && rc != nil && rc.ChannelContext != nil && rc.ChannelContext.ChannelType == "telegram" &&
			strings.TrimSpace(preloaded.Token) != "" {
			uxReg := pipeline.ParseTelegramChannelUX(preloaded.ConfigJSON)
			replyRef := pipeline.ResolveTelegramOutboundReplyTo(rc, uxReg)
			sender := pipeline.NewTelegramChannelSenderWithClient(client, preloaded.Token, 50*time.Millisecond)
			streamFlush = func(ctx2 context.Context, text string) error {
				ids, sendErr := sender.SendText(ctx2, pipeline.ChannelDeliveryTarget{
					ChannelType:  rc.ChannelContext.ChannelType,
					Conversation: rc.ChannelContext.Conversation,
					ReplyTo:      replyRef,
				}, text)
				if sendErr != nil {
					return sendErr
				}
				if err := recordDesktopChannelDelivery(ctx2, db, rc.Run.ID, rc.Run.ThreadID, rc.ChannelContext.ChannelID, rc.ChannelContext.Conversation.Target, replyRef, rc.ChannelContext.Conversation.ThreadID, ids); err != nil {
					return err
				}
				streamMidCount++
				return nil
			}
			rc.TelegramToolBoundaryFlush = streamFlush
		}

		var stopTyping context.CancelFunc
		if preloaded != nil && ux.TypingIndicator && strings.TrimSpace(preloaded.Token) != "" {
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
		if db == nil || rc.ChannelContext.ChannelType != "telegram" {
			return err
		}

		fullOut := strings.TrimSpace(rc.FinalAssistantOutput)
		remainder := strings.TrimSpace(rc.TelegramStreamDeliveryRemainder)
		if fullOut == "" && remainder == "" && streamMidCount == 0 {
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

		uxSend := pipeline.ParseTelegramChannelUX(channel.ConfigJSON)

		var finalRecordErr error
		if output != "" {
			replyTo := pipeline.ResolveTelegramOutboundReplyTo(rc, uxSend)
			sender := pipeline.NewTelegramChannelSenderWithClient(client, channel.Token, 50*time.Millisecond)
			messageIDs, sendErr := sender.SendText(ctx, pipeline.ChannelDeliveryTarget{
				ChannelType:  rc.ChannelContext.ChannelType,
				Conversation: rc.ChannelContext.Conversation,
				ReplyTo:      replyTo,
			}, output)
			if sendErr != nil {
				recordDesktopChannelDeliveryFailure(db, rc.Run.ID, sendErr)
				slog.WarnContext(ctx, "desktop telegram channel delivery failed", "run_id", rc.Run.ID, "err", sendErr.Error())
				return err
			}
			finalRecordErr = recordDesktopChannelDelivery(
				ctx,
				db,
				rc.Run.ID,
				rc.Run.ThreadID,
				rc.ChannelContext.ChannelID,
				rc.ChannelContext.Conversation.Target,
				replyTo,
				rc.ChannelContext.Conversation.ThreadID,
				messageIDs,
			)
			if finalRecordErr != nil {
				recordDesktopChannelDeliveryFailure(db, rc.Run.ID, finalRecordErr)
				slog.WarnContext(ctx, "desktop telegram channel delivery record failed", "run_id", rc.Run.ID, "err", finalRecordErr.Error())
			}
		}

		if finalRecordErr == nil && strings.TrimSpace(uxSend.ReactionEmoji) != "" {
			pipeline.MaybeTelegramInboundReaction(ctx, client, channel.Token, rc, uxSend.ReactionEmoji)
		}
		return err
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

func loadDesktopDeliveryChannel(ctx context.Context, db data.DesktopDB, channelID uuid.UUID) (*desktopDeliveryChannelRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("db must not be nil")
	}
	var (
		encryptedValue *string
		keyVersion     *int
		configRaw      []byte
	)
	err := db.QueryRow(
		ctx,
		`SELECT s.encrypted_value, s.key_version, COALESCE(c.config_json, '{}')
		   FROM channels c
		   LEFT JOIN secrets s ON s.id = c.credentials_id
		  WHERE c.id = $1
		    AND c.is_active = 1`,
		channelID,
	).Scan(&encryptedValue, &keyVersion, &configRaw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("desktop channel lookup: %w", err)
	}
	if encryptedValue == nil || strings.TrimSpace(*encryptedValue) == "" || keyVersion == nil {
		return nil, fmt.Errorf("desktop channel lookup: missing telegram token")
	}
	keyRing, err := loadDesktopKeyRing()
	if err != nil {
		return nil, fmt.Errorf("desktop channel lookup: load encryption key: %w", err)
	}
	token, err := decryptAESGCM(keyRing, *encryptedValue, *keyVersion)
	if err != nil {
		return nil, fmt.Errorf("desktop channel lookup: decrypt token: %w", err)
	}
	return &desktopDeliveryChannelRecord{Token: token, ConfigJSON: configRaw}, nil
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
			ChannelType:             "telegram",
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
			rc.TitleSummarizer = def.TitleSummarizer
			if rc.TitleSummarizer == nil {
				rc.TitleSummarizer = personas.DesktopFallbackTitleSummarizer()
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
	stubGateway llm.Gateway,
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

		gateway, err := desktopGatewayFromRoute(*decision.Selected, stubGateway, emitDebugEvents, rc.LlmMaxResponseBytes)
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
			gw, gwErr := desktopGatewayFromRoute(*d.Selected, stubGateway, emitDebugEvents, rc.LlmMaxResponseBytes)
			if gwErr != nil {
				return nil, nil, gwErr
			}
			return gw, d.Selected, nil
		}

		rc.Gateway = gateway
		rc.SelectedRoute = decision.Selected
		rc.ResolveGatewayForRouteID = resolveGateway
		rc.ResolveGatewayForAgentName = func(ctx context.Context, name string) (llm.Gateway, *routing.SelectedProviderRoute, error) {
			return resolveGateway(ctx, "")
		}

		rc.RoutingByokEnabled = false

		return next(ctx, rc)
	}
}

func desktopGatewayFromRoute(selected routing.SelectedProviderRoute, stub llm.Gateway, debug bool, maxBytes int) (llm.Gateway, error) {
	cred := selected.Credential
	advanced := mergeJSON(cred.AdvancedJSON, selected.Route.AdvancedJSON)
	switch cred.ProviderKind {
	case routing.ProviderKindStub:
		return stub, nil
	case routing.ProviderKindOpenAI:
		key, err := resolveDesktopAPIKey(cred)
		if err != nil {
			return nil, err
		}
		baseURL := ""
		if cred.BaseURL != nil {
			baseURL = *cred.BaseURL
		}
		apiMode := "auto"
		if cred.OpenAIMode != nil {
			apiMode = *cred.OpenAIMode
		}
		return llm.NewOpenAIGateway(llm.OpenAIGatewayConfig{
			APIKey: key, BaseURL: baseURL, APIMode: apiMode,
			AdvancedJSON: advanced, EmitDebugEvents: debug,
		}), nil
	case routing.ProviderKindAnthropic:
		key, err := resolveDesktopAPIKey(cred)
		if err != nil {
			return nil, err
		}
		baseURL := ""
		if cred.BaseURL != nil {
			baseURL = *cred.BaseURL
		}
		return llm.NewAnthropicGateway(llm.AnthropicGatewayConfig{
			APIKey: key, BaseURL: baseURL,
			AdvancedJSON: advanced, EmitDebugEvents: debug,
			MaxResponseBytes: maxBytes,
		}), nil
	default:
		return nil, fmt.Errorf("unknown provider_kind: %s", cred.ProviderKind)
	}
}

func resolveDesktopAPIKey(cred routing.ProviderCredential) (string, error) {
	if cred.APIKeyValue != nil && strings.TrimSpace(*cred.APIKeyValue) != "" {
		return *cred.APIKeyValue, nil
	}
	if cred.APIKeyEnv == nil || strings.TrimSpace(*cred.APIKeyEnv) == "" {
		return "", fmt.Errorf("missing api_key_env")
	}
	name := strings.TrimSpace(*cred.APIKeyEnv)
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("missing environment variable %s", name)
	}
	return value, nil
}

// --------------- desktop agent loop ---------------

var desktopTerminalStatuses = map[string]string{
	"run.completed": "completed",
	"run.failed":    "failed",
	"run.cancelled": "cancelled",
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
			telegramBoundaryFlush: rc.TelegramToolBoundaryFlush,
		}
		defer w.close(ctx)

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
			return w.flush(ctx)
		}

		execErr := exec.Execute(ctx, rc, rc.Emitter, func(ev events.RunEvent) error {
			return w.append(ctx, rc.Run.ID, ev, "")
		})
		if execErr != nil && !errors.Is(execErr, errDesktopStopProcessing) {
			return execErr
		}

		if w.completed {
			messagesRepo := data.MessagesRepository{}
			if err := w.ensureTx(ctx); err != nil {
				return err
			}
			content := strings.Join(w.assistantDeltas, "")
			_, err := messagesRepo.InsertAssistantMessage(ctx, w.tx, rc.Run.AccountID, rc.Run.ThreadID, rc.Run.ID, content)
			if err != nil {
				slog.WarnContext(ctx, "desktop: insert assistant message failed", "err", err)
			}
			rc.FinalAssistantOutput = content
			rc.TelegramStreamDeliveryRemainder = w.telegramStreamRemainder()
		}
		rc.RunToolCallCount = w.toolCallCount
		rc.RunIterationCount = w.iterationCount
		return w.flush(ctx)
	}
}

var errDesktopStopProcessing = errors.New("desktop_stop_processing")

var desktopStreamingEventTypes = map[string]struct{}{
	"message.delta":      {},
	"llm.response.chunk": {},
	"run.segment.start":  {},
	"run.segment.end":    {},
	"tool.call.delta":    {},
}

// desktopEventWriter batches event writes into transactions using DesktopDB.
type desktopEventWriter struct {
	db         data.DesktopDB
	bus        eventbus.EventBus
	run        data.Run
	traceID    string
	model      string
	runsRepo   data.DesktopRunsRepository
	eventsRepo data.DesktopRunEventsRepository
	projector  *subagentctl.SubAgentStateProjector

	tx                       pgx.Tx
	pendingEventsSinceCommit int
	lastCommitAt             time.Time
	pendingEnqueueRunIDs     []uuid.UUID
	assistantDeltas          []string
	toolCallCount            int
	iterationCount           int
	completed                bool
	hasTerminal              bool
	terminalRunStatus        string
	totalInputTokens         int64
	totalOutputTokens        int64
	totalCostUSD             float64
	telegramBoundaryFlush    func(context.Context, string) error
	telegramFlushSentDeltas  int
}

func (w *desktopEventWriter) telegramStreamRemainder() string {
	if w.telegramBoundaryFlush == nil {
		return ""
	}
	if w.telegramFlushSentDeltas >= len(w.assistantDeltas) {
		return ""
	}
	return strings.TrimSpace(strings.Join(w.assistantDeltas[w.telegramFlushSentDeltas:], ""))
}

func (w *desktopEventWriter) ensureTx(ctx context.Context) error {
	if w.tx != nil {
		return nil
	}
	tx, err := w.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	w.tx = tx
	w.lastCommitAt = time.Now()
	return nil
}

func (w *desktopEventWriter) append(ctx context.Context, runID uuid.UUID, ev events.RunEvent, personaID string) error {
	if err := w.ensureTx(ctx); err != nil {
		return err
	}

	if err := w.runsRepo.LockRunRow(ctx, w.tx, runID); err != nil {
		return err
	}

	if ev.Type == "run.route.selected" && personaID != "" {
		_ = w.runsRepo.UpdateRunMetadata(ctx, w.tx, runID, w.model, personaID)
	}

	// 检测取消
	cancelTypes := []string{"run.cancel_requested", "run.cancelled"}
	cancelType, err := w.eventsRepo.GetLatestEventType(ctx, w.tx, runID, cancelTypes)
	if err != nil {
		return err
	}
	if cancelType == "run.cancelled" {
		_ = w.commit(ctx)
		return errDesktopStopProcessing
	}
	if cancelType == "run.cancel_requested" {
		emitter := events.NewEmitter(w.traceID)
		cancelled := emitter.Emit("run.cancelled", map[string]any{}, nil, nil)
		_, _ = w.eventsRepo.AppendRunEvent(ctx, w.tx, runID, cancelled)
		if w.projector != nil {
			nextRunID, err := w.projector.ProjectRunTerminal(ctx, w.tx, w.run, data.SubAgentStatusCancelled, map[string]any{"run_id": runID.String()}, nil)
			if err != nil {
				return err
			}
			if nextRunID != nil {
				w.pendingEnqueueRunIDs = append(w.pendingEnqueueRunIDs, *nextRunID)
			}
		}
		_ = w.runsRepo.UpdateRunTerminalStatus(ctx, w.tx, runID, data.TerminalStatusUpdate{
			Status: "cancelled", TotalInputTokens: w.totalInputTokens, TotalOutputTokens: w.totalOutputTokens, TotalCostUSD: w.totalCostUSD,
		})
		w.hasTerminal = true
		w.terminalRunStatus = "cancelled"
		_ = w.commit(ctx)
		return errDesktopStopProcessing
	}

	if _, err := w.eventsRepo.AppendRunEvent(ctx, w.tx, runID, ev); err != nil {
		return err
	}
	w.pendingEventsSinceCommit++

	w.accumUsage(ev.DataJSON)

	if ev.Type == "tool.call" {
		if w.telegramBoundaryFlush != nil && len(w.assistantDeltas) > w.telegramFlushSentDeltas {
			chunk := strings.Join(w.assistantDeltas[w.telegramFlushSentDeltas:], "")
			if err := w.commit(ctx); err != nil {
				return err
			}
			w.tx = nil
			if trimmed := strings.TrimSpace(chunk); trimmed != "" {
				if err := w.telegramBoundaryFlush(ctx, trimmed); err != nil {
					return err
				}
			}
			w.telegramFlushSentDeltas = len(w.assistantDeltas)
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
			}
		}
	}

	if status, ok := desktopTerminalStatuses[ev.Type]; ok {
		if status == "completed" {
			w.completed = true
		}
		_ = w.runsRepo.UpdateRunTerminalStatus(ctx, w.tx, runID, data.TerminalStatusUpdate{
			Status: status, TotalInputTokens: w.totalInputTokens, TotalOutputTokens: w.totalOutputTokens, TotalCostUSD: w.totalCostUSD,
		})
		if w.projector != nil {
			nextRunID, err := w.projector.ProjectRunTerminal(ctx, w.tx, w.run, status, ev.DataJSON, ev.ErrorClass)
			if err != nil {
				return err
			}
			if nextRunID != nil {
				w.pendingEnqueueRunIDs = append(w.pendingEnqueueRunIDs, *nextRunID)
			}
		}
		w.hasTerminal = true
		w.terminalRunStatus = status
		return nil
	}

	if _, ok := desktopStreamingEventTypes[ev.Type]; !ok {
		return w.commit(ctx)
	}

	const batchSize = 20
	const maxInterval = 50 * time.Millisecond
	if w.pendingEventsSinceCommit >= batchSize || time.Since(w.lastCommitAt) >= maxInterval {
		return w.commit(ctx)
	}
	return nil
}

func (w *desktopEventWriter) commit(ctx context.Context) error {
	if w.tx == nil {
		return nil
	}
	if w.pendingEventsSinceCommit > 0 && !w.hasTerminal {
		if err := w.runsRepo.TouchRunActivity(ctx, w.tx, w.run.ID); err != nil {
			return err
		}
	}
	if err := w.tx.Commit(ctx); err != nil {
		return err
	}
	w.tx = nil
	w.pendingEventsSinceCommit = 0
	w.lastCommitAt = time.Now()
	if w.bus != nil {
		channel := fmt.Sprintf("run_events:%s", w.run.ID.String())
		_ = w.bus.Publish(ctx, channel, "")
	}
	if w.hasTerminal {
		for _, nextRunID := range w.pendingEnqueueRunIDs {
			if w.projector == nil {
				continue
			}
			if err := w.projector.EnqueueRun(ctx, w.run.AccountID, nextRunID, w.traceID, nil); err != nil {
				_ = w.projector.MarkRunFailed(context.Background(), nextRunID, "failed to enqueue child run job")
			}
		}
		w.pendingEnqueueRunIDs = nil
		w.hasTerminal = false
		w.terminalRunStatus = ""
	}
	return nil
}

func (w *desktopEventWriter) flush(ctx context.Context) error {
	return w.commit(ctx)
}

func (w *desktopEventWriter) close(ctx context.Context) {
	if w.tx != nil {
		_ = w.tx.Rollback(ctx)
		w.tx = nil
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
	if v, ok := toDesktopFloat64(usage["cost_usd"]); ok {
		w.totalCostUSD += v
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
	payload := map[string]any{
		"error_class": errorClass,
		"message":     message,
	}
	if len(details) > 0 {
		payload["details"] = details
	}
	failed := emitter.Emit("run.failed", payload, nil, pipeline.StringPtr(errorClass))

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("desktop write failure: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := eventsRepo.AppendRunEvent(ctx, tx, run.ID, failed); err != nil {
		return err
	}
	if err := runsRepo.UpdateRunTerminalStatus(ctx, tx, run.ID, data.TerminalStatusUpdate{Status: "failed"}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func desktopExtractDelta(dataJSON map[string]any) string {
	role, ok := dataJSON["role"]
	if ok && role != nil && role != "assistant" {
		return ""
	}
	delta, _ := dataJSON["content_delta"].(string)
	return delta
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

func mergeJSON(a, b map[string]any) map[string]any {
	out := make(map[string]any, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
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

// loadDesktopRoutingConfig builds a ProviderRoutingConfig from the SQLite
// llm_credentials, llm_routes, and secrets tables.
// All queries run inside a single read-only transaction to avoid deadlocking
// the single SQLite connection (MaxOpenConns=1).
func loadDesktopRoutingConfig(ctx context.Context, db data.DesktopDB) (routing.ProviderRoutingConfig, error) {
	keyRing, err := loadDesktopKeyRing()
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
	credMap := map[string]struct{}{}
	for _, c := range rawCreds {
		var apiKey *string
		if c.secretID != nil && *c.secretID != "" {
			var encVal string
			var keyVer int
			if err := tx.QueryRow(ctx, `SELECT encrypted_value, key_version FROM secrets WHERE id = $1`, *c.secretID).Scan(&encVal, &keyVer); err != nil {
				slog.WarnContext(ctx, "desktop: skip credential, secret not found", "cred_id", c.id, "err", err)
				continue
			}
			plain, err := decryptAESGCM(keyRing, encVal, keyVer)
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
		creds = append(creds, routing.ProviderCredential{
			ID: c.id, Name: c.name, OwnerKind: scope,
			ProviderKind: routing.ProviderKind(c.provider),
			APIKeyValue:  apiKey, BaseURL: c.baseURL, OpenAIMode: c.openAIMode, AdvancedJSON: advanced,
		})
		credMap[c.id] = struct{}{}
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
		if _, ok := credMap[credentialID]; !ok {
			continue
		}
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

func decryptAESGCM(key [32]byte, encoded string, keyVersion int) (string, error) {
	if keyVersion != 1 {
		return "", fmt.Errorf("unsupported key version %d", keyVersion)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	if len(raw) < 12 {
		return "", fmt.Errorf("ciphertext too short")
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, raw[:12], raw[12:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func loadDesktopKeyRing() ([32]byte, error) {
	dataDir, err := desktop.ResolveDataDir("")
	if err != nil {
		return [32]byte{}, err
	}
	raw, err := os.ReadFile(filepath.Join(dataDir, "encryption.key"))
	if err != nil {
		return [32]byte{}, fmt.Errorf("read encryption.key: %w", err)
	}

	decoded, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(decoded) != 32 {
		return [32]byte{}, fmt.Errorf("invalid encryption.key (expected 64 hex chars)")
	}

	var key [32]byte
	copy(key[:], decoded)
	return key, nil
}
