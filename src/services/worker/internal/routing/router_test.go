package routing

import "testing"

func TestProviderRouterDecide_NoMatchReturnsEmpty(t *testing.T) {
	cfg := DefaultRoutingConfig()
	router := NewProviderRouter(cfg)

	decision := router.Decide(map[string]any{}, false, false)
	if decision.Denied != nil {
		t.Fatalf("expected empty, got denied: %+v", decision.Denied)
	}
	if decision.Selected != nil {
		t.Fatalf("expected no selection without default route, got %s", decision.Selected.Route.ID)
	}
}

func TestProviderRouterDecide_RequestedRoute(t *testing.T) {
	cfg := ProviderRoutingConfig{
		
		Credentials: []ProviderCredential{
			{
				ID:           "stub_default",
				OwnerKind:        CredentialScopePlatform,
				ProviderKind: ProviderKindStub,
				AdvancedJSON: map[string]any{},
			},
			{
				ID:           "stub_alt",
				OwnerKind:        CredentialScopePlatform,
				ProviderKind: ProviderKindStub,
				AdvancedJSON: map[string]any{},
			},
		},
		Routes: []ProviderRouteRule{
			{ID: "default", Model: "stub", CredentialID: "stub_default", When: map[string]any{}},
			{ID: "alt", Model: "stub", CredentialID: "stub_alt", When: map[string]any{}},
		},
	}
	router := NewProviderRouter(cfg)

	decision := router.Decide(map[string]any{"route_id": "alt"}, false, false)
	if decision.Denied != nil {
		t.Fatalf("expected selected, got denied: %+v", decision.Denied)
	}
	if decision.Selected == nil {
		t.Fatalf("expected selected")
	}
	if decision.Selected.Route.ID != "alt" {
		t.Fatalf("unexpected route id: %s", decision.Selected.Route.ID)
	}
}

func TestProviderRouterDecide_RouteNotFound(t *testing.T) {
	cfg := DefaultRoutingConfig()
	router := NewProviderRouter(cfg)

	decision := router.Decide(map[string]any{"route_id": "missing"}, false, false)
	if decision.Selected != nil {
		t.Fatalf("expected denied")
	}
	if decision.Denied == nil {
		t.Fatalf("expected denied")
	}
	if decision.Denied.Code != "policy.route_not_found" {
		t.Fatalf("unexpected code: %s", decision.Denied.Code)
	}
}

func TestProviderRouterDecide_ByokDisabled(t *testing.T) {
	cfg := ProviderRoutingConfig{
		
		Credentials: []ProviderCredential{
			{
				ID:           "org_cred",
				OwnerKind:        CredentialScopeUser,
				ProviderKind: ProviderKindOpenAI,
				APIKeyEnv:    stringPtr("ARKLOOP_OPENAI_API_KEY"),
				OpenAIMode:   stringPtr("chat_completions"),
				AdvancedJSON: map[string]any{},
			},
		},
		Routes: []ProviderRouteRule{
			{ID: "default", Model: "gpt", CredentialID: "org_cred", When: map[string]any{"persona_id": "test"}},
		},
	}
	router := NewProviderRouter(cfg)

	decision := router.Decide(map[string]any{"persona_id": "test"}, false, false)
	if decision.Selected != nil {
		t.Fatalf("expected denied")
	}
	if decision.Denied == nil {
		t.Fatalf("expected denied")
	}
	if decision.Denied.Code != "policy.byok_disabled" {
		t.Fatalf("unexpected code: %s", decision.Denied.Code)
	}
}

func TestProviderRouteRuleMatches_WhenContainsObjectDoesNotPanic(t *testing.T) {
	rule := ProviderRouteRule{
		ID:   "r1",
		When: map[string]any{"meta": map[string]any{"a": "b"}},
	}

	input := map[string]any{"meta": map[string]any{"a": "b"}}
	if !rule.Matches(input) {
		t.Fatalf("expected match")
	}
}

func TestProviderRouteRuleMatches_WhenContainsArrayDoesNotPanic(t *testing.T) {
	rule := ProviderRouteRule{
		ID:   "r1",
		When: map[string]any{"tags": []any{"a", "b"}},
	}

	input := map[string]any{"tags": []any{"a", "b"}}
	if !rule.Matches(input) {
		t.Fatalf("expected match")
	}
	if rule.Matches(map[string]any{"tags": []any{"a"}}) {
		t.Fatalf("expected mismatch")
	}
}

