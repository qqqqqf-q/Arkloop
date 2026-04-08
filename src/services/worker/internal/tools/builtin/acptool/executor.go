package acptool

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"arkloop/services/shared/acptoken"
	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/worker/internal/acp"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/tools"
)

const defaultLLMProxyBaseURL = "http://api:19001/v1/llm-proxy"

const envSkipLLMProxy = "ARKLOOP_ACP_SKIP_LLM_PROXY"

const envLLMProxyBaseURL = "ARKLOOP_ACP_LLM_PROXY_URL"

var defaultWaitACPTimeout = 30 * time.Second

// runtimeHandleRegistry caches reusable ACP runtime handles inside this worker process.
// It is run-scoped and memory-only; it is not the source of truth for ACP sessions.
var runtimeHandleRegistry = acp.NewRegistry()

var globalACPHandleStore = newACPHandleStore()

type ToolExecutor struct {
	ConfigResolver  sharedconfig.Resolver
	JWTSecret       string
	LLMProxyBaseURL string
	// InjectDesktopDelegateEnv optional (desktop): merge delegate_model_selector + llm_credentials into env.
	InjectDesktopDelegateEnv func(ctx context.Context, execCtx tools.ExecutionContext, invocation acp.ResolvedInvocation, env map[string]string) *tools.ExecutionError
}

func (e ToolExecutor) llmProxyBaseURL() string {
	if e.LLMProxyBaseURL != "" {
		return e.LLMProxyBaseURL
	}
	if v := os.Getenv(envLLMProxyBaseURL); v != "" {
		return v
	}
	return defaultLLMProxyBaseURL
}

func (e ToolExecutor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	toolCallID string,
) tools.ExecutionResult {
	started := time.Now()
	switch toolName {
	case "acp_agent":
		return e.executeACPAgent(ctx, args, execCtx, toolCallID, started)
	case "spawn_acp":
		return e.executeSpawnACP(ctx, args, execCtx, started)
	case "send_acp":
		return e.executeSendACP(args, started)
	case "wait_acp":
		return e.executeWaitACP(ctx, args, execCtx, started)
	case "interrupt_acp":
		return e.executeInterruptACP(args, started)
	case "close_acp":
		return e.executeCloseACP(args, started)
	default:
		return errResult("tool.unknown", "unknown tool: "+toolName, started)
	}
}

func (e ToolExecutor) CleanupRun(_ context.Context, runID string, _ string) error {
	globalACPHandleStore.cleanupRun(runID)
	return nil
}

func CleanupRunFromExecutors(ctx context.Context, executors map[string]tools.Executor, runID string, terminalStatus string) error {
	_ = terminalStatus
	if strings.TrimSpace(runID) == "" {
		return nil
	}
	cleaner, ok := executors[SpawnACPAgentSpec.Name].(interface {
		CleanupRun(context.Context, string, string) error
	})
	if !ok {
		return nil
	}
	return cleaner.CleanupRun(ctx, runID, terminalStatus)
}

