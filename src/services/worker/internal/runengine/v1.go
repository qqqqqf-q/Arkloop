package runengine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"arkloop/services/worker/internal/agent"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/skills"
	"arkloop/services/worker/internal/tools"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	eventCommitBatchSize      = 20
	eventCommitMaxInterval    = 200 * time.Millisecond
	defaultAgentMaxIterations = 10
	threadMessageLimit        = 200
)

var (
	cancelEventTypes    = []string{"run.cancel_requested", "run.cancelled"}
	terminalEventTypes  = []string{"run.completed", "run.failed", "run.cancelled"}
	streamingEventTypes = map[string]struct{}{
		"message.delta":      {},
		"llm.response.chunk": {},
	}
	errStopProcessing = errors.New("stop_processing")
)

type EngineV1 struct {
	runsRepo     data.RunsRepository
	eventsRepo   data.RunEventsRepository
	messagesRepo data.MessagesRepository

	router          *routing.ProviderRouter
	stubGateway     llm.Gateway
	emitDebugEvents bool

	toolRegistry     *tools.Registry
	toolExecutors    map[string]tools.Executor
	allLlmToolSpecs  []llm.ToolSpec
	baseAllowlistSet map[string]struct{}
	baseToolExecutor *tools.DispatchingExecutor
	baseLlmToolSpecs []llm.ToolSpec

	skillRegistry *skills.Registry
}

type ExecuteInput struct {
	TraceID string
}

type EngineV1Deps struct {
	Router          *routing.ProviderRouter
	StubGateway     llm.Gateway
	EmitDebugEvents bool

	ToolRegistry           *tools.Registry
	ToolExecutors          map[string]tools.Executor
	AllLlmToolSpecs        []llm.ToolSpec
	BaseToolAllowlistNames []string

	SkillRegistry *skills.Registry
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

	baseToolExecutor, err := buildDispatchExecutor(deps.ToolRegistry, deps.ToolExecutors, baseAllowlistSet)
	if err != nil {
		return nil, err
	}
	baseLlmSpecs := filterToolSpecs(deps.AllLlmToolSpecs, baseAllowlistSet)

	return &EngineV1{
		runsRepo:         data.RunsRepository{},
		eventsRepo:       data.RunEventsRepository{},
		messagesRepo:     data.MessagesRepository{},
		router:           deps.Router,
		stubGateway:      deps.StubGateway,
		emitDebugEvents:  deps.EmitDebugEvents,
		toolRegistry:     deps.ToolRegistry,
		toolExecutors:    copyToolExecutors(deps.ToolExecutors),
		allLlmToolSpecs:  append([]llm.ToolSpec{}, deps.AllLlmToolSpecs...),
		baseAllowlistSet: baseAllowlistSet,
		baseToolExecutor: baseToolExecutor,
		baseLlmToolSpecs: baseLlmSpecs,
		skillRegistry:    deps.SkillRegistry,
	}, nil
}

