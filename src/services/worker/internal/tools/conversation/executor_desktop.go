//go:build desktop

package conversation

import (
	"context"

	"arkloop/services/worker/internal/tools"
)

type ToolExecutor struct{}

func NewToolExecutor(_ any, _ any) *ToolExecutor {
	return &ToolExecutor{}
}

func (e *ToolExecutor) Execute(_ context.Context, _ string, _ map[string]any, _ tools.ExecutionContext, _ string) tools.ExecutionResult {
	return tools.ExecutionResult{Error: &tools.ExecutionError{ErrorClass: "unsupported", Message: "conversation tools not available in desktop mode"}}
}