func (e ToolExecutor) executeACPAgent(
	ctx context.Context,
	args map[string]any,
	execCtx tools.ExecutionContext,
	toolCallID string,
	started time.Time,
) tools.ExecutionResult {
	rt := execCtx.RuntimeSnapshot

	task, ok := args["task"].(string)
	if !ok || strings.TrimSpace(task) == "" {
		return errResult("tool.args_invalid", "task parameter is required", started)
	}
	task = strings.TrimSpace(task)

	if _, hasLegacyAgent := args["agent"]; hasLegacyAgent {
		return errResult("tool.args_invalid", "agent parameter has been removed, use provider", started)
	}

	providerArg := ""
	if rawProvider, ok := args["provider"].(string); ok {
		providerArg = strings.TrimSpace(rawProvider)
	}

	invocation, err := acp.ResolveProviderInvocation(
		providerArg,
		execCtx.ActiveToolProviderConfigsByGroup,
		rt,
		execCtx.WorkDir,
	)
	if err != nil {
		if strings.Contains(err.Error(), "no ACP host available") {
			return errResult("tool.acp_unavailable", err.Error(), started)
		}
		return errResult("tool.args_invalid", err.Error(), started)
	}

	accountID := ""
	if execCtx.AccountID != nil {
		accountID = execCtx.AccountID.String()
	}

	env := copyStringMap(invocation.Env)
	maybeInjectLocalOpenCodeConfigHome(invocation.Provider, execCtx.ActiveToolProviderConfigsByGroup, execCtx.RunID, env)
	if e.InjectDesktopDelegateEnv != nil {
		if terr := e.InjectDesktopDelegateEnv(ctx, execCtx, invocation, env); terr != nil {
			return errResult(terr.ErrorClass, terr.Message, started)
		}
	}
	profileName := ""
	if rawProfile, ok := args["profile"].(string); ok {
		profileName = strings.TrimSpace(rawProfile)
	}
	if err := e.injectProviderEnv(ctx, execCtx.RunID.String(), accountID, profileName, invocation.Provider, env); err != nil {
		return errResult(err.ErrorClass, err.Message, started)
	}

	cmd := append([]string{invocation.Provider.Command}, invocation.Provider.Args...)
	cmd = append(cmd, "--cwd", invocation.Cwd)

	runtimeSessionKey := buildRuntimeSessionKey(execCtx.RunID.String(), invocation.Provider)
	cfg := acp.BridgeConfig{
		RuntimeSessionKey:         runtimeSessionKey,
		AccountID:                 accountID,
		Command:                   cmd,
		Cwd:                       invocation.Cwd,
		Env:                       env,
		KillGraceMs:               5000,   // 5 second default grace for ACP tool calls
		CleanupDelayMs:            300000, // 5 min cleanup delay
		SessionHandshakeTimeoutMs: sessionHandshakeTimeoutMs(execCtx),
		ProcessStartTimeoutMs:     0,
	}

	host, err := acp.ResolveProcessHost(invocation.Provider, rt)
	if err != nil {
		return errResult("tool.acp_unavailable", err.Error(), started)
	}

	emitter := execCtx.Emitter

	// Try to reuse existing session
	if entry := runtimeHandleRegistry.Get(runtimeSessionKey); entry != nil {
		result, reused := e.tryReuse(ctx, host, cfg, runtimeSessionKey, entry, task, emitter, execCtx, started)
		if reused {
			return result
		}
		// Reuse failed, remove stale entry and fall through to fresh session
		runtimeHandleRegistry.Remove(runtimeSessionKey)
		slog.Info("acp: session reuse failed, creating fresh session", "runtime_session_key", runtimeSessionKey, "provider", invocation.Provider.ID)
	}

	// Fresh session
	return e.runFresh(ctx, host, cfg, runtimeSessionKey, task, emitter, execCtx, started)
}

