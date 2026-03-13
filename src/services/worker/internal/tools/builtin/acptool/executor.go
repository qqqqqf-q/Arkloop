package acptool

import (
	"context"
	"fmt"
	"strings"
	"time"

	"arkloop/services/worker/internal/acp"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/tools"
)

var defaultCommand = []string{"opencode", "acp"}

// agentCommands maps agent name to launch command.
var agentCommands = map[string][]string{
	"opencode": {"opencode", "acp"},
}

type ToolExecutor struct{}

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

	cfg := acp.BridgeConfig{
		SandboxBaseURL:   rt.SandboxBaseURL,
		SandboxAuthToken: rt.SandboxAuthToken,
		SessionID:        execCtx.RunID.String(),
		AccountID:        accountID,
		Command:          cmd,
		Cwd:              cwd,
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
