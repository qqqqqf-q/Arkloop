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

				env["OPENCODE_API_BASE"] = defaultLLMProxyBaseURL
				env["OPENCODE_API_KEY"] = token
				env["OPENCODE_MODEL"] = mapping.Model
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
	}

	client := acp.NewClient(rt.SandboxBaseURL, rt.SandboxAuthToken)
	bridge := acp.NewBridge(client, cfg)

	var collectedEvents []events.RunEvent
	var outputParts []string
	var summary string

	emitter := execCtx.Emitter

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
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: "tool.execution_failed",
				Message:    fmt.Sprintf("acp agent execution failed: %s", err),
			},
			DurationMs: elapsed,
			Events:     collectedEvents,
		}
	}

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