func (e ToolExecutor) executeSpawnACP(
	ctx context.Context,
	args map[string]any,
	execCtx tools.ExecutionContext,
	started time.Time,
) tools.ExecutionResult {
	rt := execCtx.RuntimeSnapshot

	task, ok := args["task"].(string)
	if !ok || strings.TrimSpace(task) == "" {
		return errResult("tool.args_invalid", "task parameter is required", started)
	}
	task = strings.TrimSpace(task)

	if _, hasLegacyAgent := args["agent"]; hasLegacyAgent {
		return errResult("tool.args_invalid", "agent parameter has been removed, use provider", started)
	}

	providerArg := ""
	if rawProvider, ok := args["provider"].(string); ok {
		providerArg = strings.TrimSpace(rawProvider)
	}

	invocation, err := acp.ResolveProviderInvocation(
		providerArg,
		execCtx.ActiveToolProviderConfigsByGroup,
		rt,
		execCtx.WorkDir,
	)
	if err != nil {
		if strings.Contains(err.Error(), "no ACP host available") {
			return errResult("tool.acp_unavailable", err.Error(), started)
		}
		return errResult("tool.args_invalid", err.Error(), started)
	}

	accountID := ""
	if execCtx.AccountID != nil {
		accountID = execCtx.AccountID.String()
	}

	env := copyStringMap(invocation.Env)
	maybeInjectLocalOpenCodeConfigHome(invocation.Provider, execCtx.ActiveToolProviderConfigsByGroup, execCtx.RunID, env)
	if e.InjectDesktopDelegateEnv != nil {
		if terr := e.InjectDesktopDelegateEnv(ctx, execCtx, invocation, env); terr != nil {
			return errResult(terr.ErrorClass, terr.Message, started)
		}
	}
	profileName := ""
	if rawProfile, ok := args["profile"].(string); ok {
		profileName = strings.TrimSpace(rawProfile)
	}
	if err := e.injectProviderEnv(ctx, execCtx.RunID.String(), accountID, profileName, invocation.Provider, env); err != nil {
		return errResult(err.ErrorClass, err.Message, started)
	}

	cmd := append([]string{invocation.Provider.Command}, invocation.Provider.Args...)
	cmd = append(cmd, "--cwd", invocation.Cwd)

	handleID := uuid.New().String()

	cfg := acp.BridgeConfig{
		RuntimeSessionKey:        "spawn-acp|" + handleID,
		AccountID:                accountID,
		Command:                  cmd,
		Cwd:                      invocation.Cwd,
		Env:                      env,
		KillGraceMs:              5000,
		CleanupDelayMs:           300000,
		StandardCancelCalibrated: true,
	}

	host, err := acp.ResolveProcessHost(invocation.Provider, rt)
	if err != nil {
		return errResult("tool.acp_unavailable", err.Error(), started)
	}

	goroutineCtx, goroutineCancel := context.WithCancel(ctx)
	entry := globalACPHandleStore.create(handleID, execCtx.RunID.String(), goroutineCtx, goroutineCancel)

	go func() {
		bridge := acp.NewBridge(host, cfg)

		turnCtx, turnCancel := context.WithCancel(goroutineCtx)
		entry.mu.Lock()
		entry.turnCancel = turnCancel
		entry.bridge = bridge
		entry.mu.Unlock()

		err := bridge.EnsureAndRunTurn(turnCtx, task, events.Emitter{}, func(ev events.RunEvent) error {
			entry.evMu.Lock()
			entry.cachedEvents = append(entry.cachedEvents, ev)
			entry.evMu.Unlock()
			if ev.Type == "message.delta" {
				if delta, ok := ev.DataJSON["content_delta"].(string); ok {
					entry.mu.Lock()
					entry.output += delta
					entry.mu.Unlock()
				}
			}
			return nil
		})
		turnCancel()

		if goroutineCtx.Err() != nil {
			// close_acp 触发，清理 bridge，进程关闭
			entry.bridgeMu.Lock()
			b := entry.bridge
			entry.bridge = nil
			entry.bridgeMu.Unlock()
			if b != nil {
				b.Close()
			}
			globalACPHandleStore.setClosed(handleID)
			return
		}

		if err != nil {
			if turnCtx.Err() != nil {
				// interrupt_acp 触发的 turn 取消
				// 检查进程是否还活着（StandardCancelCalibrated 路径下进程可能仍存活）
				checkCtx, checkCancel := context.WithTimeout(context.Background(), 3*time.Second)
				aliveErr := bridge.CheckRuntimeAlive(checkCtx)
				checkCancel()
				if aliveErr == nil {
					// session 存活，转为 idle 可继续
					entry.bridgeMu.Lock()
					entry.bridge = bridge
					entry.bridgeMu.Unlock()
					globalACPHandleStore.setIdle(handleID)
				} else {
					// 进程已退，清理
					entry.bridgeMu.Lock()
					entry.bridge = nil
					entry.bridgeMu.Unlock()
					bridge.Close()
					globalACPHandleStore.setInterrupted(handleID)
				}
			} else {
				// 正常执行失败，进程不可复用
				entry.bridgeMu.Lock()
				entry.bridge = nil
				entry.bridgeMu.Unlock()
				bridge.Close()
				globalACPHandleStore.setFailed(handleID, err.Error())
			}
			return
		}

		// turn 成功完成，保活 bridge，转为 idle 等待下一个 send_acp
		entry.bridgeMu.Lock()
		entry.bridge = bridge
		entry.bridgeMu.Unlock()
		globalACPHandleStore.setIdle(handleID)
	}()

	return tools.ExecutionResult{
		ResultJSON: map[string]any{"handle_id": handleID, "status": "running"},
		DurationMs: int(time.Since(started) / time.Millisecond),
	}
}

