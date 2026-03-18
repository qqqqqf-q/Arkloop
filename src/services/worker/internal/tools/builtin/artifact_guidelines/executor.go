package artifactguidelines

import (
	"context"
	"time"

	generativeuisource "arkloop/services/worker/internal/tools/builtin/generative_ui_source"
	"arkloop/services/worker/internal/tools"
)

type ToolExecutor struct{}

func (e ToolExecutor) Execute(
	_ context.Context,
	_ string,
	args map[string]any,
	_ tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	started := time.Now()

	rawModules, _ := args["modules"].([]any)
	modules := make([]string, 0, len(rawModules))
	for _, item := range rawModules {
		if s, ok := item.(string); ok {
			modules = append(modules, s)
		}
	}

	document, err := generativeuisource.BuildDocument(modules)
	if err != nil {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: "tool.execution_failed",
				Message:    err.Error(),
			},
			DurationMs: durationMs(started),
		}
	}

	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"source":     document.Source,
			"modules":    document.Modules,
			"guidelines": document.Content,
		},
		DurationMs: durationMs(started),
	}
}

func durationMs(started time.Time) int {
	elapsed := time.Since(started)
	millis := int(elapsed / time.Millisecond)
	if millis < 0 {
		return 0
	}
	return millis
}
