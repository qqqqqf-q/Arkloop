//go:build desktop && !darwin

package sandboxshell

import (
	"context"
	"time"

	"arkloop/services/worker/internal/tools"
)

type Executor struct{}

func NewExecutor(_, _ string) *Executor { return &Executor{} }

func (e *Executor) IsNotConfigured() bool { return true }

func (e *Executor) Execute(
	_ context.Context,
	_ string,
	_ map[string]any,
	_ tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error:      &tools.ExecutionError{ErrorClass: "tool.sandbox_unavailable", Message: "Apple VM isolation requires macOS"},
		DurationMs: int(time.Millisecond),
	}
}