func (e ToolExecutor) executeSendACP(args map[string]any, started time.Time) tools.ExecutionResult {
	handleID, ok := args["handle_id"].(string)
	if !ok || strings.TrimSpace(handleID) == "" {
		return errResult("tool.args_invalid", "handle_id parameter is required", started)
	}
	input, ok := args["input"].(string)
	if !ok || strings.TrimSpace(input) == "" {
		return errResult("tool.args_invalid", "input parameter is required", started)
	}

	entry := globalACPHandleStore.get(handleID)
	if entry == nil {
		return errResult("tool.acp_handle_not_found", "handle not found: "+handleID, started)
	}

	entry.bridgeMu.Lock()
	bridge := entry.bridge
	entry.bridgeMu.Unlock()
	if bridge == nil {
		return errResult("tool.acp_session_closed", "session is closed, cannot send input", started)
	}

	entry.mu.Lock()
	status := entry.status
	goroutineCtx := entry.goroutineCtx
	entry.mu.Unlock()

	if status != acpStatusIdle {
		return errResult("tool.acp_not_idle", fmt.Sprintf("session status is %s, expected idle", status), started)
	}

	turnCtx, turnCancel := context.WithCancel(goroutineCtx)
	entry.resetTurn(turnCancel)

	go func() {
		err := bridge.EnsureAndRunTurn(turnCtx, input, events.Emitter{}, func(ev events.RunEvent) error {
			entry.evMu.Lock()
			entry.cachedEvents = append(entry.cachedEvents, ev)
			entry.evMu.Unlock()
			if ev.Type == "message.delta" {
				if delta, ok := ev.DataJSON["content_delta"].(string); ok {
					entry.mu.Lock()
					entry.output += delta
					entry.mu.Unlock()
				}
			}
			return nil
		})
		turnCancel()

		if goroutineCtx.Err() != nil {
			// close_acp 触发，关闭进程
			entry.bridgeMu.Lock()
			b := entry.bridge
			entry.bridge = nil
			entry.bridgeMu.Unlock()
			if b != nil {
				b.Close()
			}
			globalACPHandleStore.setClosed(handleID)
			return
		}

		if err != nil {
			if turnCtx.Err() != nil {
				// interrupt_acp 触发
				checkCtx, checkCancel := context.WithTimeout(context.Background(), 3*time.Second)
				aliveErr := bridge.CheckRuntimeAlive(checkCtx)
				checkCancel()
				if aliveErr == nil {
					entry.bridgeMu.Lock()
					entry.bridge = bridge
					entry.bridgeMu.Unlock()
					globalACPHandleStore.setIdle(handleID)
				} else {
					entry.bridgeMu.Lock()
					entry.bridge = nil
					entry.bridgeMu.Unlock()
					bridge.Close()
					globalACPHandleStore.setInterrupted(handleID)
				}
			} else {
				entry.bridgeMu.Lock()
				entry.bridge = nil
				entry.bridgeMu.Unlock()
				bridge.Close()
				globalACPHandleStore.setFailed(handleID, err.Error())
			}
			return
		}

		entry.bridgeMu.Lock()
		entry.bridge = bridge
		entry.bridgeMu.Unlock()
		globalACPHandleStore.setIdle(handleID)
	}()

	return tools.ExecutionResult{
		ResultJSON: map[string]any{"handle_id": handleID, "status": "running"},
		DurationMs: int(time.Since(started) / time.Millisecond),
	}
}

