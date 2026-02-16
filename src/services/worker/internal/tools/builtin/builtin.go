package builtin

import (
	webfetch "arkloop/services/worker/internal/tools/builtin/web_fetch"
	websearch "arkloop/services/worker/internal/tools/builtin/web_search"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

func AgentSpecs() []tools.AgentToolSpec {
	return []tools.AgentToolSpec{
		EchoAgentSpec,
		NoopAgentSpec,
		websearch.AgentSpec,
		webfetch.AgentSpec,
	}
}

func LlmSpecs() []llm.ToolSpec {
	return []llm.ToolSpec{
		EchoLlmSpec,
		NoopLlmSpec,
		websearch.LlmSpec,
		webfetch.LlmSpec,
	}
}

func Executors() map[string]tools.Executor {
	return map[string]tools.Executor{
		EchoAgentSpec.Name:     EchoExecutor{},
		NoopAgentSpec.Name:     NoopExecutor{},
		websearch.AgentSpec.Name: websearch.NewToolExecutor(),
		webfetch.AgentSpec.Name:  webfetch.NewToolExecutor(),
	}
}

