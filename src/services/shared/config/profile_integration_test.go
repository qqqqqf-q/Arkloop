package config

import (
	"context"
	"fmt"
	"testing"
)

// mockResolver implements the Resolver interface using the default registry
// to return default values for known config keys.
type mockResolver struct {
	registry *Registry
}

func (m *mockResolver) Resolve(_ context.Context, key string, _ Scope) (string, error) {
	entry, ok := m.registry.Get(key)
	if !ok {
		return "", fmt.Errorf("config key not registered: %s", key)
	}
	return entry.Default, nil
}

func (m *mockResolver) ResolvePrefix(_ context.Context, prefix string, _ Scope) (map[string]string, error) {
	entries := m.registry.ListByPrefix(prefix)
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		out[e.Key] = e.Default
	}
	return out, nil
}

func newMockResolver() *mockResolver {
	return &mockResolver{registry: DefaultRegistry()}
}

func TestResolveProfile_Fast(t *testing.T) {
	r := newMockResolver()
	mapping, err := ResolveProfile(context.Background(), r, "fast")
	if err != nil {
		t.Fatalf("ResolveProfile(fast) error: %v", err)
	}
	if mapping.Provider != "anthropic" {
		t.Errorf("Provider = %q, want %q", mapping.Provider, "anthropic")
	}
	if mapping.Model != "claude-haiku-3-5" {
		t.Errorf("Model = %q, want %q", mapping.Model, "claude-haiku-3-5")
	}
}

func TestResolveProfile_Balanced(t *testing.T) {
	r := newMockResolver()
	mapping, err := ResolveProfile(context.Background(), r, "balanced")
	if err != nil {
		t.Fatalf("ResolveProfile(balanced) error: %v", err)
	}
	if mapping.Provider != "anthropic" {
		t.Errorf("Provider = %q, want %q", mapping.Provider, "anthropic")
	}
	if mapping.Model != "claude-sonnet-4-5" {
		t.Errorf("Model = %q, want %q", mapping.Model, "claude-sonnet-4-5")
	}
}

func TestResolveProfile_Strong(t *testing.T) {
	r := newMockResolver()
	mapping, err := ResolveProfile(context.Background(), r, "strong")
	if err != nil {
		t.Fatalf("ResolveProfile(strong) error: %v", err)
	}
	if mapping.Provider != "anthropic" {
		t.Errorf("Provider = %q, want %q", mapping.Provider, "anthropic")
	}
	if mapping.Model != "claude-sonnet-4-5" {
		t.Errorf("Model = %q, want %q", mapping.Model, "claude-sonnet-4-5")
	}
}

func TestResolveProfile_Unknown(t *testing.T) {
	r := newMockResolver()
	_, err := ResolveProfile(context.Background(), r, "nonexistent-profile")
	if err == nil {
		t.Fatal("ResolveProfile(nonexistent-profile) expected error, got nil")
	}
}

func TestResolveProfile_CaseInsensitive(t *testing.T) {
	r := newMockResolver()
	mapping, err := ResolveProfile(context.Background(), r, "FAST")
	if err != nil {
		t.Fatalf("ResolveProfile(FAST) error: %v", err)
	}
	if mapping.Provider != "anthropic" || mapping.Model != "claude-haiku-3-5" {
		t.Errorf("got %s^%s, want anthropic^claude-haiku-3-5", mapping.Provider, mapping.Model)
	}
}

func TestResolveProfile_EmptyName(t *testing.T) {
	r := newMockResolver()
	_, err := ResolveProfile(context.Background(), r, "")
	if err == nil {
		t.Fatal("ResolveProfile('') expected error, got nil")
	}
}