func (e ToolExecutor) executeWaitACP(
	ctx context.Context,
	args map[string]any,
	execCtx tools.ExecutionContext,
	started time.Time,
) tools.ExecutionResult {
	handleID, ok := args["handle_id"].(string)
	if !ok || strings.TrimSpace(handleID) == "" {
		return errResult("tool.args_invalid", "handle_id parameter is required", started)
	}

	var timeoutDuration time.Duration
	timeoutDuration = defaultWaitACPTimeout
	switch v := args["timeout_seconds"].(type) {
	case float64:
		if v >= 1 {
			timeoutDuration = time.Duration(v) * time.Second
		}
	case int:
		if v >= 1 {
			timeoutDuration = time.Duration(v) * time.Second
		}
	}

	entry := globalACPHandleStore.get(handleID)
	if entry == nil {
		return errResult("tool.acp_handle_not_found", "handle not found: "+handleID, started)
	}

	deadlineCh := time.After(timeoutDuration)

	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()

	drainCachedEvents := func() []events.RunEvent {
		entry.evMu.Lock()
		defer entry.evMu.Unlock()
		if len(entry.cachedEvents) == 0 {
			return nil
		}
		drained := make([]events.RunEvent, len(entry.cachedEvents))
		copy(drained, entry.cachedEvents)
		entry.cachedEvents = nil
		return drained
	}

	emitCachedEvents := func() error {
		for _, ev := range drainCachedEvents() {
			if execCtx.StreamEvent != nil {
				if err := execCtx.StreamEvent(ev); err != nil {
					return err
				}
				continue
			}
			execCtx.Emitter.Emit(ev.Type, ev.DataJSON, ev.ToolName, ev.ErrorClass)
		}
		return nil
	}

	for {
		if err := emitCachedEvents(); err != nil {
			return errResult("tool.execution_failed", err.Error(), started)
		}
		entry.mu.Lock()
		status := entry.status
		output := entry.output
		errMsg := entry.errMsg
		entry.mu.Unlock()

		switch status {
		case acpStatusIdle:
			if err := emitCachedEvents(); err != nil {
				return errResult("tool.execution_failed", err.Error(), started)
			}
			return tools.ExecutionResult{
				ResultJSON: map[string]any{"handle_id": handleID, "status": "completed", "output": output},
				DurationMs: int(time.Since(started) / time.Millisecond),
			}
		case acpStatusCompleted:
			if err := emitCachedEvents(); err != nil {
				return errResult("tool.execution_failed", err.Error(), started)
			}
			return tools.ExecutionResult{
				ResultJSON: map[string]any{"handle_id": handleID, "status": "completed", "output": output},
				DurationMs: int(time.Since(started) / time.Millisecond),
			}
		case acpStatusFailed:
			if err := emitCachedEvents(); err != nil {
				return errResult("tool.execution_failed", err.Error(), started)
			}
			return tools.ExecutionResult{
				ResultJSON: map[string]any{"handle_id": handleID, "status": "failed", "error": errMsg},
				DurationMs: int(time.Since(started) / time.Millisecond),
			}
		case acpStatusInterrupted:
			if err := emitCachedEvents(); err != nil {
				return errResult("tool.execution_failed", err.Error(), started)
			}
			return tools.ExecutionResult{
				ResultJSON: map[string]any{"handle_id": handleID, "status": "interrupted"},
				DurationMs: int(time.Since(started) / time.Millisecond),
			}
		case acpStatusClosed:
			emitCachedEvents()
			return tools.ExecutionResult{
				ResultJSON: map[string]any{"handle_id": handleID, "status": "closed"},
				DurationMs: int(time.Since(started) / time.Millisecond),
			}
		}

		select {
		case <-entry.doneCh:
			// next loop will read terminal status
		case <-ticker.C:
		case <-ctx.Done():
			return errResult("tool.cancelled", "wait_acp cancelled", started)
		case <-deadlineCh:
			emitCachedEvents()
			return tools.ExecutionResult{
				ResultJSON: map[string]any{"handle_id": handleID, "status": "running", "timeout": true},
				DurationMs: int(time.Since(started) / time.Millisecond),
			}
		}
	}
}

