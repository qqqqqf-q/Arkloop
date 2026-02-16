package builtin

import (
	"context"
	"sort"
	"strings"
	"time"

	"arkloop/services/worker_go/internal/llm"
	"arkloop/services/worker_go/internal/tools"
)

const (
	echoErrorArgsInvalid = "tool.args_invalid"
)

var EchoAgentSpec = tools.AgentToolSpec{
	Name:        "echo",
	Version:     "1",
	Description: "回显输入文本",
	RiskLevel:   tools.RiskLevelLow,
}

var EchoLlmSpec = llm.ToolSpec{
	Name:        "echo",
	Description: stringPtr("回显输入文本"),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{"type": "string", "minLength": 1},
		},
		"required":             []string{"text"},
		"additionalProperties": false,
	},
}

type EchoExecutor struct{}

func (EchoExecutor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	_ tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	_ = ctx
	_ = toolName
	started := time.Now()

	unknown := []string{}
	for key := range args {
		if key != "text" {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: echoErrorArgsInvalid,
				Message:    "工具参数不支持额外字段",
				Details:    map[string]any{"unknown_fields": unknown},
			},
			DurationMs: durationMs(started),
		}
	}

	text, ok := args["text"].(string)
	if !ok || strings.TrimSpace(text) == "" {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: echoErrorArgsInvalid,
				Message:    "参数 text 必须为非空字符串",
				Details:    map[string]any{"field": "text"},
			},
			DurationMs: durationMs(started),
		}
	}

	return tools.ExecutionResult{
		ResultJSON: map[string]any{"text": strings.TrimSpace(text)},
		DurationMs: durationMs(started),
	}
}
