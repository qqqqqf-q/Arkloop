package heartbeat_decision

import (
	"context"
	"time"

	"arkloop/services/worker/internal/tools"
)

// PipelineBinding 将 RunContext 的写入操作抽象为接口，避免循环导入。
type PipelineBinding interface {
	SetHeartbeatDecisionOutcome(reply bool, fragments []string)
	IsHeartbeatRun() bool
}

type executor struct{}

// New 返回 heartbeat_decision executor。
func New() tools.Executor {
	return executor{}
}

func (executor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	started := time.Now()
	if toolName != ToolName {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: tools.ErrorClassToolExecutionFailed,
				Message:    "unexpected tool name",
			},
		}
	}

	binding, ok := execCtx.PipelineRC.(PipelineBinding)
	if !ok || binding == nil || !binding.IsHeartbeatRun() {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: tools.ErrorClassToolExecutionFailed,
				Message:    "heartbeat_decision called outside heartbeat run",
			},
		}
	}

	reply, _ := args["reply"].(bool)

	binding.SetHeartbeatDecisionOutcome(reply, nil)

	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"ok":    true,
			"reply": reply,
		},
		DurationMs: int(time.Since(started).Milliseconds()),
	}
}
