package runengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"arkloop/services/worker/internal/agent"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/mcp"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/skills"
	"arkloop/services/worker/internal/tools"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	eventCommitBatchSize      = 20
	eventCommitMaxInterval    = 50 * time.Millisecond
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
	dbPool          *pgxpool.Pool // 非 nil 时每个 run 独立从 DB 加载路由配置
	stubGateway     llm.Gateway
	emitDebugEvents bool
	runLimiterRDB   *redis.Client // nil 时跳过并发计数 DECR

	toolRegistry     *tools.Registry
	toolExecutors    map[string]tools.Executor
	allLlmToolSpecs  []llm.ToolSpec
	baseAllowlistSet map[string]struct{}
	baseToolExecutor *tools.DispatchingExecutor
	baseLlmToolSpecs []llm.ToolSpec

	skillRegistry *skills.Registry
	mcpPool       *mcp.Pool
}

type ExecuteInput struct {
	TraceID string
}

type EngineV1Deps struct {
	Router          *routing.ProviderRouter
	DBPool          *pgxpool.Pool // nil 时跳过 per-run DB 路由加载，回退到静态 Router
	StubGateway     llm.Gateway
	EmitDebugEvents bool
	RunLimiterRDB   *redis.Client // nil 时不执行并发计数 DECR

	ToolRegistry           *tools.Registry
	ToolExecutors          map[string]tools.Executor
	AllLlmToolSpecs        []llm.ToolSpec
	BaseToolAllowlistNames []string

	SkillRegistry *skills.Registry
	MCPPool       *mcp.Pool // 为 nil 时跳过 per-run org MCP 加载
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
		dbPool:           deps.DBPool,
		stubGateway:      deps.StubGateway,
		emitDebugEvents:  deps.EmitDebugEvents,
		runLimiterRDB:    deps.RunLimiterRDB,
		toolRegistry:     deps.ToolRegistry,
		toolExecutors:    copyToolExecutors(deps.ToolExecutors),
		allLlmToolSpecs:  append([]llm.ToolSpec{}, deps.AllLlmToolSpecs...),
		baseAllowlistSet: baseAllowlistSet,
		baseToolExecutor: baseToolExecutor,
		baseLlmToolSpecs: baseLlmSpecs,
		skillRegistry:    deps.SkillRegistry,
		mcpPool:          deps.MCPPool,
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
		return e.appendAndCommit(ctx, pool, run, emitter.Emit("run.cancelled", map[string]any{}, nil, nil))
	}

	terminalType, err := e.readLatestEventType(ctx, pool, run.ID, terminalEventTypes)
	if err != nil {
		return err
	}
	if terminalType != "" {
		return nil
	}

	// 用 LISTEN/NOTIFY 桥接数据库取消信号到 Go context
	listenConn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	channel := fmt.Sprintf(`"run_cancel_%s"`, run.ID.String())
	if _, err := listenConn.Exec(ctx, "LISTEN "+channel); err != nil {
		listenConn.Release()
		return err
	}
	execCtx, cancelExec := context.WithCancel(ctx)
	listenDone := make(chan struct{})
	go func() {
		defer func() {
			listenConn.Release()
			close(listenDone)
		}()
		if err := listenConn.Conn().PgConn().WaitForNotification(execCtx); err == nil {
			cancelExec()
		}
	}()
	defer func() {
		cancelExec()
		<-listenDone
	}()

	inputJSON, threadMessages, err := e.loadRunInputs(execCtx, pool, run)
	if err != nil {
		return err
	}

	// 按 org 加载 MCP 工具，合并到本次 run 的工具集。加载失败不中断 run。
	runToolExecutors := copyToolExecutors(e.toolExecutors)
	runAllLlmSpecs := append([]llm.ToolSpec{}, e.allLlmToolSpecs...)
	runBaseAllowlistSet := copyStringSet(e.baseAllowlistSet)
	runRegistry := e.toolRegistry

	if e.mcpPool != nil {
		orgReg, orgErr := mcp.DiscoverFromDB(execCtx, pool, run.OrgID, e.mcpPool)
		if orgErr == nil && len(orgReg.Executors) > 0 {
			runRegistry = forkRegistry(e.toolRegistry, orgReg.AgentSpecs)
			for name, exec := range orgReg.Executors {
				runToolExecutors[name] = exec
			}
			runAllLlmSpecs = append(runAllLlmSpecs, orgReg.LlmSpecs...)
			// org admin 主动配置的 MCP 工具自动加入本次 run 的 allowlist
			for _, spec := range orgReg.AgentSpecs {
				runBaseAllowlistSet[spec.Name] = struct{}{}
			}
		}
	}

	runBaseExecutor, err := buildDispatchExecutor(runRegistry, runToolExecutors, runBaseAllowlistSet)
	if err != nil {
		return err
	}
	runBaseLlmSpecs := filterToolSpecs(runAllLlmSpecs, runBaseAllowlistSet)

	// per-run 动态加载 org skill，DB skill 覆盖同 ID 的文件系统 skill
	runSkillRegistry := e.skillRegistry
	if e.dbPool != nil {
		dbDefs, dbErr := skills.LoadFromDB(execCtx, e.dbPool, run.OrgID)
		if dbErr != nil {
			slog.WarnContext(execCtx, "skills: db load failed, using static registry", "err", dbErr.Error())
		} else if len(dbDefs) > 0 {
			runSkillRegistry = skills.MergeRegistry(e.skillRegistry, dbDefs)
		}
	}

	skillResolution := skills.ResolveSkill(inputJSON, runSkillRegistry)
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
		return e.appendAndCommit(execCtx, pool, run, failed)
	}

	// per-run 动态路由：每次 run 独立从 DB 加载，与 MCP 动态加载对齐
	activeRouter := e.router
	if e.dbPool != nil {
		dbCfg, dbErr := routing.LoadRoutingConfigFromDB(execCtx, e.dbPool)
		if dbErr != nil {
			slog.WarnContext(execCtx, "routing: per-run db load failed, using static", "err", dbErr.Error())
		} else if len(dbCfg.Routes) > 0 {
			activeRouter = routing.NewProviderRouter(dbCfg)
		}
	}

	decision := activeRouter.Decide(inputJSON, false)
	if decision.Denied != nil {
		failed := emitter.Emit(
			"run.failed",
			decision.Denied.ToRunFailedDataJSON(),
			nil,
			stringPtr(decision.Denied.ErrorClass),
		)
		return e.appendAndCommit(execCtx, pool, run, failed)
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
		return e.appendAndCommit(execCtx, pool, run, failed)
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
		if err := e.appendAndCommit(execCtx, pool, run, failed); err != nil {
			return err
		}
		return nil
	}

	writer := newEventWriter(pool, run, traceID, e.runLimiterRDB)
	defer writer.Close(execCtx)

	routeSelected := emitter.Emit("run.route.selected", selected.ToRunEventDataJSON(), nil, nil)
	if err := writer.Append(execCtx, e.runsRepo, e.eventsRepo, run.ID, routeSelected); err != nil {
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

	toolExecutor := runBaseExecutor
	toolSpecs := runBaseLlmSpecs
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
			if _, ok := runBaseAllowlistSet[name]; !ok {
				continue
			}
			effectiveAllowlist[name] = struct{}{}
		}
		exec, err := buildDispatchExecutor(runRegistry, runToolExecutors, effectiveAllowlist)
		if err != nil {
			return err
		}
		toolExecutor = exec
		toolSpecs = filterToolSpecs(runAllLlmSpecs, effectiveAllowlist)
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
			return execCtx.Err() != nil
		},
	}

	err = loop.Run(execCtx, runCtx, agentRequest, emitter, func(ev events.RunEvent) error {
		if err := writer.Append(execCtx, e.runsRepo, e.eventsRepo, run.ID, ev); err != nil {
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
		if err := writer.InsertAssistantMessage(execCtx, e.messagesRepo, run.OrgID, run.ThreadID); err != nil {
			return err
		}
	}
	return writer.Flush(execCtx)
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

func (e *EngineV1) appendAndCommit(ctx context.Context, pool *pgxpool.Pool, run data.Run, ev events.RunEvent) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := e.eventsRepo.AppendEvent(ctx, tx, run.ID, ev.Type, ev.DataJSON, ev.ToolName, ev.ErrorClass); err != nil {
		return err
	}

	if status, ok := terminalStatuses[ev.Type]; ok {
		if err := e.runsRepo.UpdateRunTerminalStatus(ctx, tx, run.ID, data.TerminalStatusUpdate{
			Status: status,
		}); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	channel := fmt.Sprintf("run_events:%s", run.ID.String())
	_, _ = pool.Exec(ctx, "SELECT pg_notify($1, '')", channel)

	if _, ok := terminalStatuses[ev.Type]; ok {
		e.releaseRunSlot(ctx, run.OrgID)
	}

	return nil
}

// releaseRunSlot 将 org 的并发 run 计数减 1，计数不低于 0。
func (e *EngineV1) releaseRunSlot(ctx context.Context, orgID uuid.UUID) {
	if e.runLimiterRDB == nil {
		return
	}
	key := "arkloop:org:active_runs:" + orgID.String()
	result := e.runLimiterRDB.Decr(ctx, key)
	if result.Err() == nil && result.Val() < 0 {
		_ = e.runLimiterRDB.Set(ctx, key, 0, 0)
	}
}

func (e *EngineV1) gatewayFromCredential(credential routing.ProviderCredential) (llm.Gateway, error) {
	switch credential.ProviderKind {
	case routing.ProviderKindStub:
		return e.stubGateway, nil
	case routing.ProviderKindOpenAI:
		apiKey, err := resolveAPIKey(credential)
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
		apiKey, err := resolveAPIKey(credential)
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

// resolveAPIKey 优先使用 APIKeyValue（来自数据库），回退到 APIKeyEnv（环境变量名）。
func resolveAPIKey(credential routing.ProviderCredential) (string, error) {
	if credential.APIKeyValue != nil && strings.TrimSpace(*credential.APIKeyValue) != "" {
		return *credential.APIKeyValue, nil
	}
	return lookupAPIKey(credential.APIKeyEnv)
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

func copyStringSet(src map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(src))
	for k := range src {
		out[k] = struct{}{}
	}
	return out
}

// forkRegistry 创建一个包含 base 所有 spec + 额外 spec 的新 Registry。
// base 是只读的，不会被修改。
func forkRegistry(base *tools.Registry, extras []tools.AgentToolSpec) *tools.Registry {
	r := tools.NewRegistry()
	for _, name := range base.ListNames() {
		spec, ok := base.Get(name)
		if ok {
			_ = r.Register(spec) // 从已有 registry 复制，不会出错
		}
	}
	for _, spec := range extras {
		if err := r.Register(spec); err != nil {
			slog.Warn("mcp tool name conflict, skipped", "name", spec.Name)
		}
	}
	return r
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

// terminalStatuses maps terminal event types to runs.status values.
var terminalStatuses = map[string]string{
	"run.completed": "completed",
	"run.failed":    "failed",
	"run.cancelled": "cancelled",
}

type eventWriter struct {
	pool          *pgxpool.Pool
	run           data.Run
	traceID       string
	runLimiterRDB *redis.Client

	tx                       pgx.Tx
	pendingEventsSinceCommit int
	lastCommitAt             time.Time
	assistantDeltas          []string
	completed                bool
	hasTerminal              bool

	totalInputTokens  int64
	totalOutputTokens int64
	totalCostUSD      float64
}

func newEventWriter(pool *pgxpool.Pool, run data.Run, traceID string, runLimiterRDB *redis.Client) *eventWriter {
	return &eventWriter{
		pool:          pool,
		run:           run,
		traceID:       strings.TrimSpace(traceID),
		lastCommitAt:  time.Now(),
		runLimiterRDB: runLimiterRDB,
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
		if err := runsRepo.UpdateRunTerminalStatus(ctx, w.tx, runID, data.TerminalStatusUpdate{
			Status:            "cancelled",
			TotalInputTokens:  w.totalInputTokens,
			TotalOutputTokens: w.totalOutputTokens,
			TotalCostUSD:      w.totalCostUSD,
		}); err != nil {
			return err
		}
		w.hasTerminal = true
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

	w.accumUsage(ev.DataJSON)

	if ev.Type == "message.delta" {
		if delta := extractAssistantDelta(ev.DataJSON); delta != "" {
			w.assistantDeltas = append(w.assistantDeltas, delta)
		}
	}

	if status, ok := terminalStatuses[ev.Type]; ok {
		if status == "completed" {
			w.completed = true
		}
		if err := runsRepo.UpdateRunTerminalStatus(ctx, w.tx, runID, data.TerminalStatusUpdate{
			Status:            status,
			TotalInputTokens:  w.totalInputTokens,
			TotalOutputTokens: w.totalOutputTokens,
			TotalCostUSD:      w.totalCostUSD,
		}); err != nil {
			return err
		}
		w.hasTerminal = true
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

	// notify SSE listeners
	channel := fmt.Sprintf("run_events:%s", w.run.ID.String())
	_, _ = w.pool.Exec(ctx, "SELECT pg_notify($1, '')", channel)

	if w.hasTerminal {
		w.hasTerminal = false
		if w.runLimiterRDB != nil {
			key := "arkloop:org:active_runs:" + w.run.OrgID.String()
			result := w.runLimiterRDB.Decr(ctx, key)
			if result.Err() == nil && result.Val() < 0 {
				_ = w.runLimiterRDB.Set(ctx, key, 0, 0)
			}
		}
	}

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

// accumUsage extracts usage/cost from event data and accumulates into writer totals.
func (w *eventWriter) accumUsage(dataJSON map[string]any) {
	if dataJSON == nil {
		return
	}
	if usage, ok := dataJSON["usage"].(map[string]any); ok {
		if v, ok := toInt64(usage["input_tokens"]); ok {
			w.totalInputTokens += v
		}
		if v, ok := toInt64(usage["output_tokens"]); ok {
			w.totalOutputTokens += v
		}
	}
	if cost, ok := dataJSON["cost"].(map[string]any); ok {
		if v, ok := toInt64(cost["amount_micros"]); ok {
			w.totalCostUSD += float64(v) / 1_000_000.0
		}
	}
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}
