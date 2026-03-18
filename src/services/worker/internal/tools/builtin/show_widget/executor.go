package showwidget

import (
	"context"
	"time"

	"arkloop/services/worker/internal/tools"
)

type ToolExecutor struct{}

func NewToolExecutor() *ToolExecutor { return &ToolExecutor{} }

func (e *ToolExecutor) Execute(
	_ context.Context,
	_ string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	started := time.Now()

	seenReadMe, _ := args["i_have_seen_read_me"].(bool)
	if !seenReadMe {
		return errResult("tool.args_invalid", "i_have_seen_read_me must be true after visualize_read_me", started)
	}
	if !execCtx.GenerativeUIReadMeSeen {
		return errResult("tool.execution_failed", "visualize_read_me or artifact_guidelines must be called earlier in this run", started)
	}

	widgetCode, _ := args["widget_code"].(string)
	if widgetCode == "" {
		return errResult("tool.args_invalid", "widget_code is required", started)
	}

	title, _ := args["title"].(string)

	return tools.ExecutionResult{
		ResultJSON: map[string]any{"ok": true, "title": title},
		DurationMs: durationMs(started),
	}
}

func errResult(class, msg string, started time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error:      &tools.ExecutionError{ErrorClass: class, Message: msg},
		DurationMs: durationMs(started),
	}
}

func durationMs(started time.Time) int {
	ms := int(time.Since(started) / time.Millisecond)
	if ms < 0 {
		return 0
	}
	return ms
}
