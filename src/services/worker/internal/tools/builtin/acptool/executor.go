package acptool

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"arkloop/services/shared/acptoken"
	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/worker/internal/acp"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/tools"
)

const defaultLLMProxyBaseURL = "http://api:19001/v1/llm-proxy"

const envSkipLLMProxy = "ARKLOOP_ACP_SKIP_LLM_PROXY"

const envLLMProxyBaseURL = "ARKLOOP_ACP_LLM_PROXY_URL"

// runtimeHandleRegistry caches reusable ACP runtime handles inside this worker process.
// It is run-scoped and memory-only; it is not the source of truth for ACP sessions.
var runtimeHandleRegistry = acp.NewRegistry()

type ToolExecutor struct {
	ConfigResolver  sharedconfig.Resolver
	JWTSecret       string
	LLMProxyBaseURL string
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
		RuntimeSessionKey: runtimeSessionKey,
		AccountID:         accountID,
		Command:           cmd,
		Cwd:               invocation.Cwd,
		Env:               env,
		KillGraceMs:       5000,   // 5 second default grace for ACP tool calls
		CleanupDelayMs:    300000, // 5 min cleanup delay
	}

	host, err := acp.ResolveProcessHost(invocation.Provider, rt)
	if err != nil {
		return errResult("tool.acp_unavailable", err.Error(), started)
	}

	emitter := execCtx.Emitter

	// Try to reuse existing session
	if entry := runtimeHandleRegistry.Get(runtimeSessionKey); entry != nil {
		result, reused := e.tryReuse(ctx, host, cfg, runtimeSessionKey, entry, task, emitter, started)
		if reused {
			return result
		}
		// Reuse failed, remove stale entry and fall through to fresh session
		runtimeHandleRegistry.Remove(runtimeSessionKey)
		slog.Info("acp: session reuse failed, creating fresh session", "runtime_session_key", runtimeSessionKey, "provider", invocation.Provider.ID)
	}

	// Fresh session
	return e.runFresh(ctx, host, cfg, runtimeSessionKey, task, emitter, started)
}

func (e ToolExecutor) tryReuse(
	ctx context.Context,
	host acp.ProcessHost,
	cfg acp.BridgeConfig,
	runtimeSessionKey string,
	entry *acp.RuntimeHandleEntry,
	task string,
	emitter events.Emitter,
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

	err := bridge.EnsureAndRunTurn(ctx, task, emitter, func(ev events.RunEvent) error {
		collectedEvents = append(collectedEvents, ev)
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

	result := e.buildResult(collectedEvents, outputParts, summary, elapsed)
	return result, true
}

func (e ToolExecutor) runFresh(
	ctx context.Context,
	host acp.ProcessHost,
	cfg acp.BridgeConfig,
	runtimeSessionKey string,
	task string,
	emitter events.Emitter,
	started time.Time,
) tools.ExecutionResult {
	bridge := acp.NewBridge(host, cfg)

	var collectedEvents []events.RunEvent
	var outputParts []string
	var summary string

	err := bridge.EnsureAndRunTurn(ctx, task, emitter, func(ev events.RunEvent) error {
		collectedEvents = append(collectedEvents, ev)
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

	return e.buildResult(collectedEvents, outputParts, summary, elapsed)
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
