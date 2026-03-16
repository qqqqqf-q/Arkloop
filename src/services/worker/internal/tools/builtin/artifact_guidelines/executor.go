package artifactguidelines

import (
	"context"
	"strings"
	"time"

	"arkloop/services/worker/internal/tools"
)

type ToolExecutor struct{}

func (e ToolExecutor) Execute(
	_ context.Context,
	_ string,
	args map[string]any,
	_ tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	started := time.Now()

	rawModules, _ := args["modules"].([]any)
	modules := make([]string, 0, len(rawModules))
	for _, item := range rawModules {
		if s, ok := item.(string); ok {
			modules = append(modules, s)
		}
	}

	content := buildGuidelines(modules)

	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"guidelines": content,
		},
		DurationMs: durationMs(started),
	}
}

func buildGuidelines(modules []string) string {
	var sb strings.Builder
	sb.WriteString(guidelineCore)
	seen := map[string]bool{}
	for _, mod := range modules {
		sections, ok := moduleSections[mod]
		if !ok {
			continue
		}
		for _, section := range sections {
			key := sectionKey(section)
			if seen[key] {
				continue
			}
			seen[key] = true
			sb.WriteString("\n\n")
			sb.WriteString(section)
		}
	}
	return sb.String()
}

func sectionKey(s string) string {
	if len(s) > 60 {
		return s[:60]
	}
	return s
}

func durationMs(started time.Time) int {
	elapsed := time.Since(started)
	millis := int(elapsed / time.Millisecond)
	if millis < 0 {
		return 0
	}
	return millis
}