func (e *EngineV1) Execute(ctx context.Context, pool *pgxpool.Pool, run data.Run, input ExecuteInput) error {
	if pool == nil {
		return fmt.Errorf("pool must not be nil")
	}

	traceID := strings.TrimSpace(input.TraceID)
	emitter := events.NewEmitter(traceID)

	cancelType, err := e.readLatestEventType(ctx, pool, run.ID, cancelEventTypes)
	if err != nil {
		return err
	}
	if cancelType == "run.cancelled" {
		return nil
	}
	if cancelType == "run.cancel_requested" {
		return e.appendAndCommit(ctx, pool, run.ID, emitter.Emit("run.cancelled", map[string]any{}, nil, nil))
	}

	terminalType, err := e.readLatestEventType(ctx, pool, run.ID, terminalEventTypes)
	if err != nil {
		return err
	}
	if terminalType != "" {
		return nil
	}

	inputJSON, threadMessages, err := e.loadRunInputs(ctx, pool, run)
	if err != nil {
		return err
	}

	skillResolution := skills.ResolveSkill(inputJSON, e.skillRegistry)
	if skillResolution.Error != nil {
		payload := map[string]any{
			"error_class": skillResolution.Error.ErrorClass,
			"message":     skillResolution.Error.Message,
		}
		if len(skillResolution.Error.Details) > 0 {
			payload["details"] = skillResolution.Error.Details
		}
		failed := emitter.Emit(
			"run.failed",
			payload,
			nil,
			stringPtr(skillResolution.Error.ErrorClass),
		)
		return e.appendAndCommit(ctx, pool, run.ID, failed)
	}

	decision := e.router.Decide(inputJSON, false)
	if decision.Denied != nil {
		failed := emitter.Emit(
			"run.failed",
			decision.Denied.ToRunFailedDataJSON(),
			nil,
			stringPtr(decision.Denied.ErrorClass),
		)
		return e.appendAndCommit(ctx, pool, run.ID, failed)
	}

	selected := decision.Selected
	if selected == nil {
		failed := emitter.Emit(
			"run.failed",
			map[string]any{
				"error_class": llm.ErrorClassInternalError,
				"code":        "internal.route_missing",
				"message":     "route decision is empty",
			},
			nil,
			stringPtr(llm.ErrorClassInternalError),
		)
		return e.appendAndCommit(ctx, pool, run.ID, failed)
	}

	gateway, err := e.gatewayFromCredential(selected.Credential)
	if err != nil {
		failed := emitter.Emit(
			"run.failed",
			map[string]any{
				"error_class": llm.ErrorClassInternalError,
				"code":        "internal.gateway_init_failed",
				"message":     "gateway initialization failed",
			},
			nil,
			stringPtr(llm.ErrorClassInternalError),
		)
		if err := e.appendAndCommit(ctx, pool, run.ID, failed); err != nil {
			return err
		}
		return nil
	}

	writer := newEventWriter(pool, run, traceID)
	defer writer.Close(ctx)

	routeSelected := emitter.Emit("run.route.selected", selected.ToRunEventDataJSON(), nil, nil)
	if err := writer.Append(ctx, e.runsRepo, e.eventsRepo, run.ID, routeSelected); err != nil {
		if errors.Is(err, errStopProcessing) {
			return nil
		}
		return err
	}

	llmMessages := make([]llm.Message, 0, len(threadMessages))
	for _, msg := range threadMessages {
		if strings.TrimSpace(msg.Role) == "" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		parts := []llm.TextPart{}
		if content != "" {
			parts = append(parts, llm.TextPart{Text: content})
		}
		llmMessages = append(llmMessages, llm.Message{
			Role:    msg.Role,
			Content: parts,
		})
	}

	toolExecutor := e.baseToolExecutor
	toolSpecs := e.baseLlmToolSpecs
	maxIterations := defaultAgentMaxIterations
	var maxOutputTokens *int
	var toolTimeoutMs *int
	toolBudget := map[string]any{}
	systemPrompt := ""

	if skillResolution.Definition != nil {
		def := skillResolution.Definition
		systemPrompt = def.PromptMD
		if def.Budgets.MaxIterations != nil {
			maxIterations = *def.Budgets.MaxIterations
		}
		maxOutputTokens = def.Budgets.MaxOutputTokens
		toolTimeoutMs = def.Budgets.ToolTimeoutMs
		for key, value := range def.Budgets.ToolBudget {
			toolBudget[key] = value
		}

		effectiveAllowlist := map[string]struct{}{}
		for _, name := range def.ToolAllowlist {
			if _, ok := e.baseAllowlistSet[name]; !ok {
				continue
			}
			effectiveAllowlist[name] = struct{}{}
		}
		exec, err := buildDispatchExecutor(e.toolRegistry, e.toolExecutors, effectiveAllowlist)
		if err != nil {
			return err
		}
		toolExecutor = exec
		toolSpecs = filterToolSpecs(e.allLlmToolSpecs, effectiveAllowlist)
	}

	if strings.TrimSpace(systemPrompt) != "" {
		llmMessages = append([]llm.Message{
			{
				Role:    "system",
				Content: []llm.TextPart{{Text: systemPrompt}},
			},
		}, llmMessages...)
	}

	agentRequest := llm.Request{
		Model:           selected.Route.Model,
		Messages:        llmMessages,
		Tools:           append([]llm.ToolSpec{}, toolSpecs...),
		MaxOutputTokens: maxOutputTokens,
	}

	loop := agent.NewLoop(gateway, toolExecutor)
	runCtx := agent.RunContext{
		RunID:         run.ID,
		TraceID:       traceID,
		InputJSON:     inputJSON,
		MaxIterations: maxIterations,
		ToolExecutor:  toolExecutor,
		ToolTimeoutMs: toolTimeoutMs,
		ToolBudget:    toolBudget,
		CancelSignal: func() bool {
			return ctx.Err() != nil
		},
	}

	err = loop.Run(ctx, runCtx, agentRequest, emitter, func(ev events.RunEvent) error {
		if err := writer.Append(ctx, e.runsRepo, e.eventsRepo, run.ID, ev); err != nil {
			if errors.Is(err, errStopProcessing) {
				return errStopProcessing
			}
			return err
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopProcessing) {
		return err
	}

	if writer.Completed() {
		if err := writer.InsertAssistantMessage(ctx, e.messagesRepo, run.OrgID, run.ThreadID); err != nil {
			return err
		}
	}
	return writer.Flush(ctx)
}

func (e *EngineV1) loadRunInputs(
	ctx context.Context,
	pool *pgxpool.Pool,
	run data.Run,
) (map[string]any, []data.ThreadMessage, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)

	_, dataJSON, err := e.eventsRepo.FirstEventData(ctx, tx, run.ID)
	if err != nil {
		return nil, nil, err
	}

	inputJSON := map[string]any{
		"org_id":    run.OrgID.String(),
		"thread_id": run.ThreadID.String(),
	}
	if dataJSON != nil {
		if rawRouteID, ok := dataJSON["route_id"].(string); ok && strings.TrimSpace(rawRouteID) != "" {
			inputJSON["route_id"] = strings.TrimSpace(rawRouteID)
		}
		if rawSkillID, ok := dataJSON["skill_id"].(string); ok && strings.TrimSpace(rawSkillID) != "" {
			inputJSON["skill_id"] = strings.TrimSpace(rawSkillID)
		}
	}

	messages, err := e.messagesRepo.ListByThread(ctx, tx, run.OrgID, run.ThreadID, threadMessageLimit)
	if err != nil {
		return nil, nil, err
	}

	return inputJSON, messages, nil
}

func (e *EngineV1) readLatestEventType(
	ctx context.Context,
	pool *pgxpool.Pool,
	runID uuid.UUID,
	types []string,
) (string, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	return e.eventsRepo.GetLatestEventType(ctx, tx, runID, types)
}

func (e *EngineV1) appendAndCommit(ctx context.Context, pool *pgxpool.Pool, runID uuid.UUID, ev events.RunEvent) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := e.eventsRepo.AppendEvent(ctx, tx, runID, ev.Type, ev.DataJSON, ev.ToolName, ev.ErrorClass); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (e *EngineV1) gatewayFromCredential(credential routing.ProviderCredential) (llm.Gateway, error) {
	switch credential.ProviderKind {
	case routing.ProviderKindStub:
		return e.stubGateway, nil
	case routing.ProviderKindOpenAI:
		apiKey, err := lookupAPIKey(credential.APIKeyEnv)
		if err != nil {
			return nil, err
		}
		baseURL := ""
		if credential.BaseURL != nil {
			baseURL = *credential.BaseURL
		}
		apiMode := "auto"
		if credential.OpenAIMode != nil {
			apiMode = *credential.OpenAIMode
		}
		return llm.NewOpenAIGateway(llm.OpenAIGatewayConfig{
			APIKey:          apiKey,
			BaseURL:         baseURL,
			APIMode:         apiMode,
			EmitDebugEvents: e.emitDebugEvents,
		}), nil
	case routing.ProviderKindAnthropic:
		apiKey, err := lookupAPIKey(credential.APIKeyEnv)
		if err != nil {
			return nil, err
		}
		baseURL := ""
		if credential.BaseURL != nil {
			baseURL = *credential.BaseURL
		}
		return llm.NewAnthropicGateway(llm.AnthropicGatewayConfig{
			APIKey:          apiKey,
			BaseURL:         baseURL,
			AdvancedJSON:    credential.AdvancedJSON,
			EmitDebugEvents: e.emitDebugEvents,
		}), nil
	default:
		return nil, fmt.Errorf("unknown provider_kind: %s", credential.ProviderKind)
	}
}

func lookupAPIKey(envName *string) (string, error) {
	if envName == nil || strings.TrimSpace(*envName) == "" {
		return "", fmt.Errorf("missing api_key_env")
	}
	name := strings.TrimSpace(*envName)
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("missing environment variable %s", name)
	}
	return value, nil
}

