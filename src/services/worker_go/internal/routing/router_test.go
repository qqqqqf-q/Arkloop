package routing

import "testing"

func TestProviderRouterDecide_DefaultRoute(t *testing.T) {
	cfg := DefaultRoutingConfig()
	router := NewProviderRouter(cfg)

	decision := router.Decide(map[string]any{}, false)
	if decision.Denied != nil {
		t.Fatalf("expected selected, got denied: %+v", decision.Denied)
	}
	if decision.Selected == nil {
		t.Fatalf("expected selected")
	}
	if decision.Selected.Route.ID != "default" {
		t.Fatalf("unexpected route id: %s", decision.Selected.Route.ID)
	}
	if decision.Selected.Credential.ProviderKind != ProviderKindStub {
		t.Fatalf("unexpected provider kind: %s", decision.Selected.Credential.ProviderKind)
	}
}

func TestProviderRouterDecide_RequestedRoute(t *testing.T) {
	cfg := ProviderRoutingConfig{
		DefaultRouteID: "default",
		Credentials: []ProviderCredential{
			{
				ID:           "stub_default",
				Scope:        CredentialScopePlatform,
				ProviderKind: ProviderKindStub,
				AdvancedJSON: map[string]any{},
			},
			{
				ID:           "stub_alt",
				Scope:        CredentialScopePlatform,
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

	decision := router.Decide(map[string]any{"route_id": "alt"}, false)
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

	decision := router.Decide(map[string]any{"route_id": "missing"}, false)
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
		DefaultRouteID: "default",
		Credentials: []ProviderCredential{
			{
				ID:           "org_cred",
				Scope:        CredentialScopeOrg,
				ProviderKind: ProviderKindOpenAI,
				APIKeyEnv:    stringPtr("ARKLOOP_OPENAI_API_KEY"),
				OpenAIMode:   stringPtr("chat_completions"),
				AdvancedJSON: map[string]any{},
			},
		},
		Routes: []ProviderRouteRule{
			{ID: "default", Model: "gpt", CredentialID: "org_cred", When: map[string]any{}},
		},
	}
	router := NewProviderRouter(cfg)

	decision := router.Decide(map[string]any{}, false)
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

func stringPtr(value string) *string {
	return &value
}
