package speak

import (
	"context"
	"strings"
	"time"

	"arkloop/services/worker/internal/tools"
)

type PipelineBinding interface {
	SetDiscussSpeak(replyToMessageID string)
	IsDiscussRun() bool
}

type executor struct{}

func New() tools.Executor {
	return executor{}
}

func (executor) Execute(
	_ context.Context,
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
	if !ok || binding == nil || !binding.IsDiscussRun() {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: tools.ErrorClassToolExecutionFailed,
				Message:    "speak called outside discuss run",
			},
		}
	}

	replyTo, _ := args["reply_to_message_id"].(string)
	replyTo = strings.TrimSpace(replyTo)
	binding.SetDiscussSpeak(replyTo)

	result := map[string]any{"ok": true}
	if replyTo != "" {
		result["reply_to_message_id"] = replyTo
	}
	return tools.ExecutionResult{
		ResultJSON: result,
		DurationMs: int(time.Since(started).Milliseconds()),
	}
}
