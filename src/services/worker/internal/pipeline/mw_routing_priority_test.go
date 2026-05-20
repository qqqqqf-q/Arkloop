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
		llm.NewAuxGateway(llm.AuxGatewayConfig{}),
		false,
		data.RunsRepository{},
		data.RunEventsRepository{},
		nil,
		nil,
	)
}

// TestRoutingNoSelectorNoMatch 验证无 route_id、无 model 选择器时，无路由匹配，run 失败。
func TestRoutingNoSelectorNoMatch(t *testing.T) {
	mw := buildRoutingMW(routing.NewProviderRouter(routing.DefaultRoutingConfig()))

	rc := &pipeline.RunContext{
		InputJSON: map[string]any{},
	}
	// 无选择器 + 无 default route -> decision denied -> appendAndCommitSingle(nil pool) -> panic
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("无路由匹配时应 panic（nil pool）")
		}
	}()
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error {
		t.Fatal("不应到达终端 handler")
		return nil
	})
	_ = h(context.Background(), rc)
}

// TestRoutingUserRouteIDTakesPriority 验证用户显式 route_id 直接走 Decide()，命中对应路由。
func TestRoutingUserRouteIDTakesPriority(t *testing.T) {
	cfg := routing.ProviderRoutingConfig{
		
		Credentials: []routing.ProviderCredential{
			{ID: "c-default", OwnerKind: routing.CredentialScopePlatform, ProviderKind: routing.ProviderKindStub, AdvancedJSON: map[string]any{}},
			{ID: "c-alt", OwnerKind: routing.CredentialScopePlatform, ProviderKind: routing.ProviderKindStub, AdvancedJSON: map[string]any{}},
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

		Credentials: []routing.ProviderCredential{
			{ID: "c-default", OwnerKind: routing.CredentialScopePlatform, ProviderKind: routing.ProviderKindStub, AdvancedJSON: map[string]any{}},
			{ID: "c-final", OwnerKind: routing.CredentialScopePlatform, ProviderKind: routing.ProviderKindStub, AdvancedJSON: map[string]any{}},
		},
		Routes: []routing.ProviderRouteRule{
			{ID: "default", Model: "stub-default", CredentialID: "c-default", When: map[string]any{"model": "stub-default"}},
			{ID: "final-route", Model: "stub-final", CredentialID: "c-final", When: map[string]any{}},
		},
	}
	mw := buildRoutingMW(routing.NewProviderRouter(cfg))

	rc := &pipeline.RunContext{
		InputJSON: map[string]any{"model": "stub-default"},
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

// TestRoutingCredentialNameNoDBNoMatch 验证无 DB 路由时，凭证名称匹配不到，无 default fallback，run 失败。
func TestRoutingCredentialNameNoDBNoMatch(t *testing.T) {
	mw := buildRoutingMW(routing.NewProviderRouter(routing.DefaultRoutingConfig()))

	model := "my-anthropic"
	rc := &pipeline.RunContext{
		InputJSON:               map[string]any{},
		PreferredCredentialName: "my-anthropic",
		AgentConfig: &pipeline.ResolvedAgentConfig{
			Model: &model,
		},
	}
	// 无 DB + 无 default route -> no match -> appendAndCommitSingle(nil pool) -> panic
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("凭证名称无 DB 匹配时应 panic（nil pool）")
		}
	}()
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error {
		t.Fatal("不应到达终端 handler")
		return nil
	})
	_ = h(context.Background(), rc)
}