func (e ToolExecutor) executeInterruptACP(args map[string]any, started time.Time) tools.ExecutionResult {
	handleID, ok := args["handle_id"].(string)
	if !ok || strings.TrimSpace(handleID) == "" {
		return errResult("tool.args_invalid", "handle_id parameter is required", started)
	}

	entry := globalACPHandleStore.get(handleID)
	if entry == nil {
		return errResult("tool.acp_handle_not_found", "handle not found: "+handleID, started)
	}

	entry.mu.Lock()
	status := entry.status
	turnCancel := entry.turnCancel
	entry.mu.Unlock()

	if status != acpStatusRunning {
		return tools.ExecutionResult{
			ResultJSON: map[string]any{
				"handle_id": handleID,
				"status":    string(status),
				"note":      "not in running state",
			},
			DurationMs: int(time.Since(started) / time.Millisecond),
		}
	}

	if turnCancel != nil {
		turnCancel()
	}

	return tools.ExecutionResult{
		ResultJSON: map[string]any{"handle_id": handleID, "status": "interrupting"},
		DurationMs: int(time.Since(started) / time.Millisecond),
	}
}

func (e ToolExecutor) executeCloseACP(args map[string]any, started time.Time) tools.ExecutionResult {
	handleID, ok := args["handle_id"].(string)
	if !ok || strings.TrimSpace(handleID) == "" {
		return errResult("tool.args_invalid", "handle_id parameter is required", started)
	}

	entry := globalACPHandleStore.get(handleID)
	if entry == nil {
		return errResult("tool.acp_handle_not_found", "handle not found: "+handleID, started)
	}

	entry.mu.Lock()
	status := entry.status
	goroutineCancel := entry.goroutineCancel
	entry.mu.Unlock()

	if status == acpStatusRunning {
		return errResult("tool.acp_close_while_running",
			"close not allowed while a turn is active, call interrupt_acp first", started)
	}

	// 触发进程级 cancel
	goroutineCancel()

	// 若进程是 idle 状态（无 goroutine 在跑），直接关 bridge
	entry.bridgeMu.Lock()
	bridge := entry.bridge
	entry.bridge = nil
	entry.bridgeMu.Unlock()
	if bridge != nil {
		bridge.Close()
	}

	globalACPHandleStore.setClosed(handleID)

	return tools.ExecutionResult{
		ResultJSON: map[string]any{"handle_id": handleID, "status": "closed"},
		DurationMs: int(time.Since(started) / time.Millisecond),
	}
}