func stringPtr(value string) *string {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}

func copyToolExecutors(values map[string]tools.Executor) map[string]tools.Executor {
	out := map[string]tools.Executor{}
	for key, executor := range values {
		out[key] = executor
	}
	return out
}

func buildDispatchExecutor(
	registry *tools.Registry,
	executors map[string]tools.Executor,
	allowlistSet map[string]struct{},
) (*tools.DispatchingExecutor, error) {
	allowlistNames := make([]string, 0, len(allowlistSet))
	for name := range allowlistSet {
		allowlistNames = append(allowlistNames, name)
	}
	sort.Strings(allowlistNames)

	allowlist := tools.AllowlistFromNames(allowlistNames)
	policy := tools.NewPolicyEnforcer(registry, allowlist)
	dispatch := tools.NewDispatchingExecutor(registry, policy)
	for toolName, bound := range executors {
		if err := dispatch.Bind(toolName, bound); err != nil {
			return nil, err
		}
	}
	return dispatch, nil
}

func filterToolSpecs(specs []llm.ToolSpec, allowlistSet map[string]struct{}) []llm.ToolSpec {
	if len(allowlistSet) == 0 {
		return nil
	}
	out := make([]llm.ToolSpec, 0, len(specs))
	for _, spec := range specs {
		if _, ok := allowlistSet[spec.Name]; !ok {
			continue
		}
		out = append(out, spec)
	}
	return out
}

