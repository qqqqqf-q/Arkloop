package pipeline_test

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/routing"
)

func buildRoutingMW(staticRouter *routing.ProviderRouter) pipeline.RunMiddleware {
	return pipeline.NewRoutingMiddleware(
		staticRouter,
		nil,
		llm.NewStubGateway(llm.StubGatewayConfig{}),
		false,
		data.RunsRepository{},
		data.RunEventsRepository{},
		nil,
		nil,
	)
}

// TestRoutingDefaultFallback 验证无 route_id、无凭证名称时，兜底走静态路由默认路由。
func TestRoutingDefaultFallback(t *testing.T) {
	mw := buildRoutingMW(routing.NewProviderRouter(routing.DefaultRoutingConfig()))

	rc := &pipeline.RunContext{
		InputJSON: map[string]any{},
	}
	var selectedRouteID string
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		if rc.SelectedRoute != nil {
			selectedRouteID = rc.SelectedRoute.Route.ID
		}
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selectedRouteID != "default" {
		t.Fatalf("expected default route, got %q", selectedRouteID)
	}
}

// TestRoutingUserRouteIDTakesPriority 验证用户显式 route_id 直接走 Decide()，命中对应路由。
func TestRoutingUserRouteIDTakesPriority(t *testing.T) {
	cfg := routing.ProviderRoutingConfig{
		DefaultRouteID: "default",
		Credentials: []routing.ProviderCredential{
			{ID: "c-default", Scope: routing.CredentialScopePlatform, ProviderKind: routing.ProviderKindStub, AdvancedJSON: map[string]any{}},
			{ID: "c-alt", Scope: routing.CredentialScopePlatform, ProviderKind: routing.ProviderKindStub, AdvancedJSON: map[string]any{}},
		},
		Routes: []routing.ProviderRouteRule{
			{ID: "default", Model: "stub", CredentialID: "c-default", When: map[string]any{}},
			{ID: "alt-route", Model: "stub", CredentialID: "c-alt", When: map[string]any{}},
		},
	}
	mw := buildRoutingMW(routing.NewProviderRouter(cfg))

	rc := &pipeline.RunContext{
		InputJSON: map[string]any{"route_id": "alt-route"},
	}
	var selectedRouteID string
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		if rc.SelectedRoute != nil {
			selectedRouteID = rc.SelectedRoute.Route.ID
		}
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selectedRouteID != "alt-route" {
		t.Fatalf("expected alt-route, got %q", selectedRouteID)
	}
}

// TestRoutingResolverByRouteID 验证 RoutingMiddleware 会注入 ResolveGatewayForRouteID，
// 且可按 route_id 解析出目标路由。
func TestRoutingResolverByRouteID(t *testing.T) {
	cfg := routing.ProviderRoutingConfig{
		DefaultRouteID: "default",
		Credentials: []routing.ProviderCredential{
			{ID: "c-default", Scope: routing.CredentialScopePlatform, ProviderKind: routing.ProviderKindStub, AdvancedJSON: map[string]any{}},
			{ID: "c-final", Scope: routing.CredentialScopePlatform, ProviderKind: routing.ProviderKindStub, AdvancedJSON: map[string]any{}},
		},
		Routes: []routing.ProviderRouteRule{
			{ID: "default", Model: "stub-default", CredentialID: "c-default", When: map[string]any{}},
			{ID: "final-route", Model: "stub-final", CredentialID: "c-final", When: map[string]any{}},
		},
	}
	mw := buildRoutingMW(routing.NewProviderRouter(cfg))

	rc := &pipeline.RunContext{
		InputJSON: map[string]any{},
	}
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(ctx context.Context, rc *pipeline.RunContext) error {
		if rc.ResolveGatewayForRouteID == nil {
			t.Fatal("expected ResolveGatewayForRouteID to be injected")
		}
		gw, selected, err := rc.ResolveGatewayForRouteID(ctx, "final-route")
		if err != nil {
			t.Fatalf("ResolveGatewayForRouteID returned error: %v", err)
		}
		if gw == nil {
			t.Fatal("expected non-nil gateway")
		}
		if selected == nil || selected.Route.ID != "final-route" {
			t.Fatalf("expected final-route selected, got %+v", selected)
		}
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRoutingCredentialNameNoDBFallsToDefault 验证无 DB 路由时，凭证名称匹配不到，兜底走静态路由默认路由。
func TestRoutingCredentialNameNoDBFallsToDefault(t *testing.T) {
	mw := buildRoutingMW(routing.NewProviderRouter(routing.DefaultRoutingConfig()))

	model := "my-anthropic"
	rc := &pipeline.RunContext{
		InputJSON:               map[string]any{},
		PreferredCredentialName: "my-anthropic",
		AgentConfig: &pipeline.ResolvedAgentConfig{
			Model: &model,
		},
	}
	var selectedRouteID string
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		if rc.SelectedRoute != nil {
			selectedRouteID = rc.SelectedRoute.Route.ID
		}
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selectedRouteID != "default" {
		t.Fatalf("expected default fallback, got %q", selectedRouteID)
	}
}
