package pipeline_test

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/skills"
)

// TestSkillResolutionPreferredCredentialSet 验证 skill 有 preferred_credential 时，设置 rc.PreferredCredentialName。
func TestSkillResolutionPreferredCredentialSet(t *testing.T) {
	credName := "my-anthropic"
	mw := pipeline.NewSkillResolutionMiddleware(
		buildSkillRegistry(t, "test-skill", &credName),
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	rc := &pipeline.RunContext{
		InputJSON: map[string]any{"skill_id": "test-skill"},
	}

	var capturedCredName string
	terminal := func(ctx context.Context, rc *pipeline.RunContext) error {
		capturedCredName = rc.PreferredCredentialName
		return nil
	}

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, terminal)
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedCredName != credName {
		t.Fatalf("expected PreferredCredentialName %q, got %q", credName, capturedCredName)
	}
}

// TestSkillResolutionNoPreferredCredentialEmpty 验证 skill 无 preferred_credential 时，PreferredCredentialName 为空。
func TestSkillResolutionNoPreferredCredentialEmpty(t *testing.T) {
	mw := pipeline.NewSkillResolutionMiddleware(
		buildSkillRegistry(t, "test-skill", nil),
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	rc := &pipeline.RunContext{
		InputJSON: map[string]any{"skill_id": "test-skill"},
	}

	var credName string
	terminal := func(ctx context.Context, rc *pipeline.RunContext) error {
		credName = rc.PreferredCredentialName
		return nil
	}

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, terminal)
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if credName != "" {
		t.Fatalf("expected PreferredCredentialName empty, got %q", credName)
	}
}

// TestSkillResolutionUserRouteIDNotAffectedBySkillCredential 验证用户显式传 route_id 时不被 skill credential 覆盖。
func TestSkillResolutionUserRouteIDNotAffectedBySkillCredential(t *testing.T) {
	skillCred := "my-anthropic"
	userRouteID := "openai-gpt4"
	mw := pipeline.NewSkillResolutionMiddleware(
		buildSkillRegistry(t, "test-skill", &skillCred),
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
		t.Fatalf("expected user route_id %q to be preserved, got %v", userRouteID, capturedRouteID)
	}
}

func buildSkillRegistry(t *testing.T, id string, preferredCredential *string) *skills.Registry {
	t.Helper()
	reg := skills.NewRegistry()
	def := skills.Definition{
		ID:                  id,
		Version:             "1",
		Title:               "Test Skill",
		PromptMD:            "# test",
		ExecutorType:        "agent.simple",
		ExecutorConfig:      map[string]any{},
		PreferredCredential: preferredCredential,
	}
	if err := reg.Register(def); err != nil {
		t.Fatalf("register skill failed: %v", err)
	}
	return reg
}