type eventWriter struct {
	pool    *pgxpool.Pool
	run     data.Run
	traceID string

	tx                       pgx.Tx
	pendingEventsSinceCommit int
	lastCommitAt             time.Time
	assistantDeltas          []string
	completed                bool
}

func newEventWriter(pool *pgxpool.Pool, run data.Run, traceID string) *eventWriter {
	return &eventWriter{
		pool:         pool,
		run:          run,
		traceID:      strings.TrimSpace(traceID),
		lastCommitAt: time.Now(),
	}
}

func (w *eventWriter) ensureTx(ctx context.Context) error {
	if w.tx != nil {
		return nil
	}
	tx, err := w.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	w.tx = tx
	w.lastCommitAt = time.Now()
	return nil
}

func (w *eventWriter) Append(
	ctx context.Context,
	runsRepo data.RunsRepository,
	eventsRepo data.RunEventsRepository,
	runID uuid.UUID,
	ev events.RunEvent,
) error {
	if err := w.ensureTx(ctx); err != nil {
		return err
	}

	if err := runsRepo.LockRunRow(ctx, w.tx, runID); err != nil {
		return err
	}

	cancelType, err := eventsRepo.GetLatestEventType(ctx, w.tx, runID, cancelEventTypes)
	if err != nil {
		return err
	}
	if cancelType == "run.cancel_requested" {
		emitter := events.NewEmitter(w.traceID)
		cancelled := emitter.Emit("run.cancelled", map[string]any{}, nil, nil)
		if _, err := eventsRepo.AppendEvent(ctx, w.tx, runID, cancelled.Type, cancelled.DataJSON, cancelled.ToolName, cancelled.ErrorClass); err != nil {
			return err
		}
		if err := w.commit(ctx); err != nil {
			return err
		}
		return errStopProcessing
	}
	if cancelType == "run.cancelled" {
		if err := w.commit(ctx); err != nil {
			return err
		}
		return errStopProcessing
	}

	if _, err := eventsRepo.AppendEvent(ctx, w.tx, runID, ev.Type, ev.DataJSON, ev.ToolName, ev.ErrorClass); err != nil {
		return err
	}
	w.pendingEventsSinceCommit++

	if ev.Type == "message.delta" {
		if delta := extractAssistantDelta(ev.DataJSON); delta != "" {
			w.assistantDeltas = append(w.assistantDeltas, delta)
		}
	} else if ev.Type == "run.completed" {
		w.completed = true
		return nil
	}

	if _, ok := streamingEventTypes[ev.Type]; !ok {
		return w.commit(ctx)
	}

	now := time.Now()
	if w.pendingEventsSinceCommit >= eventCommitBatchSize || now.Sub(w.lastCommitAt) >= eventCommitMaxInterval {
		return w.commit(ctx)
	}
	return nil
}

func (w *eventWriter) commit(ctx context.Context) error {
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

func (w *eventWriter) Completed() bool {
	return w.completed
}

func (w *eventWriter) InsertAssistantMessage(
	ctx context.Context,
	repo data.MessagesRepository,
	orgID uuid.UUID,
	threadID uuid.UUID,
) error {
	if err := w.ensureTx(ctx); err != nil {
		return err
	}
	content := strings.Join(w.assistantDeltas, "")
	return repo.InsertAssistantMessage(ctx, w.tx, orgID, threadID, content)
}

func (w *eventWriter) Flush(ctx context.Context) error {
	return w.commit(ctx)
}

func (w *eventWriter) Close(ctx context.Context) {
	if w.tx != nil {
		_ = w.tx.Rollback(ctx)
		w.tx = nil
	}
}

func extractAssistantDelta(dataJSON map[string]any) string {
	role, ok := dataJSON["role"]
	if ok && role != nil && role != "assistant" {
		return ""
	}
	delta, _ := dataJSON["content_delta"].(string)
	if delta == "" {
		return ""
	}
	return delta
}
