//go:build desktop

package app

import (
	"context"
	"testing"

	"arkloop/services/shared/desktop"
	"arkloop/services/shared/localproviders"
	"arkloop/services/worker/internal/routing"
)

func TestResolveDesktopLocalProviderTestRouteUsesLocalProviderCatalog(t *testing.T) {
	t.Setenv("CODEX_API_KEY", "sk-test")

	selected, err := resolveDesktopLocalProviderTestRoute(context.Background(), desktop.LLMProviderModelTestRequest{
		ProviderID: localproviders.ProviderUUID(localproviders.CodexProviderID).String(),
		ModelID:    localproviders.RouteUUID(localproviders.CodexProviderID, "gpt-5.5").String(),
	})
	if err != nil {
		t.Fatalf("resolveDesktopLocalProviderTestRoute() error = %v", err)
	}
	if selected.Credential.ProviderKind != routing.ProviderKindCodexLocal {
		t.Fatalf("unexpected provider kind: %s", selected.Credential.ProviderKind)
	}
	if selected.Route.Model != "gpt-5.5" {
		t.Fatalf("unexpected model: %s", selected.Route.Model)
	}
}
