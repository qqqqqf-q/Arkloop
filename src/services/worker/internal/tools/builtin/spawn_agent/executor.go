package spawnagent

import (
	"context"
	"strings"
	"time"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

const (
	errorArgsInvalid    = "tool.args_invalid"
	errorSpawnFailed    = "tool.spawn_failed"
	errorNotInitialized = "tool.not_initialized"
)

var AgentSpec = tools.AgentToolSpec{
	Name:        "spawn_agent",
	Version:     "1",
	Description: "spawn a sub-agent to execute a task with a specific skill",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true,
}

var LlmSpec = llm.ToolSpec{
	Name:        "spawn_agent",
	Description: stringPtr("spawn a sub-agent to execute a task with a specific skill, returns the sub-agent output"),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"skill_id": map[string]any{
				"type":      "string",
				"minLength": 1,
			},
			"input": map[string]any{
				"type":      "string",
				"minLength": 1,
			},
		},
		"required":             []string{"skill_id", "input"},
		"additionalProperties": false,
	},
}

// SpawnFunc 是 SpawnChildRun 的函数签名，per-run 注入。
type SpawnFunc func(ctx context.Context, skillID string, input string) (string, error)

type ToolExecutor struct {
	SpawnFn SpawnFunc
}

func (e *ToolExecutor) Execute(
	ctx context.Context,
	_ string,
	args map[string]any,
	_ tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	started := time.Now()

	if e.SpawnFn == nil {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorNotInitialized,
				Message:    "spawn_agent not available",
			},
			DurationMs: durationMs(started),
		}
	}

	skillID, input, argErr := parseArgs(args)
	if argErr != nil {
		return tools.ExecutionResult{
			Error:      argErr,
			DurationMs: durationMs(started),
		}
	}

	output, err := e.SpawnFn(ctx, skillID, input)
	if err != nil {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorSpawnFailed,
				Message:    err.Error(),
			},
			DurationMs: durationMs(started),
		}
	}

	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"output": output,
		},
		DurationMs: durationMs(started),
	}
}

func parseArgs(args map[string]any) (string, string, *tools.ExecutionError) {
	for key := range args {
		if key != "skill_id" && key != "input" {
			return "", "", &tools.ExecutionError{
				ErrorClass: errorArgsInvalid,
				Message:    "unknown parameter: " + key,
			}
		}
	}

	skillID, ok := args["skill_id"].(string)
	if !ok || strings.TrimSpace(skillID) == "" {
		return "", "", &tools.ExecutionError{
			ErrorClass: errorArgsInvalid,
			Message:    "skill_id must be a non-empty string",
		}
	}

	input, ok := args["input"].(string)
	if !ok || strings.TrimSpace(input) == "" {
		return "", "", &tools.ExecutionError{
			ErrorClass: errorArgsInvalid,
			Message:    "input must be a non-empty string",
		}
	}

	return strings.TrimSpace(skillID), strings.TrimSpace(input), nil
}

func stringPtr(s string) *string { return &s }

func durationMs(started time.Time) int {
	ms := int(time.Since(started) / time.Millisecond)
	if ms < 0 {
		return 0
	}
	return ms
}
