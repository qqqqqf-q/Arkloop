package pipeline_test

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/routing"
)

func TestConditionalToolsMiddlewareAddsToolWhenRouteLacksImage(t *testing.T) {
	rc := &pipeline.RunContext{
		PersonaDefinition: &personas.Definition{
			ConditionalTools: []personas.ConditionalToolRule{
				{
					When: personas.ConditionalToolWhen{
						LacksInputModalities: []string{"image"},
					},
					Tools: []string{"read"},
				},
			},
		},
		AllowlistSet: map[string]struct{}{},
		ToolDenylist: []string{"read"},
		SelectedRoute: &routing.SelectedProviderRoute{
			Route: routing.ProviderRouteRule{
				AdvancedJSON: map[string]any{
					"available_catalog": map[string]any{
						"input_modalities": []string{"text"},
					},
				},
			},
		},
	}

	mw := pipeline.NewConditionalToolsMiddleware()
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		if _, ok := rc.AllowlistSet["read"]; !ok {
			t.Fatal("expected read added to allowlist")
		}
		if len(rc.ToolDenylist) != 0 {
			t.Fatalf("expected denylist cleared, got %#v", rc.ToolDenylist)
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConditionalToolsMiddlewareSkipsToolWhenRouteSupportsImage(t *testing.T) {
	rc := &pipeline.RunContext{
		PersonaDefinition: &personas.Definition{
			ConditionalTools: []personas.ConditionalToolRule{
				{
					When: personas.ConditionalToolWhen{
						LacksInputModalities: []string{"image"},
					},
					Tools: []string{"read"},
				},
			},
		},
		AllowlistSet: map[string]struct{}{},
		ToolDenylist: []string{"read"},
		SelectedRoute: &routing.SelectedProviderRoute{
			Route: routing.ProviderRouteRule{
				AdvancedJSON: map[string]any{
					"available_catalog": map[string]any{
						"input_modalities": []string{"text", "image"},
					},
				},
			},
		},
	}

	mw := pipeline.NewConditionalToolsMiddleware()
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		if _, ok := rc.AllowlistSet["read"]; ok {
			t.Fatal("did not expect read added to allowlist")
		}
		if len(rc.ToolDenylist) != 1 || rc.ToolDenylist[0] != "read" {
			t.Fatalf("unexpected denylist: %#v", rc.ToolDenylist)
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
