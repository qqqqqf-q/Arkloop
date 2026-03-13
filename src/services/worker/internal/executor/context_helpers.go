package executor

import (
	"sort"

	"arkloop/services/worker/internal/pipeline"
)

func sortedToolNames(items map[string]struct{}) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for item := range items {
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func routeIDFromRunContext(rc *pipeline.RunContext) string {
	if rc == nil || rc.SelectedRoute == nil {
		return ""
	}
	return rc.SelectedRoute.Route.ID
}

func modelFromRunContext(rc *pipeline.RunContext) string {
	if rc == nil || rc.SelectedRoute == nil {
		return ""
	}
	return rc.SelectedRoute.Route.Model
}
