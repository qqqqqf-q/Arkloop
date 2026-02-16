package app

import (
	"context"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/mcp"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/runengine"
	"arkloop/services/worker/internal/skills"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin"
)

func ComposeNativeEngine(ctx context.Context) (*runengine.EngineV1, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	routingCfg, err := routing.LoadRoutingConfigFromEnv()
	if err != nil {
		return nil, err
	}
	router := routing.NewProviderRouter(routingCfg)

	stubCfg, err := llm.StubGatewayConfigFromEnv()
	if err != nil {
		return nil, err
	}
	stubGateway := llm.NewStubGateway(stubCfg)

	toolRegistry := tools.NewRegistry()
	for _, spec := range builtin.AgentSpecs() {
		if err := toolRegistry.Register(spec); err != nil {
			return nil, err
		}
	}

	executors := builtin.Executors()
	allLlmSpecs := builtin.LlmSpecs()

	mcpPool := mcp.NewPool()
	mcpRegistration, err := mcp.DiscoverFromEnv(ctx, mcpPool)
	if err != nil {
		return nil, err
	}
	for _, spec := range mcpRegistration.AgentSpecs {
		if err := toolRegistry.Register(spec); err != nil {
			return nil, err
		}
	}
	for name, executor := range mcpRegistration.Executors {
		executors[name] = executor
	}
	allLlmSpecs = append(allLlmSpecs, mcpRegistration.LlmSpecs...)

	baseAllowlistNames := tools.ParseAllowlistNamesFromEnv()

	skillsRoot, err := skills.BuiltinSkillsRoot()
	if err != nil {
		return nil, err
	}
	skillRegistry, err := skills.LoadRegistry(skillsRoot)
	if err != nil {
		return nil, err
	}

	return runengine.NewEngineV1(runengine.EngineV1Deps{
		Router:                router,
		StubGateway:           stubGateway,
		EmitDebugEvents:       stubCfg.EmitDebugEvents,
		ToolRegistry:          toolRegistry,
		ToolExecutors:         executors,
		AllLlmToolSpecs:       allLlmSpecs,
		BaseToolAllowlistNames: baseAllowlistNames,
		SkillRegistry:         skillRegistry,
	})
}
