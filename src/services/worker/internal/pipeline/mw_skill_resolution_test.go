package pipeline_test

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/skills"
)

// TestSkillResolutionPreferredRouteIDInjected 验证 skill 有 preferred_route_id 且用户未指定 route_id 时，注入到 InputJSON。
func TestSkillResolutionPreferredRouteIDInjected(t *testing.T) {
	routeID := "anthropic-opus"
	mw := pipeline.NewSkillResolutionMiddleware(
		buildSkillRegistry(t, "test-skill", &routeID),
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	rc := &pipeline.RunContext{
		InputJSON: map[string]any{"skill_id": "test-skill"},
	}

	var capturedRouteID any
	terminal := func(ctx context.Context, rc *pipeline.RunContext) error {
		capturedRouteID = rc.InputJSON["route_id"]
		return nil
	}

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, terminal)
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedRouteID != routeID {
		t.Fatalf("expected route_id %q, got %v", routeID, capturedRouteID)
	}
}

// TestSkillResolutionNoPreferredRouteIDNotInjected 验证 skill 无 preferred_route_id 时，InputJSON 中不注入 route_id。
func TestSkillResolutionNoPreferredRouteIDNotInjected(t *testing.T) {
	mw := pipeline.NewSkillResolutionMiddleware(
		buildSkillRegistry(t, "test-skill", nil),
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	rc := &pipeline.RunContext{
		InputJSON: map[string]any{"skill_id": "test-skill"},
	}

	var routeIDPresent bool
	terminal := func(ctx context.Context, rc *pipeline.RunContext) error {
		_, routeIDPresent = rc.InputJSON["route_id"]
		return nil
	}

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, terminal)
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if routeIDPresent {
		t.Fatal("expected route_id not to be injected when skill has no preferred_route_id")
	}
}

// TestSkillResolutionUserRouteIDTakesPriority 验证用户显式传 route_id 时，优先于 skill 的 preferred_route_id。
func TestSkillResolutionUserRouteIDTakesPriority(t *testing.T) {
	skillRouteID := "anthropic-opus"
	userRouteID := "openai-gpt4"
	mw := pipeline.NewSkillResolutionMiddleware(
		buildSkillRegistry(t, "test-skill", &skillRouteID),
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	rc := &pipeline.RunContext{
		InputJSON: map[string]any{
			"skill_id": "test-skill",
			"route_id": userRouteID,
		},
	}

	var capturedRouteID any
	terminal := func(ctx context.Context, rc *pipeline.RunContext) error {
		capturedRouteID = rc.InputJSON["route_id"]
		return nil
	}

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, terminal)
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedRouteID != userRouteID {
		t.Fatalf("expected user route_id %q to take priority, got %v", userRouteID, capturedRouteID)
	}
}

func buildSkillRegistry(t *testing.T, id string, preferredRouteID *string) *skills.Registry {
	t.Helper()
	reg := skills.NewRegistry()
	def := skills.Definition{
		ID:               id,
		Version:          "1",
		Title:            "Test Skill",
		PromptMD:         "# test",
		ExecutorType:     "agent.simple",
		ExecutorConfig:   map[string]any{},
		PreferredRouteID: preferredRouteID,
	}
	if err := reg.Register(def); err != nil {
		t.Fatalf("register skill failed: %v", err)
	}
	return reg
}
