package pipeline

import (
	"context"
	"strings"

	"arkloop/services/worker/internal/subagentctl"
	"arkloop/services/worker/internal/tools"
)

func NewSubAgentContextMiddleware(storage *subagentctl.SnapshotStorage) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if storage == nil || rc == nil || rc.Pool == nil || rc.Run.ParentRunID == nil {
			return next(ctx, rc)
		}
		snapshot, err := storage.LoadByCurrentRun(ctx, rc.Pool, rc.Run.ID)
		if err != nil {
			return err
		}
		if snapshot == nil {
			return next(ctx, rc)
		}
		if routeID := strings.TrimSpace(snapshot.Runtime.RouteID); routeID != "" {
			if _, ok := rc.InputJSON["route_id"]; !ok {
				rc.InputJSON["route_id"] = routeID
			}
		}
		if len(snapshot.Runtime.ToolAllowlist) > 0 {
			rc.AllowlistSet = intersectAllowlist(rc.AllowlistSet, snapshot.Runtime.ToolAllowlist, rc.ToolRegistry)
		}
		if len(snapshot.Runtime.ToolDenylist) > 0 {
			for _, denied := range snapshot.Runtime.ToolDenylist {
				RemoveToolOrGroup(rc.AllowlistSet, rc.ToolRegistry, denied)
			}
			rc.ToolDenylist = mergeToolNames(rc.ToolDenylist, snapshot.Runtime.ToolDenylist)
		}
		return next(ctx, rc)
	}
}

func intersectAllowlist(current map[string]struct{}, parent []string, registry *tools.Registry) map[string]struct{} {
	resolved := map[string]struct{}{}
	if len(current) == 0 || len(parent) == 0 {
		return resolved
	}
	parentSet := map[string]struct{}{}
	for _, item := range parent {
		cleaned := strings.TrimSpace(item)
		if cleaned == "" {
			continue
		}
		parentSet[cleaned] = struct{}{}
	}
	for name := range current {
		if ToolAllowed(parentSet, registry, name) {
			resolved[name] = struct{}{}
		}
	}
	return resolved
}

func mergeToolNames(left []string, right []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(left)+len(right))
	for _, group := range [][]string{left, right} {
		for _, item := range group {
			cleaned := strings.TrimSpace(item)
			if cleaned == "" {
				continue
			}
			if _, ok := seen[cleaned]; ok {
				continue
			}
			seen[cleaned] = struct{}{}
			result = append(result, cleaned)
		}
	}
	return result
}
