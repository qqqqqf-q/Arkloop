package pipeline_test

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/personas"
)

// TestPersonaResolutionPreferredCredentialSet 验证 persona 有 preferred_credential 时，设置 rc.PreferredCredentialName。
func TestPersonaResolutionPreferredCredentialSet(t *testing.T) {
	credName := "my-anthropic"
	reg := buildPersonaRegistry(t, "test-persona", &credName)
	mw := pipeline.NewPersonaResolutionMiddleware(
		func() *personas.Registry { return reg },
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	rc := &pipeline.RunContext{
		InputJSON: map[string]any{  "persona_id": "test-persona"},
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

// TestPersonaResolutionNoPreferredCredentialEmpty 验证 persona 无 preferred_credential 时，PreferredCredentialName 为空。
func TestPersonaResolutionNoPreferredCredentialEmpty(t *testing.T) {
	reg := buildPersonaRegistry(t, "test-persona", nil)
	mw := pipeline.NewPersonaResolutionMiddleware(
		func() *personas.Registry { return reg },
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	rc := &pipeline.RunContext{
		InputJSON: map[string]any{  "persona_id": "test-persona"},
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

// TestPersonaResolutionUserRouteIDNotAffectedByPersonaCredential 验证用户显式传 route_id 时不被 persona credential 覆盖。
func TestPersonaResolutionUserRouteIDNotAffectedByPersonaCredential(t *testing.T) {
	personaCred := "my-anthropic"
	userRouteID := "openai-gpt4"
	reg := buildPersonaRegistry(t, "test-persona", &personaCred)
	mw := pipeline.NewPersonaResolutionMiddleware(
		func() *personas.Registry { return reg },
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	rc := &pipeline.RunContext{
		InputJSON: map[string]any{
			  "persona_id": "test-persona",
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

func buildPersonaRegistry(t *testing.T, id string, preferredCredential *string) *personas.Registry {
	t.Helper()
	return buildPersonaRegistryFull(t, id, preferredCredential, nil)
}

func buildPersonaRegistryFull(t *testing.T, id string, preferredCredential *string, agentConfigName *string) *personas.Registry {
	t.Helper()
	reg := personas.NewRegistry()
	def := personas.Definition{
		ID:                  id,
		Version:             "1",
		Title:               "Test Persona",
		PromptMD:            "# test",
		ExecutorType:        "agent.simple",
		ExecutorConfig:      map[string]any{},
		PreferredCredential: preferredCredential,
		AgentConfigName:     agentConfigName,
	}
	if err := reg.Register(def); err != nil {
		t.Fatalf("register persona failed: %v", err)
	}
	return reg
}

// TestPersonaResolutionAgentConfigNameNilPreservesInheritance 验证 persona 无 agent_config_name 时，rc.AgentConfig 保持继承链结果不变。
func TestPersonaResolutionAgentConfigNameNilPreservesInheritance(t *testing.T) {
	reg := buildPersonaRegistryFull(t, "test-persona", nil, nil)
	mw := pipeline.NewPersonaResolutionMiddleware(
		func() *personas.Registry { return reg },
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	existing := &pipeline.ResolvedAgentConfig{Model: strPtr("inherited-model")}
	rc := &pipeline.RunContext{
		InputJSON:   map[string]any{  "persona_id": "test-persona"},
		AgentConfig: existing,
	}

	var capturedConfig *pipeline.ResolvedAgentConfig
	terminal := func(ctx context.Context, rc *pipeline.RunContext) error {
		capturedConfig = rc.AgentConfig
		return nil
	}

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, terminal)
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedConfig != existing {
		t.Fatalf("expected AgentConfig to be unchanged (inherited), got different pointer")
	}
}

func strPtr(s string) *string { return &s }
