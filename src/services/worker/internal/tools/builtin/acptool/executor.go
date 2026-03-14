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

// envSkipLLMProxy, when set to "true", forces the executor to skip
// LLM proxy injection so the inner agent uses the user's own API keys.
const envSkipLLMProxy = "ARKLOOP_ACP_SKIP_LLM_PROXY"

var defaultCommand = []string{"opencode", "acp"}

// agentCommands maps agent name to launch command.
var agentCommands = map[string][]string{
	"opencode": {"opencode", "acp"},
}

// sessionRegistry is a package-level singleton shared across all executor calls
// within the same worker process, enabling ACP session reuse across tool invocations.
var sessionRegistry = acp.NewRegistry()

type ToolExecutor struct {
	ConfigResolver sharedconfig.Resolver
	JWTSecret      string
}

func (e ToolExecutor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	toolCallID string,
) tools.ExecutionResult {
	started := time.Now()

	if execCtx.RuntimeSnapshot == nil || execCtx.RuntimeSnapshot.SandboxBaseURL == "" {
		return errResult("tool.sandbox_unavailable", "sandbox not available for acp agent", started)
	}

	task, ok := args["task"].(string)
	if !ok || strings.TrimSpace(task) == "" {
		return errResult("tool.args_invalid", "task parameter is required", started)
	}
	task = strings.TrimSpace(task)

	// resolve agent command
	agentName := "opencode"
	if a, ok := args["agent"].(string); ok && strings.TrimSpace(a) != "" {
		agentName = strings.TrimSpace(a)
	}
	cmd, known := agentCommands[agentName]
	if !known {
		return errResult("tool.args_invalid", fmt.Sprintf("unknown agent: %s", agentName), started)
	}

	cwd := "/workspace"
	cmd = append(append([]string(nil), cmd...), "--cwd", cwd)

	rt := execCtx.RuntimeSnapshot
	accountID := ""
	if execCtx.AccountID != nil {
		accountID = execCtx.AccountID.String()
	}

	env := make(map[string]string)

	// resolve profile and inject LLM proxy configuration (skip in local mode)
	if profileName, ok := args["profile"].(string); ok && strings.TrimSpace(profileName) != "" {
		profileName = strings.TrimSpace(profileName)

		// Local mode: skip LLM proxy if explicitly disabled or if running locally
		skipProxy := os.Getenv(envSkipLLMProxy) == "true"
		if !skipProxy {
			sandboxURL := strings.ToLower(rt.SandboxBaseURL)
			if strings.Contains(sandboxURL, "localhost") || strings.Contains(sandboxURL, "127.0.0.1") {
				skipProxy = true
				slog.Info("acp: local sandbox detected, skipping LLM proxy injection",
					"sandbox_url", rt.SandboxBaseURL,
					"run_id", execCtx.RunID.String(),
				)
			}
		}

		if !skipProxy && e.ConfigResolver != nil {
			mapping, err := sharedconfig.ResolveProfile(ctx, e.ConfigResolver, profileName)
			if err != nil {
				return errResult("tool.profile_invalid", fmt.Sprintf("failed to resolve profile %q: %s", profileName, err), started)
			}

			if e.JWTSecret != "" {
				issuer, err := acptoken.NewIssuer(e.JWTSecret, 30*time.Minute)
				if err != nil {
					return errResult("tool.token_issue_failed", fmt.Sprintf("create token issuer: %s", err), started)
				}

				token, err := issuer.Issue(acptoken.IssueParams{
					RunID:     execCtx.RunID.String(),
					AccountID: accountID,
					Models:    []string{mapping.Model},
					Budget:    0,
				})
				if err != nil {
					return errResult("tool.token_issue_failed", fmt.Sprintf("issue session token: %s", err), started)
				}

				// opencode reads OPENCODE_API_KEY for its "opencode" provider
				// and OPENAI_API_KEY / OPENAI_BASE_URL for OpenAI-based models.
				// Override the provider API URL via config so requests hit our proxy.
				env["OPENCODE_API_KEY"] = token
				env["OPENAI_API_KEY"] = token
				env["OPENAI_BASE_URL"] = defaultLLMProxyBaseURL
				env["OPENCODE_CONFIG_CONTENT"] = fmt.Sprintf(
					`{"provider":{"opencode":{"api":%q}}}`,
					defaultLLMProxyBaseURL,
				)
				env["OPENCODE_DISABLE_AUTOUPDATE"] = "true"
				env["OPENCODE_DISABLE_MODELS_FETCH"] = "true"
			}
		}
	}

	cfg := acp.BridgeConfig{
		SandboxBaseURL:   rt.SandboxBaseURL,
		SandboxAuthToken: rt.SandboxAuthToken,
		SessionID:        execCtx.RunID.String(),
		AccountID:        accountID,
		Command:          cmd,
		Cwd:              cwd,
		Env:              env,
		KillGraceMs:      5000,   // 5 second default grace for ACP tool calls
		CleanupDelayMs:   300000, // 5 min cleanup delay
	}

	client := acp.NewClient(rt.SandboxBaseURL, rt.SandboxAuthToken)
	emitter := execCtx.Emitter
	sandboxSessionID := execCtx.RunID.String()

	// Try to reuse existing session
	if entry := sessionRegistry.Get(sandboxSessionID); entry != nil {
		result, reused := e.tryReuse(ctx, client, cfg, entry, task, emitter, started)
		if reused {
			return result
		}
		// Reuse failed, remove stale entry and fall through to fresh session
		sessionRegistry.Remove(sandboxSessionID)
		slog.Info("acp: session reuse failed, creating fresh session", "session_id", sandboxSessionID)
	}

	// Fresh session
	return e.runFresh(ctx, client, cfg, sandboxSessionID, task, emitter, started)
}

