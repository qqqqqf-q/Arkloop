//go:build !desktop

package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	sharedtoolruntime "arkloop/services/shared/toolruntime"
	"arkloop/services/worker/internal/tools"
)

const (
	errorNotConfigured = "tool.not_configured"
	errorUnavailable   = "tool.unavailable"
)

type DynamicSandboxExecutor struct {
	manager *Manager
	factory *SandboxExecutorFactory
}

func NewDynamicSandboxExecutor(manager *Manager, factory *SandboxExecutorFactory) *DynamicSandboxExecutor {
	return &DynamicSandboxExecutor{manager: manager, factory: factory}
}

func (e *DynamicSandboxExecutor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	toolCallID string,
) tools.ExecutionResult {
	started := time.Now()
	snapshot, err := resolveSnapshot(ctx, execCtx.RuntimeSnapshot, e.manager)
	if err != nil {
		return runtimeError(errorUnavailable, err.Error(), started)
	}
	if snapshot.SandboxBaseURL == "" {
		return runtimeError(errorNotConfigured, "sandbox service not configured", started)
	}
	if strings.TrimSpace(toolName) == "browser" && !snapshot.BrowserEnabled {
		return runtimeError(errorNotConfigured, "browser tool not configured", started)
	}
	inner := e.factory.Resolve(snapshot)
	if inner == nil {
		return runtimeError(errorUnavailable, "sandbox executor unavailable", started)
	}
	return inner.Execute(ctx, toolName, args, execCtx, toolCallID)
}

type DynamicMemoryExecutor struct {
	manager         *Manager
	providerFactory *MemoryProviderFactory
	executorFactory *MemoryExecutorFactory
}

func NewDynamicMemoryExecutor(
	manager *Manager,
	providerFactory *MemoryProviderFactory,
	executorFactory *MemoryExecutorFactory,
) *DynamicMemoryExecutor {
	return &DynamicMemoryExecutor{
		manager:         manager,
		providerFactory: providerFactory,
		executorFactory: executorFactory,
	}
}

func (e *DynamicMemoryExecutor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	toolCallID string,
) tools.ExecutionResult {
	started := time.Now()
	snapshot, err := resolveSnapshot(ctx, execCtx.RuntimeSnapshot, e.manager)
	if err != nil {
		return runtimeError(errorUnavailable, err.Error(), started)
	}
	provider := e.providerFactory.Resolve(snapshot)
	if provider == nil {
		return runtimeError(errorNotConfigured, "memory provider not configured", started)
	}
	inner := e.executorFactory.Resolve(snapshot, provider)
	if inner == nil {
		return runtimeError(errorUnavailable, "memory executor unavailable", started)
	}
	return inner.Execute(ctx, toolName, args, execCtx, toolCallID)
}

func resolveSnapshot(
	ctx context.Context,
	snapshot *sharedtoolruntime.RuntimeSnapshot,
	manager *Manager,
) (sharedtoolruntime.RuntimeSnapshot, error) {
	if snapshot != nil {
		return *snapshot, nil
	}
	if manager == nil {
		return sharedtoolruntime.RuntimeSnapshot{}, fmt.Errorf("runtime snapshot not available")
	}
	return manager.Current(ctx)
}

func runtimeError(errorClass, message string, started time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error: &tools.ExecutionError{
			ErrorClass: errorClass,
			Message:    message,
		},
		DurationMs: int(time.Since(started) / time.Millisecond),
	}
}
