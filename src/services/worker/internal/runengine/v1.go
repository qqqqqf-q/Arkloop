package runengine

import (
	"context"
	"fmt"
	"strings"

	sharedent "arkloop/services/shared/entitlement"
	"arkloop/services/shared/runlimit"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/mcp"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/skills"
	"arkloop/services/worker/internal/tools"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type EngineV1 struct {
	middlewares  []pipeline.RunMiddleware
	terminal     pipeline.RunHandler
	router       *routing.ProviderRouter
	directPool   *pgxpool.Pool
	broadcastRDB *redis.Client
}

type ExecuteInput struct {
	TraceID string
}

type EngineV1Deps struct {
	Router          *routing.ProviderRouter
	DBPool          *pgxpool.Pool
	DirectDBPool    *pgxpool.Pool // LISTEN/NOTIFY 专用直连，不走 PgBouncer；nil 时 Execute 内回落 DBPool
	StubGateway     llm.Gateway
	EmitDebugEvents bool
	RunLimiterRDB   *redis.Client

	ToolRegistry           *tools.Registry
	ToolExecutors          map[string]tools.Executor
	AllLlmToolSpecs        []llm.ToolSpec
	BaseToolAllowlistNames []string

	SkillRegistry *skills.Registry
	MCPPool       *mcp.Pool
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

	// 验证 base 工具集可构建
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
		key := runlimit.Key(run.OrgID.String())
		runlimit.Release(ctx, rdb, key)
	}

	resolver := sharedent.NewResolver(deps.DBPool, rdb)

	middlewares := []pipeline.RunMiddleware{
		pipeline.NewCancelGuardMiddleware(runsRepo, eventsRepo),
		pipeline.NewInputLoaderMiddleware(eventsRepo, messagesRepo),
		pipeline.NewEntitlementMiddleware(resolver, runsRepo, eventsRepo, releaseSlot),
		pipeline.NewMCPDiscoveryMiddleware(
			deps.MCPPool,
			pipeline.CopyToolExecutors(deps.ToolExecutors),
			append([]llm.ToolSpec{}, deps.AllLlmToolSpecs...),
			baseAllowlistSet,
			deps.ToolRegistry,
		),
		pipeline.NewAgentConfigMiddleware(deps.DBPool),
		pipeline.NewSkillResolutionMiddleware(deps.SkillRegistry, deps.DBPool, runsRepo, eventsRepo, releaseSlot),
		pipeline.NewRoutingMiddleware(deps.Router, deps.DBPool, deps.StubGateway, deps.EmitDebugEvents, runsRepo, eventsRepo, releaseSlot, resolver),
		pipeline.NewToolBuildMiddleware(),
	}

	terminal := pipeline.NewAgentLoopHandler(runsRepo, eventsRepo, messagesRepo, deps.RunLimiterRDB, usageRepo, creditsRepo)

	return &EngineV1{
		middlewares:  middlewares,
		terminal:     terminal,
		router:       deps.Router,
		directPool:   deps.DirectDBPool,
		broadcastRDB: deps.RunLimiterRDB, // 复用 RunLimiterRDB；middleware 错误路径通过 RunContext.BroadcastRDB 发布，terminal handler 通过 eventWriter.runLimiterRDB 发布
	}, nil
}

func (e *EngineV1) Execute(ctx context.Context, pool *pgxpool.Pool, run data.Run, input ExecuteInput) error {
	if pool == nil {
		return fmt.Errorf("pool must not be nil")
	}

	traceID := strings.TrimSpace(input.TraceID)

	directPool := e.directPool
	if directPool == nil {
		directPool = pool
	}
	rc := &pipeline.RunContext{
		Run:           run,
		Pool:          pool,
		DirectPool:    directPool,
		BroadcastRDB:  e.broadcastRDB,
		TraceID:       traceID,
		Emitter:       events.NewEmitter(traceID),
		Router:        e.router,
		MaxIterations: 10,
		ToolBudget:    map[string]any{},
	}

	handler := pipeline.Build(e.middlewares, e.terminal)
	return handler(ctx, rc)
}
