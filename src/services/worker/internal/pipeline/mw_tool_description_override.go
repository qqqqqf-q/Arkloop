package pipeline

import (
	"context"
	"log/slog"
	"strings"

	sharedtoolmeta "arkloop/services/shared/toolmeta"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"

	"github.com/google/uuid"
)

type ToolDescriptionOverridesReader interface {
	ListByScope(ctx context.Context, projectID *uuid.UUID, scope string) ([]data.ToolDescriptionOverride, error)
}

func NewToolDescriptionOverrideMiddleware(repo ToolDescriptionOverridesReader) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if repo == nil {
			return next(ctx, rc)
		}

		overrides, err := repo.ListByScope(ctx, nil, "platform")
		if err != nil {
			slog.WarnContext(ctx, "tool description override load failed", "run_id", rc.Run.ID, "err", err.Error())
			return next(ctx, rc)
		}

		descriptionByTool := make(map[string]string, len(overrides))
		disabledByTool := make(map[string]struct{}, len(overrides))
		for _, override := range overrides {
			if override.IsDisabled {
				disabledByTool[override.ToolName] = struct{}{}
			}
			if strings.TrimSpace(override.Description) == "" {
				continue
			}
			descriptionByTool[override.ToolName] = override.Description
		}
		for toolName := range disabledByTool {
			RemoveToolOrGroup(rc.AllowlistSet, rc.ToolRegistry, toolName)
		}
		if len(descriptionByTool) == 0 {
			return next(ctx, rc)
		}

		rc.ToolSpecs = applyToolDescriptionOverrides(rc.ToolSpecs, descriptionByTool)
		return next(ctx, rc)
	}
}

func applyToolDescriptionOverrides(specs []llm.ToolSpec, descriptionByTool map[string]string) []llm.ToolSpec {
	if len(specs) == 0 || len(descriptionByTool) == 0 {
		return specs
	}

	out := append([]llm.ToolSpec(nil), specs...)
	for i := range out {
		if _, ok := sharedtoolmeta.Lookup(out[i].Name); !ok {
			continue
		}
		description, ok := descriptionByTool[out[i].Name]
		if !ok {
			continue
		}
		out[i].Description = StringPtr(description)
	}
	return out
}
