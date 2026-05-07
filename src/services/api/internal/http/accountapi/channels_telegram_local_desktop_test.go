//go:build desktop

package accountapi

import (
	"testing"

	"arkloop/services/shared/localproviders"
)

func TestAppendLocalTelegramSelectorStatusCandidates(t *testing.T) {
	candidates := appendLocalTelegramSelectorStatusCandidates(nil, []localproviders.ProviderStatus{
		{
			ID:          localproviders.CodexProviderID,
			DisplayName: localproviders.CodexDisplayName,
			Provider:    localproviders.CodexProviderKind,
			AuthMode:    localproviders.AuthModeOAuth,
			Models: []localproviders.Model{
				{ID: "gpt-5.4", Priority: 900},
				{ID: "hidden-model", Hidden: true},
			},
		},
	})

	selected, ok := resolveTelegramSelectorCandidate(candidates, "Codex (Local)^gpt-5.4")
	if !ok {
		t.Fatal("expected local Codex model selector to resolve")
	}
	if selected.credentialName != localproviders.CodexDisplayName {
		t.Fatalf("unexpected credential name: %q", selected.credentialName)
	}
	if selected.ownerKind != "platform" {
		t.Fatalf("expected platform owner kind, got %q", selected.ownerKind)
	}

	if _, ok := resolveTelegramSelectorCandidate(candidates, "Codex (Local)^hidden-model"); ok {
		t.Fatal("hidden local model must not resolve")
	}
}
