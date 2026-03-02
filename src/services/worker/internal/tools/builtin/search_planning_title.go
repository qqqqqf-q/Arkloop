package builtin

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

const (
	searchPlanningTitleErrorArgsInvalid = "tool.args_invalid"
	searchPlanningTitleMaxRunes         = 60
)

var SearchPlanningTitleAgentSpec = tools.AgentToolSpec{
	Name:        "search_planning_title",
	Version:     "1",
	Description: "set search timeline planning title (UI only)",
	RiskLevel:   tools.RiskLevelLow,
}

var SearchPlanningTitleLlmSpec = llm.ToolSpec{
	Name:        "search_planning_title",
	Description: stringPtr("set search timeline planning title (UI only)"),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"label": map[string]any{
				"type":        "string",
				"description": "a short single-line title for the search timeline planning step",
			},
		},
		"required":             []string{"label"},
		"additionalProperties": false,
	},
}

type SearchPlanningTitleExecutor struct{}

func (SearchPlanningTitleExecutor) Execute(
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
		if key != "label" {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: searchPlanningTitleErrorArgsInvalid,
				Message:    "tool args do not accept extra fields",
				Details:    map[string]any{"unknown_fields": unknown},
			},
			DurationMs: durationMs(started),
		}
	}

	rawLabel, _ := args["label"].(string)
	label := strings.TrimSpace(rawLabel)
	label = strings.Join(strings.Fields(label), " ")
	if label == "" {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: searchPlanningTitleErrorArgsInvalid,
				Message:    "parameter label must be a non-empty string",
				Details:    map[string]any{"field": "label"},
			},
			DurationMs: durationMs(started),
		}
	}

	label = truncateRunes(label, searchPlanningTitleMaxRunes)

	return tools.ExecutionResult{
		ResultJSON: map[string]any{"label": label},
		DurationMs: durationMs(started),
	}
}

func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	if maxRunes <= 3 {
		return string([]rune(s)[:maxRunes])
	}
	runes := []rune(s)
	return string(runes[:maxRunes-3]) + "..."
}

