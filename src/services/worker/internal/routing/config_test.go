package routing

import "testing"

func TestGetHighestPriorityRouteByCredentialName_Found(t *testing.T) {
	cfg := ProviderRoutingConfig{
		DefaultRouteID: "default",
		Credentials: []ProviderCredential{
			{ID: "cred-a", Name: "my-anthropic", OwnerKind: CredentialScopePlatform, ProviderKind: ProviderKindStub, AdvancedJSON: map[string]any{}},
			{ID: "cred-b", Name: "my-openai", OwnerKind: CredentialScopePlatform, ProviderKind: ProviderKindStub, AdvancedJSON: map[string]any{}},
		},
		Routes: []ProviderRouteRule{
			{ID: "route-b", Model: "gpt-4o", CredentialID: "cred-b", When: map[string]any{}},
			{ID: "route-a", Model: "claude-3", CredentialID: "cred-a", When: map[string]any{}},
		},
	}

	route, cred, ok := cfg.GetHighestPriorityRouteByCredentialName("my-anthropic", map[string]any{})
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
			{ID: "cred-a", Name: "My-Anthropic", OwnerKind: CredentialScopePlatform, ProviderKind: ProviderKindStub, AdvancedJSON: map[string]any{}},
		},
		Routes: []ProviderRouteRule{
			{ID: "route-a", Model: "claude-3", CredentialID: "cred-a", When: map[string]any{}},
		},
	}

	_, _, ok := cfg.GetHighestPriorityRouteByCredentialName("my-anthropic", map[string]any{})
	if !ok {
		t.Fatal("expected case-insensitive match")
	}
}

func TestGetHighestPriorityRouteByCredentialName_NotFound(t *testing.T) {
	cfg := ProviderRoutingConfig{
		Credentials: []ProviderCredential{
			{ID: "cred-a", Name: "my-anthropic", OwnerKind: CredentialScopePlatform, ProviderKind: ProviderKindStub, AdvancedJSON: map[string]any{}},
		},
		Routes: []ProviderRouteRule{
			{ID: "route-a", Model: "claude-3", CredentialID: "cred-a", When: map[string]any{}},
		},
	}

	_, _, ok := cfg.GetHighestPriorityRouteByCredentialName("nonexistent", map[string]any{})
	if ok {
		t.Fatal("expected not found")
	}
}

func TestGetHighestPriorityRouteByCredentialName_EmptyName(t *testing.T) {
	cfg := ProviderRoutingConfig{}
	_, _, ok := cfg.GetHighestPriorityRouteByCredentialName("", map[string]any{})
	if ok {
		t.Fatal("expected false for empty name")
	}
}

func TestGetHighestPriorityRouteByCredentialName_CredentialWithNoRoute(t *testing.T) {
	cfg := ProviderRoutingConfig{
		Credentials: []ProviderCredential{
			{ID: "cred-a", Name: "orphan-cred", OwnerKind: CredentialScopePlatform, ProviderKind: ProviderKindStub, AdvancedJSON: map[string]any{}},
		},
		Routes: []ProviderRouteRule{},
	}

	_, _, ok := cfg.GetHighestPriorityRouteByCredentialName("orphan-cred", map[string]any{})
	if ok {
		t.Fatal("expected false when credential has no routes")
	}
}

// TestGetHighestPriorityRouteByCredentialName_PrefersWhenMatch 验证同凭证下优先选命中 When 条件的路由。
func TestGetHighestPriorityRouteByCredentialName_PrefersWhenMatch(t *testing.T) {
	cfg := ProviderRoutingConfig{
		Credentials: []ProviderCredential{
			{ID: "cred-a", Name: "my-anthropic", OwnerKind: CredentialScopePlatform, ProviderKind: ProviderKindStub, AdvancedJSON: map[string]any{}},
		},
		Routes: []ProviderRouteRule{
			// 第一条：有 When 条件，匹配特定 persona
			{ID: "route-specific", Model: "claude-3-opus", CredentialID: "cred-a", When: map[string]any{"persona_id": "code-review"}},
			// 第二条：无 When 条件，兜底
			{ID: "route-fallback", Model: "claude-3-haiku", CredentialID: "cred-a", When: map[string]any{}},
		},
	}

	// inputJSON 命中第一条路由的 When 条件
	input := map[string]any{"persona_id": "code-review"}
	route, _, ok := cfg.GetHighestPriorityRouteByCredentialName("my-anthropic", input)
	if !ok {
		t.Fatal("expected route to be found")
	}
	if route.ID != "route-specific" {
		t.Fatalf("expected route-specific (When match), got %q", route.ID)
	}
}

// TestGetHighestPriorityRouteByCredentialName_FallbackWhenNoMatch 验证 When 条件不命中时回退到首条路由。
func TestGetHighestPriorityRouteByCredentialName_FallbackWhenNoMatch(t *testing.T) {
	cfg := ProviderRoutingConfig{
		Credentials: []ProviderCredential{
			{ID: "cred-a", Name: "my-anthropic", OwnerKind: CredentialScopePlatform, ProviderKind: ProviderKindStub, AdvancedJSON: map[string]any{}},
		},
		Routes: []ProviderRouteRule{
			{ID: "route-specific", Model: "claude-3-opus", CredentialID: "cred-a", When: map[string]any{"persona_id": "code-review"}},
			{ID: "route-fallback", Model: "claude-3-haiku", CredentialID: "cred-a", When: map[string]any{}},
		},
	}

	// inputJSON 不匹配任何 When 条件
	input := map[string]any{"persona_id": "chat"}
	route, _, ok := cfg.GetHighestPriorityRouteByCredentialName("my-anthropic", input)
	if !ok {
		t.Fatal("expected fallback route to be found")
	}
	if route.ID != "route-fallback" {
		t.Fatalf("expected route-fallback (no When match), got %q", route.ID)
	}
}
