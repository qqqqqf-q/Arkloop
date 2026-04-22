package app

import (
	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/shared/objectstore"
	workerdata "arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/tools"
	documentwritetool "arkloop/services/worker/internal/tools/builtin/document_write"
	imagegeneratetool "arkloop/services/worker/internal/tools/builtin/image_generate"
)

// registerStoredArtifactTools wires tools that require persisted artifact storage.
func registerStoredArtifactTools(
	toolRegistry *tools.Registry,
	executors map[string]tools.Executor,
	specs []llm.ToolSpec,
	store objectstore.Store,
	db workerdata.QueryDB,
	configResolver sharedconfig.Resolver,
	routingLoader *routing.ConfigLoader,
) ([]llm.ToolSpec, bool, error) {
	if toolRegistry == nil || executors == nil || store == nil {
		return specs, false, nil
	}

	artifactExecutor := documentwritetool.NewToolExecutor(store)
	imageExecutor := imagegeneratetool.NewToolExecutor(store, db, configResolver, routingLoader)
	registered := false
	for _, item := range []struct {
		agentSpec tools.AgentToolSpec
		llmSpec   llm.ToolSpec
		executor  tools.Executor
	}{
		{agentSpec: documentwritetool.CreateArtifactAgentSpec, llmSpec: documentwritetool.CreateArtifactLlmSpec, executor: artifactExecutor},
		{agentSpec: documentwritetool.AgentSpec, llmSpec: documentwritetool.LlmSpec, executor: artifactExecutor},
		{agentSpec: imagegeneratetool.AgentSpec, llmSpec: imagegeneratetool.LlmSpec, executor: imageExecutor},
	} {
		wasRegistered, err := registerToolIfMissing(toolRegistry, item.agentSpec)
		if err != nil {
			return nil, false, err
		}
		registered = registered || wasRegistered
		executors[item.agentSpec.Name] = item.executor
		specs = appendToolSpecIfMissing(specs, item.llmSpec)
	}

	return specs, registered, nil
}

func registerToolIfMissing(registry *tools.Registry, spec tools.AgentToolSpec) (bool, error) {
	if registry == nil {
		return false, nil
	}
	if _, ok := registry.Get(spec.Name); ok {
		return false, nil
	}
	if err := registry.Register(spec); err != nil {
		return false, err
	}
	return true, nil
}

func appendToolSpecIfMissing(specs []llm.ToolSpec, spec llm.ToolSpec) []llm.ToolSpec {
	for _, existing := range specs {
		if existing.Name == spec.Name {
			return specs
		}
	}
	return append(specs, spec)
}
