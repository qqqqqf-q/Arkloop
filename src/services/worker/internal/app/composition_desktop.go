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

	sharedexec "arkloop/services/shared/executionconfig"
	"arkloop/services/shared/desktop"
	"arkloop/services/shared/eventbus"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/memory"
	localmemory "arkloop/services/worker/internal/memory/local"
	"arkloop/services/worker/internal/memory/openviking"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin"
	"arkloop/services/worker/internal/tools/localfs"
	"arkloop/services/worker/internal/tools/localshell"
	"arkloop/services/worker/internal/tools/sandboxshell"
	memorytool "arkloop/services/worker/internal/tools/memory"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DesktopEngine executes LLM agent runs backed by SQLite.
type DesktopEngine struct {
	db               data.DesktopDB
	bus              eventbus.EventBus
	stubRouter       *routing.ProviderRouter
	stubGateway      llm.Gateway
	emitDebugEvents  bool
	toolRegistry     *tools.Registry
	toolExecutors    map[string]tools.Executor
	allLlmSpecs      []llm.ToolSpec
	baseAllowlist    map[string]struct{}
	executorRegistry pipeline.AgentExecutorBuilder
	personaRegistry  func() *personas.Registry
	memProvider      memory.MemoryProvider
	useOV            bool
}

// ComposeDesktopEngine assembles a DesktopEngine from environment configuration.
// execRegistry is the agent executor builder (e.g., executor.DefaultExecutorRegistry()).
func ComposeDesktopEngine(ctx context.Context, db data.DesktopDB, bus eventbus.EventBus, execRegistry pipeline.AgentExecutorBuilder) (*DesktopEngine, error) {
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
	for _, spec := range localfs.AgentSpecs() {
		if err := toolRegistry.Register(spec); err != nil {
			slog.WarnContext(ctx, "desktop: skip tool registration", "name", spec.Name, "err", err)
		}
	}

	executors := builtin.Executors(nil, nil, nil)

	if useVM {
		sandboxAddr := desktop.GetSandboxAddr()
		authToken := strings.TrimSpace(os.Getenv("ARKLOOP_DESKTOP_TOKEN"))
		vmExec := sandboxshell.NewExecutor("http://"+sandboxAddr, authToken)
		executors[sandboxshell.ExecCommandAgentSpec.Name] = vmExec
		executors[sandboxshell.WriteStdinAgentSpec.Name] = vmExec
		slog.Info("desktop: using VM isolation for shell execution", "sandbox_addr", sandboxAddr)
	} else {
		shellExec := localshell.NewExecutor()
		executors[localshell.ExecCommandAgentSpec.Name] = shellExec
		executors[localshell.WriteStdinAgentSpec.Name] = shellExec
		if isolationMode == "vm" {
			slog.Warn("desktop: VM isolation requested but sandbox not available, falling back to trusted local shell")
		}
	}

	fsExec := localfs.NewExecutor()
	executors[localfs.FileReadAgentSpec.Name] = fsExec
	executors[localfs.FileWriteAgentSpec.Name] = fsExec

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

	var shellLlmSpecs []llm.ToolSpec
	if useVM {
		shellLlmSpecs = sandboxshell.LlmSpecs()
	} else {
		shellLlmSpecs = localshell.LlmSpecs()
	}
	allLlmSpecs := append(builtin.LlmSpecs(), shellLlmSpecs...)
	allLlmSpecs = append(allLlmSpecs, localfs.LlmSpecs()...)
	if memProvider != nil {
		allLlmSpecs = append(allLlmSpecs, memorytool.LlmSpecs()...)
	}

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

	return &DesktopEngine{
		db:               db,
		bus:              bus,
		stubRouter:       stubRouter,
		stubGateway:      stubGateway,
		emitDebugEvents:  stubCfg.EmitDebugEvents,
		toolRegistry:     toolRegistry,
		toolExecutors:    executors,
		allLlmSpecs:      allLlmSpecs,
		baseAllowlist:    filtered,
		executorRegistry: execRegistry,
		personaRegistry:  personaGetter,
		memProvider:      memProvider,
		useOV:            useOV,
	}, nil
}

func loadPersonaRegistryFromFS() func() *personas.Registry {
	dirs := []string{"personas", "src/personas", "../personas"}
	for _, dir := range dirs {
		reg, err := personas.LoadRegistry(dir)
		if err == nil && len(reg.ListIDs()) > 0 {
			slog.Info("desktop: personas loaded from filesystem", "dir", dir, "count", len(reg.ListIDs()))
			return func() *personas.Registry { return reg }
		}
	}
	return nil
}