func (e ToolExecutor) tryReuse(
	ctx context.Context,
	client *acp.Client,
	cfg acp.BridgeConfig,
	entry *acp.SessionEntry,
	task string,
	emitter events.Emitter,
	started time.Time,
) (tools.ExecutionResult, bool) {
	bridge := acp.NewBridge(client, cfg)
	bridge.Bind(acp.BridgeState{
		ProcessID:    entry.ProcessID,
		ACPSessionID: entry.ACPSessionID,
		Cursor:       entry.Cursor,
		AgentVersion: entry.AgentVersion,
	})

	// Verify the process is still alive
	if err := bridge.CheckAlive(ctx); err != nil {
		slog.Warn("acp: reuse check failed", "error", err, "session_id", cfg.SessionID)
		return tools.ExecutionResult{}, false
	}

	var collectedEvents []events.RunEvent
	var outputParts []string
	var summary string

	err := bridge.RunPrompt(ctx, task, emitter, func(ev events.RunEvent) error {
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
		slog.Warn("acp: reuse prompt failed", "error", err, "session_id", cfg.SessionID)
		return tools.ExecutionResult{}, false
	}

	// Update registry with new cursor position
	state := bridge.State()
	sessionRegistry.Store(cfg.SessionID, acp.SessionEntry{
		ProcessID:    state.ProcessID,
		ACPSessionID: state.ACPSessionID,
		Cursor:       state.Cursor,
		AgentVersion: state.AgentVersion,
	})

	result := e.buildResult(collectedEvents, outputParts, summary, elapsed)
	return result, true
}

func (e ToolExecutor) runFresh(
	ctx context.Context,
	client *acp.Client,
	cfg acp.BridgeConfig,
	sandboxSessionID string,
	task string,
	emitter events.Emitter,
	started time.Time,
) tools.ExecutionResult {
	bridge := acp.NewBridge(client, cfg)

	var collectedEvents []events.RunEvent
	var outputParts []string
	var summary string

	err := bridge.Run(ctx, task, emitter, func(ev events.RunEvent) error {
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
	if state.ProcessID != "" && state.ACPSessionID != "" {
		sessionRegistry.Store(sandboxSessionID, acp.SessionEntry{
			ProcessID:    state.ProcessID,
			ACPSessionID: state.ACPSessionID,
			Cursor:       state.Cursor,
			AgentVersion: state.AgentVersion,
		})
		slog.Info("acp: session saved for reuse",
			"session_id", sandboxSessionID,
			"process_id", state.ProcessID,
			"acp_session_id", state.ACPSessionID,
		)
	} else {
		bridge.Close()
	}

	return e.buildResult(collectedEvents, outputParts, summary, elapsed)
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