func (e ToolExecutor) tryReuse(
	ctx context.Context,
	host acp.ProcessHost,
	cfg acp.BridgeConfig,
	runtimeSessionKey string,
	entry *acp.RuntimeHandleEntry,
	task string,
	emitter events.Emitter,
	execCtx tools.ExecutionContext,
	started time.Time,
) (tools.ExecutionResult, bool) {
	bridge := acp.NewBridge(host, cfg)
	bridge.Bind(acp.BridgeState{
		HostProcessID:     entry.HostProcessID,
		ProtocolSessionID: entry.ProtocolSessionID,
		OutputCursor:      entry.OutputCursor,
		AgentVersion:      entry.AgentVersion,
	})

	// Verify the process is still alive
	if err := bridge.CheckRuntimeAlive(ctx); err != nil {
		slog.Warn("acp: reuse check failed", "error", err, "runtime_session_key", cfg.RuntimeSessionKey)
		return tools.ExecutionResult{}, false
	}

	var collectedEvents []events.RunEvent
	var outputParts []string
	var summary string

	streamed := false
	err := bridge.EnsureAndRunTurn(ctx, task, emitter, func(ev events.RunEvent) error {
		collectedEvents = append(collectedEvents, ev)
		if execCtx.StreamEvent != nil {
			if err := execCtx.StreamEvent(ev); err != nil {
				return err
			}
			streamed = true
		}
		switch ev.Type {
		case "message.delta":
			if delta, ok := ev.DataJSON["content_delta"].(string); ok {
				outputParts = append(outputParts, delta)
			}
		case "run.completed":
			if s, ok := ev.DataJSON["summary"].(string); ok {
				summary = s
			}
		}
		return nil
	})

	elapsed := int(time.Since(started) / time.Millisecond)

	if err != nil {
		slog.Warn("acp: reuse prompt failed", "error", err, "runtime_session_key", cfg.RuntimeSessionKey)
		return tools.ExecutionResult{}, false
	}

	// Update registry with new cursor position
	state := bridge.State()
	runtimeHandleRegistry.Store(runtimeSessionKey, acp.RuntimeHandleEntry{
		HostProcessID:     state.HostProcessID,
		ProtocolSessionID: state.ProtocolSessionID,
		OutputCursor:      state.OutputCursor,
		AgentVersion:      state.AgentVersion,
	})

	result := e.buildResult(collectedEvents, outputParts, summary, streamed, elapsed)
	return result, true
}

func (e ToolExecutor) runFresh(
	ctx context.Context,
	host acp.ProcessHost,
	cfg acp.BridgeConfig,
	runtimeSessionKey string,
	task string,
	emitter events.Emitter,
	execCtx tools.ExecutionContext,
	started time.Time,
) tools.ExecutionResult {
	bridge := acp.NewBridge(host, cfg)

	var collectedEvents []events.RunEvent
	var outputParts []string
	var summary string

	streamed := false
	err := bridge.EnsureAndRunTurn(ctx, task, emitter, func(ev events.RunEvent) error {
		collectedEvents = append(collectedEvents, ev)
		if execCtx.StreamEvent != nil {
			if err := execCtx.StreamEvent(ev); err != nil {
				return err
			}
			streamed = true
		}
		switch ev.Type {
		case "message.delta":
			if delta, ok := ev.DataJSON["content_delta"].(string); ok {
				outputParts = append(outputParts, delta)
			}
		case "run.completed":
			if s, ok := ev.DataJSON["summary"].(string); ok {
				summary = s
			}
		}
		return nil
	})

	elapsed := int(time.Since(started) / time.Millisecond)

	if err != nil {
		bridge.Close()
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: "tool.execution_failed",
				Message:    fmt.Sprintf("acp agent execution failed: %s", err),
			},
			DurationMs: elapsed,
			Events:     collectedEvents,
			Streamed:   streamed,
		}
	}

	// Save session for reuse (process stays alive)
	state := bridge.State()
	if state.HostProcessID != "" && state.ProtocolSessionID != "" {
		runtimeHandleRegistry.Store(runtimeSessionKey, acp.RuntimeHandleEntry{
			HostProcessID:     state.HostProcessID,
			ProtocolSessionID: state.ProtocolSessionID,
			OutputCursor:      state.OutputCursor,
			AgentVersion:      state.AgentVersion,
		})
		slog.Info("acp: session saved for reuse",
			"runtime_session_key", runtimeSessionKey,
			"process_id", state.HostProcessID,
			"protocol_session_id", state.ProtocolSessionID,
		)
	} else {
		bridge.Close()
	}

	return e.buildResult(collectedEvents, outputParts, summary, streamed, elapsed)
}

