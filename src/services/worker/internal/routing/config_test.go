package routing

import "testing"

func TestGetHighestPriorityRouteByCredentialName_Found(t *testing.T) {
	cfg := ProviderRoutingConfig{
		DefaultRouteID: "default",
		Credentials: []ProviderCredential{
			{ID: "cred-a", Name: "my-anthropic", Scope: CredentialScopePlatform, ProviderKind: ProviderKindStub, AdvancedJSON: map[string]any{}},
			{ID: "cred-b", Name: "my-openai", Scope: CredentialScopePlatform, ProviderKind: ProviderKindStub, AdvancedJSON: map[string]any{}},
		},
		Routes: []ProviderRouteRule{
			{ID: "route-b", Model: "gpt-4o", CredentialID: "cred-b", When: map[string]any{}},
			{ID: "route-a", Model: "claude-3", CredentialID: "cred-a", When: map[string]any{}},
		},
	}

	route, cred, ok := cfg.GetHighestPriorityRouteByCredentialName("my-anthropic")
	if !ok {
		t.Fatal("expected route to be found")
	}
	if route.ID != "route-a" {
		t.Fatalf("expected route-a, got %q", route.ID)
	}
	if cred.ID != "cred-a" {
		t.Fatalf("expected cred-a, got %q", cred.ID)
	}
}

func TestGetHighestPriorityRouteByCredentialName_CaseInsensitive(t *testing.T) {
	cfg := ProviderRoutingConfig{
		Credentials: []ProviderCredential{
			{ID: "cred-a", Name: "My-Anthropic", Scope: CredentialScopePlatform, ProviderKind: ProviderKindStub, AdvancedJSON: map[string]any{}},
		},
		Routes: []ProviderRouteRule{
			{ID: "route-a", Model: "claude-3", CredentialID: "cred-a", When: map[string]any{}},
		},
	}

	_, _, ok := cfg.GetHighestPriorityRouteByCredentialName("my-anthropic")
	if !ok {
		t.Fatal("expected case-insensitive match")
	}
}

func TestGetHighestPriorityRouteByCredentialName_NotFound(t *testing.T) {
	cfg := ProviderRoutingConfig{
		Credentials: []ProviderCredential{
			{ID: "cred-a", Name: "my-anthropic", Scope: CredentialScopePlatform, ProviderKind: ProviderKindStub, AdvancedJSON: map[string]any{}},
		},
		Routes: []ProviderRouteRule{
			{ID: "route-a", Model: "claude-3", CredentialID: "cred-a", When: map[string]any{}},
		},
	}

	_, _, ok := cfg.GetHighestPriorityRouteByCredentialName("nonexistent")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestGetHighestPriorityRouteByCredentialName_EmptyName(t *testing.T) {
	cfg := ProviderRoutingConfig{}
	_, _, ok := cfg.GetHighestPriorityRouteByCredentialName("")
	if ok {
		t.Fatal("expected false for empty name")
	}
}

func TestGetHighestPriorityRouteByCredentialName_CredentialWithNoRoute(t *testing.T) {
	cfg := ProviderRoutingConfig{
		Credentials: []ProviderCredential{
			{ID: "cred-a", Name: "orphan-cred", Scope: CredentialScopePlatform, ProviderKind: ProviderKindStub, AdvancedJSON: map[string]any{}},
		},
		Routes: []ProviderRouteRule{},
	}

	_, _, ok := cfg.GetHighestPriorityRouteByCredentialName("orphan-cred")
	if ok {
		t.Fatal("expected false when credential has no routes")
	}
}
