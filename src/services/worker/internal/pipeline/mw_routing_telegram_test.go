package pipeline_test

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/routing"
)

func TestRoutingUsesInputModelOverride(t *testing.T) {
	cfg := routing.ProviderRoutingConfig{
		
		Credentials: []routing.ProviderCredential{
			{ID: "c-default", OwnerKind: routing.CredentialScopePlatform, ProviderKind: routing.ProviderKindStub, AdvancedJSON: map[string]any{}},
			{ID: "c-model", OwnerKind: routing.CredentialScopePlatform, ProviderKind: routing.ProviderKindStub, AdvancedJSON: map[string]any{}},
		},
		Routes: []routing.ProviderRouteRule{
			{ID: "default", Model: "stub", CredentialID: "c-default", When: map[string]any{}},
			{ID: "tg-model-route", Model: "gpt-5-mini", CredentialID: "c-model", When: map[string]any{}, Priority: 10},
		},
	}
	mw := pipeline.NewRoutingMiddleware(
		routing.NewProviderRouter(cfg),
		nil,
		llm.NewAuxGateway(llm.AuxGatewayConfig{}),
		false,
		data.RunsRepository{},
		data.RunEventsRepository{},
		nil,
		nil,
	)

	personaModel := "old-model"
	rc := &pipeline.RunContext{
		InputJSON: map[string]any{"model": "gpt-5-mini"},
		AgentConfig: &pipeline.ResolvedAgentConfig{
			Model: &personaModel,
		},
	}

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		if rc.SelectedRoute == nil {
			t.Fatal("expected selected route")
		}
		if rc.SelectedRoute.Route.ID != "tg-model-route" {
			t.Fatalf("unexpected route: %s", rc.SelectedRoute.Route.ID)
		}
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