func TestProviderRouterDecide_PlatformOnlySkipsAccountScoped(t *testing.T) {
	cfg := ProviderRoutingConfig{
		
		Credentials: []ProviderCredential{
			{ID: "cred-acct", OwnerKind: CredentialScopeUser, ProviderKind: ProviderKindStub, AdvancedJSON: map[string]any{}},
			{ID: "cred-plat", OwnerKind: CredentialScopePlatform, ProviderKind: ProviderKindStub, AdvancedJSON: map[string]any{}},
		},
		Routes: []ProviderRouteRule{
			{ID: "acct-default", Model: "gpt-acct", CredentialID: "cred-acct", AccountScoped: true, When: map[string]any{}},
			{ID: "acct-match", Model: "gpt-acct-match", CredentialID: "cred-acct", AccountScoped: true, When: map[string]any{"tier": "pro"}},
			{ID: "plat-match", Model: "gpt-plat", CredentialID: "cred-plat", AccountScoped: false, When: map[string]any{"tier": "pro"}},
		},
	}
	router := NewProviderRouter(cfg)

	// platformOnly=true: 跳过 account-scoped 默认路由和匹配路由，选中 platform 路由
	dec := router.Decide(map[string]any{"tier": "pro"}, true, true)
	if dec.Denied != nil {
		t.Fatalf("expected selected, got denied: %+v", dec.Denied)
	}
	if dec.Selected == nil {
		t.Fatal("expected selected route")
	}
	if dec.Selected.Route.ID != "plat-match" {
		t.Fatalf("expected plat-match, got %s", dec.Selected.Route.ID)
	}

	// platformOnly=false: account-scoped 匹配路由正常参与
	dec2 := router.Decide(map[string]any{"tier": "pro"}, true, false)
	if dec2.Selected == nil {
		t.Fatal("expected selected route")
	}
	if dec2.Selected.Route.ID != "acct-match" {
		t.Fatalf("expected acct-match, got %s", dec2.Selected.Route.ID)
	}
}

func TestProviderRouterDecide_PlatformOnlyFallbackNoRoutes(t *testing.T) {
	cfg := ProviderRoutingConfig{
		
		Credentials: []ProviderCredential{
			{ID: "cred-acct", OwnerKind: CredentialScopeUser, ProviderKind: ProviderKindStub, AdvancedJSON: map[string]any{}},
		},
		Routes: []ProviderRouteRule{
			{ID: "acct-only", Model: "gpt", CredentialID: "cred-acct", AccountScoped: true, When: map[string]any{}},
		},
	}
	router := NewProviderRouter(cfg)

	dec := router.Decide(map[string]any{}, true, true)
	if dec.Denied != nil {
		t.Fatalf("expected nil denied, got: %+v", dec.Denied)
	}
	if dec.Selected != nil {
		t.Fatalf("expected nil selected when all routes are account-scoped, got: %s", dec.Selected.Route.ID)
	}
}

func stringPtr(value string) *string {
	return &value
}

func TestProviderRouterDecide_WhenMatchSelectsHighestPriority(t *testing.T) {
	cfg := ProviderRoutingConfig{
		Credentials: []ProviderCredential{
			{ID: "c1", Name: "openrouter", OwnerKind: CredentialScopePlatform, ProviderKind: ProviderKindOpenAI, AdvancedJSON: map[string]any{}},
			{ID: "c2", Name: "backup", OwnerKind: CredentialScopePlatform, ProviderKind: ProviderKindOpenAI, AdvancedJSON: map[string]any{}},
		},
		Routes: []ProviderRouteRule{
			{ID: "r-high", Model: "openai/gpt-oss-120b", CredentialID: "c1", When: map[string]any{"persona_id": "test"}, Priority: 100},
			{ID: "r-low", Model: "openai/gpt-4.1-mini", CredentialID: "c2", When: map[string]any{"persona_id": "test"}, Priority: 10},
		},
	}
	router := NewProviderRouter(cfg)

	decision := router.Decide(map[string]any{"persona_id": "test"}, false, false)
	if decision.Denied != nil {
		t.Fatalf("expected selected, got denied: %+v", decision.Denied)
	}
	if decision.Selected == nil {
		t.Fatal("expected selected route")
	}
	if decision.Selected.Route.ID != "r-high" {
		t.Fatalf("unexpected route id: %s", decision.Selected.Route.ID)
	}
}
