package pipeline

import (
	"context"
	"strings"

	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/routing"
)

func NewConditionalToolsMiddleware() RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if rc == nil || rc.PersonaDefinition == nil || rc.SelectedRoute == nil {
			return next(ctx, rc)
		}
		rules := rc.PersonaDefinition.ConditionalTools
		if len(rules) == 0 {
			return next(ctx, rc)
		}
		if rc.AllowlistSet == nil {
			rc.AllowlistSet = map[string]struct{}{}
		}

		caps := routing.SelectedRouteModelCapabilities(rc.SelectedRoute)
		for _, rule := range rules {
			if !matchesConditionalToolRule(rule, caps) {
				continue
			}
			for _, name := range rule.Tools {
				cleaned := strings.TrimSpace(name)
				if cleaned == "" {
					continue
				}
				rc.AllowlistSet[cleaned] = struct{}{}
				rc.ToolDenylist = removeToolName(rc.ToolDenylist, cleaned)
			}
		}
		return next(ctx, rc)
	}
}

func matchesConditionalToolRule(rule personas.ConditionalToolRule, caps routing.ModelCapabilities) bool {
	for _, modality := range rule.When.LacksInputModalities {
		if caps.SupportsInputModality(modality) {
			return false
		}
	}
	return true
}

func removeToolName(items []string, target string) []string {
	if len(items) == 0 {
		return nil
	}
	cleanedTarget := strings.TrimSpace(target)
	if cleanedTarget == "" {
		return items
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item) == cleanedTarget {
			continue
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
