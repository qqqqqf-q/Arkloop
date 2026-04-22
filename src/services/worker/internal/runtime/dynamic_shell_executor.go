//go:build desktop

package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"arkloop/services/shared/desktop"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/fileops"
	"arkloop/services/worker/internal/tools/localshell"
	"arkloop/services/worker/internal/tools/sandboxshell"
)

type DynamicShellExecutor struct {
	mu            sync.RWMutex
	local         *localshell.Executor
	vm            *sandboxshell.Executor
	vmAddr        string
	vmToken       string
	processOwners map[string]string
	processRuns   map[string]string
	fileTracker   *fileops.FileTracker
}

func NewDynamicShellExecutor(vmAddr, vmToken string, ft *fileops.FileTracker) *DynamicShellExecutor {
	return &DynamicShellExecutor{
		vmAddr:        strings.TrimSpace(vmAddr),
		vmToken:       vmToken,
		processOwners: map[string]string{},
		processRuns:   map[string]string{},
		fileTracker:   ft,
	}
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
	addr := strings.TrimSpace(desktop.GetSandboxAddr())
	if addr != "" && addr != e.vmAddr {
		e.vmAddr = addr
		e.vm = nil
	}
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
	mode := strings.TrimSpace(desktop.GetExecutionMode())
	vmAddr := strings.TrimSpace(desktop.GetSandboxAddr())
	slog.Info("dynamic_shell_executor: Execute",
		"tool", toolName,
		"mode", mode,
		"vm_addr", vmAddr,
		"run_id", execCtx.RunID.String(),
	)

	backend, routeErr := e.resolveBackend(toolName, args, mode, vmAddr)
	if routeErr != nil {
		return tools.ExecutionResult{
			Error:      &tools.ExecutionError{ErrorClass: "tool.args_invalid", Message: routeErr.Error()},
			DurationMs: 0,
		}
	}

	var result tools.ExecutionResult
	switch backend {
	case "vm":
		result = e.ensureVM().Execute(ctx, toolName, args, execCtx, toolCallID)
	default:
		result = e.ensureLocal().Execute(ctx, toolName, args, execCtx, toolCallID)
	}

	e.reconcileProcessOwner(toolName, backend, execCtx, args, result)

	// invalidate FileTracker read state for files the command may have modified
	if e.fileTracker != nil && toolName == localshell.ExecCommandAgentSpec.Name && backend == "local" {
		command, _ := args["command"].(string)
		cwd := execCtx.WorkDir
		for _, p := range localshell.DetectModifiedFiles(command, cwd) {
			e.fileTracker.InvalidateReadState(execCtx.RunID.String(), p)
		}
	}

	return result
}

func (e *DynamicShellExecutor) resolveBackend(toolName string, args map[string]any, mode string, vmAddr string) (string, error) {
	switch toolName {
	case localshell.ExecCommandAgentSpec.Name:
		if mode == "vm" && vmAddr != "" {
			return "vm", nil
		}
		return "local", nil
	case localshell.ContinueProcessAgentSpec.Name,
		localshell.TerminateProcessAgentSpec.Name,
		localshell.ResizeProcessAgentSpec.Name:
		processRef, _ := args["process_ref"].(string)
		processRef = strings.TrimSpace(processRef)
		if processRef == "" {
			return "", fmt.Errorf("parameter process_ref is required")
		}
		e.mu.RLock()
		owner := e.processOwners[processRef]
		e.mu.RUnlock()
		if owner != "" {
			return owner, nil
		}
		if mode == "vm" && vmAddr != "" {
			return "vm", nil
		}
		return "local", nil
	default:
		if mode == "vm" && vmAddr != "" {
			return "vm", nil
		}
		return "local", nil
	}
}

func (e *DynamicShellExecutor) reconcileProcessOwner(toolName string, backend string, execCtx tools.ExecutionContext, args map[string]any, result tools.ExecutionResult) {
	if result.Error != nil {
		return
	}

	switch toolName {
	case localshell.ExecCommandAgentSpec.Name:
		processRef, _ := result.ResultJSON["process_ref"].(string)
		processRef = strings.TrimSpace(processRef)
		if processRef == "" {
			return
		}
		running, _ := result.ResultJSON["running"].(bool)
		hasMore, _ := result.ResultJSON["has_more"].(bool)
		if !running && !hasMore {
			e.releaseProcess(processRef)
			return
		}
		e.trackProcess(execCtx.RunID.String(), processRef, backend)
	case localshell.ContinueProcessAgentSpec.Name:
		processRef, _ := args["process_ref"].(string)
		processRef = strings.TrimSpace(processRef)
		if processRef == "" {
			return
		}
		running, _ := result.ResultJSON["running"].(bool)
		hasMore, _ := result.ResultJSON["has_more"].(bool)
		if running || hasMore {
			return
		}
		e.releaseProcess(processRef)
	case localshell.TerminateProcessAgentSpec.Name:
		processRef, _ := args["process_ref"].(string)
		processRef = strings.TrimSpace(processRef)
		if processRef == "" {
			return
		}
		e.releaseProcess(processRef)
	}
}

func (e *DynamicShellExecutor) CleanupRun(ctx context.Context, runID string, terminalStatus string) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil
	}

	e.mu.Lock()
	localRefs := make([]string, 0)
	for processRef, ownedRunID := range e.processRuns {
		if ownedRunID != runID {
			continue
		}
		if e.processOwners[processRef] == "local" {
			localRefs = append(localRefs, processRef)
		}
		delete(e.processOwners, processRef)
		delete(e.processRuns, processRef)
	}
	e.mu.Unlock()

	if len(localRefs) == 0 {
		return nil
	}
	return e.ensureLocal().CleanupProcesses(ctx, localRefs, terminalStatus)
}

func (e *DynamicShellExecutor) trackProcess(runID string, processRef string, backend string) {
	processRef = strings.TrimSpace(processRef)
	if processRef == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.processOwners[processRef] = backend
	if strings.TrimSpace(runID) != "" {
		e.processRuns[processRef] = strings.TrimSpace(runID)
	}
}

func (e *DynamicShellExecutor) releaseProcess(processRef string) {
	processRef = strings.TrimSpace(processRef)
	if processRef == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.processOwners, processRef)
	delete(e.processRuns, processRef)
}
