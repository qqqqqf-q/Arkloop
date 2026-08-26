package runtime

import (
	"context"
	"strings"
	"sync"

	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/fileops"
	"arkloop/services/worker/internal/tools/localshell"
)

// ShellExecutor 绑定 shell 工具到本机执行器。sandbox 后端已移除，
// 本机是唯一执行路径；保留进程归属跟踪用于 run 结束时清理遗留进程。
type ShellExecutor struct {
	mu          sync.Mutex
	local       *localshell.Executor
	processRuns map[string]string
	fileTracker *fileops.FileTracker
}

func NewShellExecutor(ft *fileops.FileTracker) *ShellExecutor {
	return &ShellExecutor{
		processRuns: map[string]string{},
		fileTracker: ft,
	}
}

func (e *ShellExecutor) ensureLocal() *localshell.Executor {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.local == nil {
		e.local = localshell.NewExecutor()
	}
	return e.local
}

func (e *ShellExecutor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	toolCallID string,
) tools.ExecutionResult {
	result := e.ensureLocal().Execute(ctx, toolName, args, execCtx, toolCallID)

	e.reconcileProcess(toolName, execCtx, args, result)

	// invalidate FileTracker read state for files the command may have modified
	if e.fileTracker != nil && toolName == localshell.ExecCommandAgentSpec.Name {
		command, _ := args["command"].(string)
		cwd := execCtx.WorkDir
		for _, p := range localshell.DetectModifiedFiles(command, cwd) {
			e.fileTracker.InvalidateReadState(execCtx.RunID.String(), p)
		}
	}

	return result
}

func (e *ShellExecutor) reconcileProcess(toolName string, execCtx tools.ExecutionContext, args map[string]any, result tools.ExecutionResult) {
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
		e.trackProcess(execCtx.RunID.String(), processRef)
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

func (e *ShellExecutor) CleanupRun(ctx context.Context, runID string, terminalStatus string) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil
	}

	e.mu.Lock()
	refs := make([]string, 0)
	for processRef, ownedRunID := range e.processRuns {
		if ownedRunID != runID {
			continue
		}
		refs = append(refs, processRef)
		delete(e.processRuns, processRef)
	}
	e.mu.Unlock()

	if len(refs) == 0 {
		return nil
	}
	return e.ensureLocal().CleanupProcesses(ctx, refs, terminalStatus)
}

func (e *ShellExecutor) trackProcess(runID string, processRef string) {
	processRef = strings.TrimSpace(processRef)
	if processRef == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if strings.TrimSpace(runID) != "" {
		e.processRuns[processRef] = strings.TrimSpace(runID)
	}
}

func (e *ShellExecutor) releaseProcess(processRef string) {
	processRef = strings.TrimSpace(processRef)
	if processRef == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.processRuns, processRef)
}
