package pipeline_test

import (
	"context"
	"os"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/routing"
)

type stubGateway struct{}

func (s stubGateway) Stream(_ context.Context, _ llm.Request, _ func(llm.StreamEvent) error) error {
	return nil
}

func buildStubRouterConfig() routing.ProviderRoutingConfig {
	return routing.ProviderRoutingConfig{
		DefaultRouteID: "route-default",
		Credentials: []routing.ProviderCredential{
			{ID: "cred-stub", Name: "stub-cred", OwnerKind: routing.CredentialScopePlatform, ProviderKind: routing.ProviderKindStub},
		},
		Routes: []routing.ProviderRouteRule{
			{ID: "route-default", Model: "stub-model", CredentialID: "cred-stub", Multiplier: 1.0},
		},
	}
}

func TestRoutingMiddleware_StubGatewaySelected(t *testing.T) {
	cfg := buildStubRouterConfig()
	router := routing.NewProviderRouter(cfg)
	stub := stubGateway{}

	mw := pipeline.NewRoutingMiddleware(
		router, nil, stub, false,
		data.RunsRepository{}, data.RunEventsRepository{},
		nil, nil,
	)

	rc := &pipeline.RunContext{
		Emitter:   events.NewEmitter("test"),
		InputJSON: map[string]any{},
	}

	var reached bool
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		reached = true
		if rc.Gateway == nil {
			t.Fatal("Gateway 未设置")
		}
		if rc.SelectedRoute == nil {
			t.Fatal("SelectedRoute 未设置")
		}
		if rc.SelectedRoute.Route.ID != "route-default" {
			t.Fatalf("路由 ID = %q, 期望 route-default", rc.SelectedRoute.Route.ID)
		}
		if rc.ResolveGatewayForRouteID == nil {
			t.Fatal("ResolveGatewayForRouteID 未设置")
		}
		if rc.ResolveGatewayForAgentName == nil {
			t.Fatal("ResolveGatewayForAgentName 未设置")
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reached {
		t.Fatal("终端 handler 未执行")
	}
}

func TestRoutingMiddleware_NilDbPoolUsesStaticRouter(t *testing.T) {
	cfg := buildStubRouterConfig()
	router := routing.NewProviderRouter(cfg)
	stub := stubGateway{}

	mw := pipeline.NewRoutingMiddleware(
		router, nil, stub, false,
		data.RunsRepository{}, data.RunEventsRepository{},
		nil, nil,
	)

	rc := &pipeline.RunContext{
		Emitter:   events.NewEmitter("test"),
		InputJSON: map[string]any{},
	}

	var gatewaySet bool
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		gatewaySet = rc.Gateway != nil
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !gatewaySet {
		t.Fatal("dbPool 为 nil 时应使用 static router 选中路由")
	}
}

func TestRoutingMiddleware_EmptyRouterNoSelectedRoute(t *testing.T) {
	emptyCfg := routing.ProviderRoutingConfig{}
	router := routing.NewProviderRouter(emptyCfg)
	stub := stubGateway{}

	mw := pipeline.NewRoutingMiddleware(
		router, nil, stub, false,
		data.RunsRepository{}, data.RunEventsRepository{},
		nil, nil,
	)

	rc := &pipeline.RunContext{
		Emitter:   events.NewEmitter("test"),
		InputJSON: map[string]any{},
	}

	// 空路由配置 -> selected=nil -> 尝试 appendAndCommitSingle(nil pool) -> panic
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("空路由配置下应 panic（nil pool 调 BeginTx）")
		}
	}()

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error {
		t.Fatal("不应到达终端 handler")
		return nil
	})
	_ = h(context.Background(), rc)
}

func TestRoutingMiddleware_UnknownProviderKind(t *testing.T) {
	cfg := routing.ProviderRoutingConfig{
		DefaultRouteID: "route-unknown",
		Credentials: []routing.ProviderCredential{
			{ID: "cred-x", Name: "unknown-cred", OwnerKind: routing.CredentialScopePlatform, ProviderKind: "unknown_kind"},
		},
		Routes: []routing.ProviderRouteRule{
			{ID: "route-unknown", Model: "x-model", CredentialID: "cred-x", Multiplier: 1.0},
		},
	}
	router := routing.NewProviderRouter(cfg)

	mw := pipeline.NewRoutingMiddleware(
		router, nil, stubGateway{}, false,
		data.RunsRepository{}, data.RunEventsRepository{},
		nil, nil,
	)

	rc := &pipeline.RunContext{
		Emitter:   events.NewEmitter("test"),
		InputJSON: map[string]any{},
	}

	// unknown provider_kind -> gatewayFromCredential 返回 error -> 尝试 appendAndCommitSingle(nil pool) -> panic
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("未知 provider_kind 应 panic（nil pool 调 BeginTx）")
		}
	}()

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error {
		t.Fatal("不应到达终端 handler")
		return nil
	})
	_ = h(context.Background(), rc)
}