func (e ToolExecutor) injectProviderEnv(
	ctx context.Context,
	runID string,
	accountID string,
	profileName string,
	provider acp.ResolvedProvider,
	env map[string]string,
) *tools.ExecutionError {
	if provider.AuthStrategy == acp.AuthStrategyProviderNative || profileName == "" {
		return nil
	}

	if os.Getenv(envSkipLLMProxy) == "true" {
		return nil
	}
	if e.ConfigResolver == nil {
		return nil
	}

	mapping, err := sharedconfig.ResolveProfile(ctx, e.ConfigResolver, profileName)
	if err != nil {
		return &tools.ExecutionError{
			ErrorClass: "tool.profile_invalid",
			Message:    fmt.Sprintf("failed to resolve profile %q: %s", profileName, err),
		}
	}
	if e.JWTSecret == "" {
		return nil
	}

	issuer, err := acptoken.NewIssuer(e.JWTSecret, 30*time.Minute)
	if err != nil {
		return &tools.ExecutionError{
			ErrorClass: "tool.token_issue_failed",
			Message:    fmt.Sprintf("create token issuer: %s", err),
		}
	}

	token, err := issuer.Issue(acptoken.IssueParams{
		RunID:     runID,
		AccountID: accountID,
		Models:    []string{mapping.Model},
		Budget:    0,
	})
	if err != nil {
		return &tools.ExecutionError{
			ErrorClass: "tool.token_issue_failed",
			Message:    fmt.Sprintf("issue session token: %s", err),
		}
	}

	env["OPENCODE_API_KEY"] = token
	env["OPENAI_API_KEY"] = token
	proxyURL := e.llmProxyBaseURL()
	env["OPENAI_BASE_URL"] = proxyURL
	env["OPENCODE_CONFIG_CONTENT"] = fmt.Sprintf(`{"provider":{"opencode":{"api":%q}}}`, proxyURL)
	env["OPENCODE_DISABLE_AUTOUPDATE"] = "true"
	env["OPENCODE_DISABLE_MODELS_FETCH"] = "true"
	env["ARKLOOP_ACP_HOST_KIND"] = string(provider.HostKind)
	return nil
}

func buildRuntimeSessionKey(runID string, provider acp.ResolvedProvider) string {
	return strings.Join([]string{runID, provider.ID, string(provider.HostKind)}, "|")
}

func copyStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func (e ToolExecutor) buildResult(
	collectedEvents []events.RunEvent,
	outputParts []string,
	summary string,
	streamed bool,
	elapsed int,
) tools.ExecutionResult {
	if len(collectedEvents) > 0 {
		last := collectedEvents[len(collectedEvents)-1]
		if last.Type == "run.failed" {
			msg := "acp agent reported failure"
			if m, ok := last.DataJSON["message"].(string); ok {
				msg = m
			}
			errClass := "tool.acp_agent_failed"
			if ec, ok := last.DataJSON["error_class"].(string); ok {
				errClass = ec
			}
			return tools.ExecutionResult{
				Error: &tools.ExecutionError{
					ErrorClass: errClass,
					Message:    msg,
				},
				DurationMs: elapsed,
				Events:     collectedEvents,
				Streamed:   streamed,
			}
		}
	}

	output := strings.Join(outputParts, "")
	result := map[string]any{
		"status": "completed",
		"output": output,
	}
	if summary != "" {
		result["summary"] = summary
	}

	return tools.ExecutionResult{
		ResultJSON: result,
		DurationMs: elapsed,
		Events:     collectedEvents,
		Streamed:   streamed,
	}
}

func errResult(errorClass, message string, started time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error: &tools.ExecutionError{
			ErrorClass: errorClass,
			Message:    message,
		},
		DurationMs: int(time.Since(started) / time.Millisecond),
	}
}

func sessionHandshakeTimeoutMs(execCtx tools.ExecutionContext) int {
	if execCtx.TimeoutMs == nil || *execCtx.TimeoutMs <= 0 {
		return 0
	}
	v := *execCtx.TimeoutMs
	const minMs = 30000
	const maxMs = 300000
	if v < minMs {
		v = minMs
	}
	if v > maxMs {
		v = maxMs
	}
	return v
}
