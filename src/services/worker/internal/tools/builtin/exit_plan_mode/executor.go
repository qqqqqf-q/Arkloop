package exit_plan_mode

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/fileops"
)

type PipelineBinding interface {
	SetIsPlanMode(active bool)
	PlanFilePathValue() string
	IsPlanModeActive() bool
}

type executor struct{}

func New() tools.Executor {
	return executor{}
}

func (executor) Execute(
	ctx context.Context,
	toolName string,
	_ map[string]any,
	execCtx tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	started := time.Now()
	if toolName != ToolName {
		return errResult("unexpected tool name", started)
	}

	binding, ok := execCtx.PipelineRC.(PipelineBinding)
	if !ok || binding == nil {
		return errResult("exit_plan_mode: pipeline binding unavailable", started)
	}

	if !binding.IsPlanModeActive() {
		return errResult("not in plan mode", started)
	}
	if execCtx.ThreadID == nil {
		return errResult("exit_plan_mode: thread_id is required", started)
	}

	planPath := binding.PlanFilePathValue()
	if planPath == "" {
		return errResult("plan file has not been created yet", started)
	}
	backend := fileops.ResolveBackend(execCtx.RuntimeSnapshot, execCtx.WorkDir, execCtx.RunID.String(), resolveAccountID(execCtx), execCtx.ProfileRef, execCtx.WorkspaceRef)
	planBytes, err := backend.ReadFile(ctx, planPath)
	if err != nil {
		return errResult("plan file is required before marking the plan ready", started)
	}
	planText := strings.TrimSpace(string(planBytes))
	if planText == "" {
		return errResult("plan file is empty", started)
	}

	binding.SetIsPlanMode(false)
	event := execCtx.Emitter.Emit("thread.collaboration_mode.updated", map[string]any{
		"thread_id":                   execCtx.ThreadID.String(),
		"run_id":                      execCtx.RunID.String(),
		"previous_collaboration_mode": "plan",
		"collaboration_mode":          "default",
		"plan_file_path":              planPath,
		"source":                      "model",
	}, nil, nil)

	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"status":         "plan_mode_exited",
			"plan_file_path": planPath,
			"filename":       filepath.Base(planPath),
			"plan":           planText,
			"next_action":    "Plan Mode has ended. Continue in this same run by executing the approved plan with normal tools; do not stop after this tool call.",
		},
		DurationMs: int(time.Since(started).Milliseconds()),
		Events:     []events.RunEvent{event},
	}
}

func errResult(message string, started time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error: &tools.ExecutionError{
			ErrorClass: tools.ErrorClassToolExecutionFailed,
			Message:    message,
		},
		DurationMs: int(time.Since(started).Milliseconds()),
	}
}

func resolveAccountID(execCtx tools.ExecutionContext) string {
	if execCtx.AccountID == nil {
		return ""
	}
	return execCtx.AccountID.String()
}