func TestRoutingMiddleware_ResolveGatewayForRouteID_EmptyFallbackCurrent(t *testing.T) {
	cfg := buildStubRouterConfig()
	router := routing.NewProviderRouter(cfg)
	stub := stubGateway{}

	mw := pipeline.NewRoutingMiddleware(
		router, nil, stub, false,
		data.RunsRepository{}, data.RunEventsRepository{},
		nil, nil,
	)

	rc := &pipeline.RunContext{
		Emitter:   events.NewEmitter("test"),
		InputJSON: map[string]any{},
	}

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		// 空 route_id 应回退当前路由
		gw, sel, err := rc.ResolveGatewayForRouteID(context.Background(), "")
		if err != nil {
			t.Fatalf("空 route_id 应回退当前路由, 但返回 error: %v", err)
		}
		if gw == nil || sel == nil {
			t.Fatal("空 route_id 应返回当前 gateway 和 route")
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRoutingMiddleware_ResolveGatewayForRouteID_ValidRoute(t *testing.T) {
	cfg := buildStubRouterConfig()
	router := routing.NewProviderRouter(cfg)
	stub := stubGateway{}

	mw := pipeline.NewRoutingMiddleware(
		router, nil, stub, false,
		data.RunsRepository{}, data.RunEventsRepository{},
		nil, nil,
	)

	rc := &pipeline.RunContext{
		Emitter:   events.NewEmitter("test"),
		InputJSON: map[string]any{},
	}

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		gw, sel, err := rc.ResolveGatewayForRouteID(context.Background(), "route-default")
		if err != nil {
			t.Fatalf("合法 route_id 不应返回 error: %v", err)
		}
		if gw == nil {
			t.Fatal("gateway 不应为 nil")
		}
		if sel.Route.ID != "route-default" {
			t.Fatalf("route ID = %q, 期望 route-default", sel.Route.ID)
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRoutingMiddleware_ResolveGatewayForRouteID_NotFound(t *testing.T) {
	cfg := buildStubRouterConfig()
	router := routing.NewProviderRouter(cfg)
	stub := stubGateway{}

	mw := pipeline.NewRoutingMiddleware(
		router, nil, stub, false,
		data.RunsRepository{}, data.RunEventsRepository{},
		nil, nil,
	)

	rc := &pipeline.RunContext{
		Emitter:   events.NewEmitter("test"),
		InputJSON: map[string]any{},
	}

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		_, _, err := rc.ResolveGatewayForRouteID(context.Background(), "nonexistent-route")
		if err == nil {
			t.Fatal("不存在的 route_id 应返回 error")
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRoutingMiddleware_OpenAIGateway_WithEnvApiKey(t *testing.T) {
	const envKey = "ARKLOOP_TEST_OPENAI_KEY"
	os.Setenv(envKey, "sk-test-12345")
	defer os.Unsetenv(envKey)

	cfg := routing.ProviderRoutingConfig{
		DefaultRouteID: "route-openai",
		Credentials: []routing.ProviderCredential{
			{
				ID: "cred-openai", Name: "openai-cred",
				OwnerKind: routing.CredentialScopePlatform, ProviderKind: routing.ProviderKindOpenAI,
				APIKeyEnv: strPtr(envKey),
			},
		},
		Routes: []routing.ProviderRouteRule{
			{ID: "route-openai", Model: "gpt-4", CredentialID: "cred-openai", Multiplier: 1.0},
		},
	}
	router := routing.NewProviderRouter(cfg)

	mw := pipeline.NewRoutingMiddleware(
		router, nil, stubGateway{}, false,
		data.RunsRepository{}, data.RunEventsRepository{},
		nil, nil,
	)

	rc := &pipeline.RunContext{
		Emitter:   events.NewEmitter("test"),
		InputJSON: map[string]any{},
	}

	var gatewayOK bool
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		gatewayOK = rc.Gateway != nil
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !gatewayOK {
		t.Fatal("OpenAI gateway 应被成功创建")
	}
}

func TestRoutingMiddleware_AnthropicGateway_WithDirectApiKey(t *testing.T) {
	cfg := routing.ProviderRoutingConfig{
		DefaultRouteID: "route-anthropic",
		Credentials: []routing.ProviderCredential{
			{
				ID: "cred-anthropic", Name: "anthropic-cred",
				OwnerKind: routing.CredentialScopePlatform, ProviderKind: routing.ProviderKindAnthropic,
				APIKeyValue: strPtr("sk-ant-test-key"),
			},
		},
		Routes: []routing.ProviderRouteRule{
			{ID: "route-anthropic", Model: "claude-3", CredentialID: "cred-anthropic", Multiplier: 1.0},
		},
	}
	router := routing.NewProviderRouter(cfg)

	mw := pipeline.NewRoutingMiddleware(
		router, nil, stubGateway{}, false,
		data.RunsRepository{}, data.RunEventsRepository{},
		nil, nil,
	)

	rc := &pipeline.RunContext{
		Emitter:   events.NewEmitter("test"),
		InputJSON: map[string]any{},
	}

	var gatewayOK bool
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		gatewayOK = rc.Gateway != nil
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !gatewayOK {
		t.Fatal("Anthropic gateway 应被成功创建")
	}
}

func TestRoutingMiddleware_MissingApiKey_Panics(t *testing.T) {
	cfg := routing.ProviderRoutingConfig{
		DefaultRouteID: "route-nokey",
		Credentials: []routing.ProviderCredential{
			{
				ID: "cred-nokey", Name: "nokey-cred",
				OwnerKind: routing.CredentialScopePlatform, ProviderKind: routing.ProviderKindOpenAI,
				APIKeyEnv: strPtr("ARKLOOP_NONEXISTENT_KEY_FOR_TEST"),
			},
		},
		Routes: []routing.ProviderRouteRule{
			{ID: "route-nokey", Model: "gpt-4", CredentialID: "cred-nokey", Multiplier: 1.0},
		},
	}
	router := routing.NewProviderRouter(cfg)

	mw := pipeline.NewRoutingMiddleware(
		router, nil, stubGateway{}, false,
		data.RunsRepository{}, data.RunEventsRepository{},
		nil, nil,
	)

	rc := &pipeline.RunContext{
		Emitter:   events.NewEmitter("test"),
		InputJSON: map[string]any{},
	}

	// API key 缺失 -> gatewayFromCredential error -> appendAndCommitSingle(nil pool) -> panic
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("缺失 API key 时应 panic（nil pool）")
		}
	}()

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, _ *pipeline.RunContext) error {
		t.Fatal("不应到达终端 handler")
		return nil
	})
	_ = h(context.Background(), rc)
}

func TestRoutingMiddleware_ResolveGatewayForAgentName_NilDbPool(t *testing.T) {
	cfg := buildStubRouterConfig()
	router := routing.NewProviderRouter(cfg)

	mw := pipeline.NewRoutingMiddleware(
		router, nil, stubGateway{}, false,
		data.RunsRepository{}, data.RunEventsRepository{},
		nil, nil,
	)

	rc := &pipeline.RunContext{
		Emitter:   events.NewEmitter("test"),
		InputJSON: map[string]any{},
	}

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		_, _, err := rc.ResolveGatewayForAgentName(context.Background(), "some-agent")
		if err == nil {
			t.Fatal("dbPool 为 nil 时 ResolveGatewayForAgentName 应返回 error")
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRoutingMiddleware_ResolveGatewayForAgentName_EmptyFallbackCurrent(t *testing.T) {
	cfg := buildStubRouterConfig()
	router := routing.NewProviderRouter(cfg)

	mw := pipeline.NewRoutingMiddleware(
		router, nil, stubGateway{}, false,
		data.RunsRepository{}, data.RunEventsRepository{},
		nil, nil,
	)

	rc := &pipeline.RunContext{
		Emitter:   events.NewEmitter("test"),
		InputJSON: map[string]any{},
	}

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		gw, sel, err := rc.ResolveGatewayForAgentName(context.Background(), "")
		if err != nil {
			t.Fatalf("空 agentName 应回退当前路由: %v", err)
		}
		if gw == nil || sel == nil {
			t.Fatal("应返回当前 gateway/route")
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
