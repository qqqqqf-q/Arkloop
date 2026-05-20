package routing

import (
	"testing"

	"arkloop/services/shared/localproviders"
)

func TestAppendLocalProvidersAddsSyntheticCredentialAndRoutes(t *testing.T) {
	cfg := AppendLocalProviders(ProviderRoutingConfig{}, []localproviders.ProviderStatus{{
		ID:          localproviders.CodexProviderID,
		DisplayName: localproviders.CodexDisplayName,
		Provider:    localproviders.CodexProviderKind,
		AuthMode:    localproviders.AuthModeAPIKey,
		Models: []localproviders.Model{{
			ID:              "gpt-5.3-codex",
			ContextLength:   400000,
			MaxOutputTokens: 128000,
			ToolCalling:     true,
			Reasoning:       true,
			Priority:        900,
		}, {
			ID:            "gpt-hidden",
			ContextLength: 400000,
			ToolCalling:   true,
			Reasoning:     true,
			Hidden:        true,
			Priority:      1000,
		}},
	}})

	if len(cfg.Credentials) != 1 {
		t.Fatalf("expected one credential, got %d", len(cfg.Credentials))
	}
	credential := cfg.Credentials[0]
	if credential.ID != localproviders.CodexProviderID || credential.ProviderKind != ProviderKindCodexLocal {
		t.Fatalf("unexpected credential: %#v", credential)
	}
	if credential.APIKeyEnv != nil || credential.APIKeyValue != nil {
		t.Fatalf("local provider must not store API key references: %#v", credential)
	}
	if len(cfg.Routes) != 1 {
		t.Fatalf("unexpected routes/default: %#v", cfg)
	}
	route := cfg.Routes[0]
	if route.CredentialID != credential.ID || route.Model != "gpt-5.3-codex" || route.AccountScoped {
		t.Fatalf("unexpected route: %#v", route)
	}
}
