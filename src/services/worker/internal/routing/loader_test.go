package routing

import (
	"context"
	"testing"
)

func TestDesktopSQLiteRoutingLoaderCachesShortWindow(t *testing.T) {
	calls := 0
	cfg := ProviderRoutingConfig{
		Credentials: []ProviderCredential{{ID: "cred", ProviderKind: ProviderKindStub}},
		Routes:      []ProviderRouteRule{{ID: "route", CredentialID: "cred", Model: "stub"}},
	}
	loader := NewDesktopSQLiteRoutingLoader(func(context.Context) (ProviderRoutingConfig, error) {
		calls++
		return cfg, nil
	}, ProviderRoutingConfig{})

	for range 3 {
		loaded, err := loader.Load(context.Background(), nil)
		if err != nil {
			t.Fatalf("load routing config: %v", err)
		}
		if len(loaded.Routes) != 1 || loaded.Routes[0].ID != "route" {
			t.Fatalf("unexpected routing config: %#v", loaded)
		}
	}
	if calls != 1 {
		t.Fatalf("desktop loader calls = %d, want 1", calls)
	}
}