// Execute runs the agent pipeline for a single run.
func (e *DesktopEngine) Execute(ctx context.Context, run data.Run, traceID string) error {
	traceID = strings.TrimSpace(traceID)
	emitter := events.NewEmitter(traceID)

	runsRepo := data.DesktopRunsRepository{}
	eventsRepo := data.DesktopRunEventsRepository{}

	rc := &pipeline.RunContext{
		Run:      run,
		Pool:     nil,
		EventBus: e.bus,
		TraceID:  traceID,
		Emitter:  emitter,
		Router:   e.stubRouter,

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
		LlmMaxResponseBytes:          16384,

		UserID:       run.CreatedByUserID,
		ProfileRef:   derefStr(run.ProfileRef),
		WorkspaceRef: derefStr(run.WorkspaceRef),
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
		desktopInputLoader(e.db, eventsRepo),
		desktopToolInit(e.toolExecutors, e.allLlmSpecs, e.baseAllowlist, e.toolRegistry),
		desktopPersonaResolution(e.db, e.personaRegistry, runsRepo, eventsRepo),
		memMiddleware,
		desktopRouting(e.stubRouter, e.stubGateway, e.emitDebugEvents, e.db, runsRepo, eventsRepo),
		pipeline.NewToolBuildMiddleware(),
	}
	terminal := desktopAgentLoop(e.db, runsRepo, eventsRepo)
	handler := pipeline.Build(middlewares, terminal)

	return handler(ctx, rc)
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
			for _, key := range []string{"route_id", "persona_id", "role", "output_route_id"} {
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
		llmMessages := make([]llm.Message, 0, len(messages))
		for _, msg := range messages {
			if strings.TrimSpace(msg.Role) == "" {
				continue
			}
			llmMessages = append(llmMessages, llm.Message{
				Role:    msg.Role,
				Content: []llm.ContentPart{{Type: "text", Text: msg.Content}},
			})
		}
		rc.Messages = llmMessages

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

// desktopPersonaResolution resolves persona from desktop DB or filesystem.
func desktopPersonaResolution(
	db data.DesktopDB,
	getBaseRegistry func() *personas.Registry,
	runsRepo data.DesktopRunsRepository,
	eventsRepo data.DesktopRunEventsRepository,
) pipeline.RunMiddleware {
	return func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		registry := personas.NewRegistry()

		dbDefs, err := personas.LoadPersonasFromDesktopDB(ctx, db)
		if err != nil {
			slog.WarnContext(ctx, "desktop: persona db load failed, trying filesystem", "err", err)
		} else {
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
			selector := ""
			if rc.AgentConfig != nil && rc.AgentConfig.Model != nil {
				selector = strings.TrimSpace(*rc.AgentConfig.Model)
			}
			if selector != "" {
				route, cred, ok := cfg.GetHighestPriorityRouteByModel(selector, rc.InputJSON)
				if ok {
					decision = routing.ProviderRouteDecision{
						Selected: &routing.SelectedProviderRoute{Route: route, Credential: cred},
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
	runsRepo data.DesktopRunsRepository,
	eventsRepo data.DesktopRunEventsRepository,
) pipeline.RunHandler {
	return func(ctx context.Context, rc *pipeline.RunContext) error {
		selected := rc.SelectedRoute

		w := &desktopEventWriter{
			db:         db,
			run:        rc.Run,
			traceID:    rc.TraceID,
			model:      selected.Route.Model,
			runsRepo:   runsRepo,
			eventsRepo: eventsRepo,
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
		}
		rc.RunToolCallCount = w.toolCallCount
		rc.RunIterationCount = w.iterationCount
		return w.flush(ctx)
	}
}

var errDesktopStopProcessing = errors.New("desktop_stop_processing")

// desktopEventWriter batches event writes into transactions using DesktopDB.
type desktopEventWriter struct {
	db         data.DesktopDB
	run        data.Run
	traceID    string
	model      string
	runsRepo   data.DesktopRunsRepository
	eventsRepo data.DesktopRunEventsRepository

	tx                       pgx.Tx
	pendingEventsSinceCommit int
	lastCommitAt             time.Time
	assistantDeltas          []string
	toolCallCount            int
	iterationCount           int
	completed                bool
	hasTerminal              bool
	totalInputTokens         int64
	totalOutputTokens        int64
	totalCostUSD             float64
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
		_, _ = w.eventsRepo.AppendEvent(ctx, w.tx, runID, cancelled.Type, cancelled.DataJSON, cancelled.ToolName, cancelled.ErrorClass)
		_ = w.runsRepo.UpdateRunTerminalStatus(ctx, w.tx, runID, data.TerminalStatusUpdate{
			Status: "cancelled", TotalInputTokens: w.totalInputTokens, TotalOutputTokens: w.totalOutputTokens, TotalCostUSD: w.totalCostUSD,
		})
		w.hasTerminal = true
		_ = w.commit(ctx)
		return errDesktopStopProcessing
	}

	if _, err := w.eventsRepo.AppendEvent(ctx, w.tx, runID, ev.Type, ev.DataJSON, ev.ToolName, ev.ErrorClass); err != nil {
		return err
	}
	w.pendingEventsSinceCommit++

	w.accumUsage(ev.DataJSON)

	if ev.Type == "tool.call" {
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
		w.hasTerminal = true
		return nil
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
	if err := w.tx.Commit(ctx); err != nil {
		return err
	}
	w.tx = nil
	w.pendingEventsSinceCommit = 0
	w.lastCommitAt = time.Now()
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

	if _, err := eventsRepo.AppendEvent(ctx, tx, run.ID, failed.Type, failed.DataJSON, failed.ToolName, failed.ErrorClass); err != nil {
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
			APIKeyValue: apiKey, BaseURL: c.baseURL, OpenAIMode: c.openAIMode, AdvancedJSON: advanced,
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
	dataDir := os.Getenv("ARKLOOP_DATA_DIR")
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return [32]byte{}, err
		}
		dataDir = filepath.Join(home, ".arkloop")
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
