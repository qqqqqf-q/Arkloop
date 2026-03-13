//go:build desktop

package memory

import (
	"context"

	"arkloop/services/worker/internal/tools"
)

type ToolExecutor struct{}

func (e *ToolExecutor) Execute(_ context.Context, _ string, _ map[string]any, _ tools.ExecutionContext, _ string) tools.ExecutionResult {
	return tools.ExecutionResult{Error: &tools.ExecutionError{ErrorClass: "unsupported", Message: "memory tools not available in desktop mode"}}
}
