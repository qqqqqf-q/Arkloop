package arkloophelp

import (
	"context"
	"time"

	"arkloop/services/worker/internal/tools"
)

type Executor struct{}

func (e Executor) Execute(
	_ context.Context,
	_ string,
	args map[string]any,
	_ tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	started := time.Now()

	q, _ := args["query"].(string)
	limit := 4
	if v, ok := args["limit"]; ok {
		switch t := v.(type) {
		case float64:
			limit = int(t)
		case int:
			limit = t
		case int64:
			limit = int(t)
		}
	}

	chunks, err := Search(q, limit)
	if err != nil {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: "tool.execution_failed",
				Message:    err.Error(),
			},
			DurationMs: durationMs(started),
		}
	}

	out := make([]map[string]any, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, map[string]any{
			"path":  c.Path,
			"title": c.Title,
			"text":  c.Text,
		})
	}

	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"chunks": out,
		},
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
