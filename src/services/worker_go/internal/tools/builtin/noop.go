package builtin

import (
	"context"
	"sort"
	"time"

	"arkloop/services/worker_go/internal/llm"
	"arkloop/services/worker_go/internal/tools"
)

const noopErrorArgsInvalid = "tool.args_invalid"

var NoopAgentSpec = tools.AgentToolSpec{
	Name:        "noop",
	Version:     "1",
	Description: "无副作用的空操作",
	RiskLevel:   tools.RiskLevelLow,
}

var NoopLlmSpec = llm.ToolSpec{
	Name:        "noop",
	Description: stringPtr("无副作用的空操作"),
	JSONSchema: map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	},
}

type NoopExecutor struct{}

func (NoopExecutor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	_ tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	_ = ctx
	_ = toolName
	started := time.Now()

	if len(args) > 0 {
		fields := make([]string, 0, len(args))
		for key := range args {
			fields = append(fields, key)
		}
		sort.Strings(fields)
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: noopErrorArgsInvalid,
				Message:    "noop 不接受参数",
				Details:    map[string]any{"unexpected_fields": fields},
			},
			DurationMs: durationMs(started),
		}
	}

	return tools.ExecutionResult{
		ResultJSON: map[string]any{"ok": true},
		DurationMs: durationMs(started),
	}
}
