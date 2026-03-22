//go:build desktop

package runtime

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"arkloop/services/shared/desktop"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/localshell"
	"arkloop/services/worker/internal/tools/sandboxshell"
)

type DynamicShellExecutor struct {
	mu       sync.RWMutex
	mode     string // "local" | "vm"
	local    *localshell.Executor
	vm       *sandboxshell.Executor
	vmAddr   string
	vmToken  string
}

func NewDynamicShellExecutor(vmAddr, vmToken string) *DynamicShellExecutor {
	initialMode := strings.TrimSpace(desktop.GetExecutionMode())
	if initialMode == "" {
		initialMode = "local"
	}
	return &DynamicShellExecutor{
		mode:    initialMode,
		vmAddr:  strings.TrimSpace(vmAddr),
		vmToken: vmToken,
	}
}

func (e *DynamicShellExecutor) SetMode(mode string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.mode = strings.TrimSpace(mode)
}

func (e *DynamicShellExecutor) currentMode() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.mode
}

func (e *DynamicShellExecutor) ensureLocal() *localshell.Executor {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.local == nil {
		e.local = localshell.NewExecutor()
	}
	return e.local
}

func (e *DynamicShellExecutor) ensureVM() *sandboxshell.Executor {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.vm == nil {
		e.vm = sandboxshell.NewExecutor("http://"+e.vmAddr, e.vmToken)
	}
	return e.vm
}

func (e *DynamicShellExecutor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	toolCallID string,
) tools.ExecutionResult {
	mode := desktop.GetExecutionMode()
	// Read sandbox addr fresh every time — it may be set after this executor was created
	vmAddr := desktop.GetSandboxAddr()
	slog.Info("dynamic_shell_executor: Execute",
		"tool", toolName,
		"mode", mode,
		"vm_addr", vmAddr,
		"run_id", execCtx.RunID.String(),
	)
	if mode == "vm" && vmAddr != "" {
		// Re-initialize VM executor if addr changed
		e.vmAddr = vmAddr
		e.vm = nil
		return e.ensureVM().Execute(ctx, toolName, args, execCtx, toolCallID)
	}
	return e.ensureLocal().Execute(ctx, toolName, args, execCtx, toolCallID)
}
