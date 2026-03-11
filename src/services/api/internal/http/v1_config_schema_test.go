package http

import (
	"testing"

	sharedconfig "arkloop/services/shared/config"
)

func TestFilterSchemaEntriesNonPlatformAdminOnlySeesOrgAndBoth(t *testing.T) {
	entries := []sharedconfig.Entry{
		{Key: "a", Scope: sharedconfig.ScopePlatform},
		{Key: "b", Scope: sharedconfig.ScopeProject},
		{Key: "c", Scope: sharedconfig.ScopeBoth},
	}

	filtered := filterSchemaEntries(entries, false)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(filtered))
	}
	if filtered[0].Key != "b" || filtered[1].Key != "c" {
		t.Fatalf("unexpected order/keys: %#v", filtered)
	}
}

func TestFilterSchemaEntriesPlatformAdminSeesAll(t *testing.T) {
	entries := []sharedconfig.Entry{
		{Key: "a", Scope: sharedconfig.ScopePlatform},
		{Key: "b", Scope: sharedconfig.ScopeProject},
		{Key: "c", Scope: sharedconfig.ScopeBoth},
	}

	filtered := filterSchemaEntries(entries, true)
	if len(filtered) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(filtered))
	}
	if filtered[0].Key != "a" || filtered[1].Key != "b" || filtered[2].Key != "c" {
		t.Fatalf("unexpected order/keys: %#v", filtered)
	}
}
