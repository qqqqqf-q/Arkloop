package config

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type stubStore struct {
	platform map[string]string
	project  map[uuid.UUID]map[string]string

	platformCalls int
	projectCalls  int
}

func (s *stubStore) GetPlatformSetting(ctx context.Context, key string) (string, bool, error) {
	_ = ctx
	s.platformCalls++
	val, ok := s.platform[key]
	return val, ok, nil
}

func (s *stubStore) GetProjectSetting(ctx context.Context, projectID uuid.UUID, key string) (string, bool, error) {
	_ = ctx
	s.projectCalls++
	m, ok := s.project[projectID]
	if !ok {
		return "", false, nil
	}
	val, ok := m[key]
	return val, ok, nil
}

func TestResolvePriorityEnvOverridesAll(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(Entry{
		Key:     "x.k",
		Type:    TypeString,
		Default: "def",
		Scope:   ScopeBoth,
		EnvKeys: []string{"ARKLOOP_TEST_X_K"},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	store := &stubStore{
		platform: map[string]string{"x.k": "p"},
		project:  map[uuid.UUID]map[string]string{},
	}
	cache := NewMemoryCache()
	resolver, _ := NewResolver(reg, store, cache, 60*time.Second)

	if err := os.Setenv("ARKLOOP_TEST_X_K", "env"); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("ARKLOOP_TEST_X_K") })

	projectID := uuid.New()
	val, src, err := resolver.ResolveWithSource(context.Background(), "x.k", Scope{ProjectID: &projectID})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if val != "env" || src != "env" {
		t.Fatalf("unexpected value/source: %q %q", val, src)
	}
}

func TestResolveEnvEmptyIsIgnoredFallsBackToDB(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(Entry{
		Key:     "x.k",
		Type:    TypeString,
		Default: "def",
		Scope:   ScopeBoth,
		EnvKeys: []string{"ARKLOOP_TEST_X_K"},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	projectID := uuid.New()
	store := &stubStore{
		platform: map[string]string{"x.k": "p"},
		project: map[uuid.UUID]map[string]string{
			projectID: {"x.k": "o"},
		},
	}
	resolver, _ := NewResolver(reg, store, nil, 0)

	if err := os.Setenv("ARKLOOP_TEST_X_K", ""); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("ARKLOOP_TEST_X_K") })

	val, src, err := resolver.ResolveWithSource(context.Background(), "x.k", Scope{ProjectID: &projectID})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if val != "o" || src != "project_db" {
		t.Fatalf("unexpected value/source: %q %q", val, src)
	}
}

func TestResolvePriorityProjectThenPlatformThenDefault(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(Entry{
		Key:     "x.k",
		Type:    TypeString,
		Default: "def",
		Scope:   ScopeBoth,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	projectID := uuid.New()
	store := &stubStore{
		platform: map[string]string{"x.k": "p"},
		project: map[uuid.UUID]map[string]string{
			projectID: {"x.k": "o"},
		},
	}
	resolver, _ := NewResolver(reg, store, nil, 0)

	val, src, err := resolver.ResolveWithSource(context.Background(), "x.k", Scope{ProjectID: &projectID})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if val != "o" || src != "project_db" {
		t.Fatalf("unexpected value/source: %q %q", val, src)
	}

	val, src, err = resolver.ResolveWithSource(context.Background(), "x.k", Scope{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if val != "p" || src != "platform_db" {
		t.Fatalf("unexpected value/source: %q %q", val, src)
	}

	store.platform = map[string]string{}
	val, src, err = resolver.ResolveWithSource(context.Background(), "x.k", Scope{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if val != "def" || src != "default" {
		t.Fatalf("unexpected value/source: %q %q", val, src)
	}
}

func TestResolvePrefixReturnsAllRegisteredKeys(t *testing.T) {
	reg := NewRegistry()
	for _, key := range []string{"p.a", "p.b"} {
		if err := reg.Register(Entry{
			Key:     key,
			Type:    TypeString,
			Default: "",
			Scope:   ScopePlatform,
		}); err != nil {
			t.Fatalf("register %s: %v", key, err)
		}
	}

	store := &stubStore{platform: map[string]string{"p.a": "x"}}
	resolver, _ := NewResolver(reg, store, nil, 0)

	m, err := resolver.ResolvePrefix(context.Background(), "p.", Scope{})
	if err != nil {
		t.Fatalf("resolve prefix: %v", err)
	}
	if _, ok := m["p.a"]; !ok {
		t.Fatalf("missing p.a")
	}
	if _, ok := m["p.b"]; !ok {
		t.Fatalf("missing p.b")
	}
	if m["p.a"] != "x" || m["p.b"] != "" {
		t.Fatalf("unexpected values: %#v", m)
	}
}

func TestScopePlatformIgnoresProjectOverrides(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(Entry{
		Key:     "k",
		Type:    TypeString,
		Default: "d",
		Scope:   ScopePlatform,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	projectID := uuid.New()
	store := &stubStore{
		platform: map[string]string{"k": "p"},
		project: map[uuid.UUID]map[string]string{
			projectID: {"k": "o"},
		},
	}
	resolver, _ := NewResolver(reg, store, nil, 0)

	val, src, err := resolver.ResolveWithSource(context.Background(), "k", Scope{ProjectID: &projectID})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if val != "p" || src != "platform_db" {
		t.Fatalf("unexpected value/source: %q %q", val, src)
	}
}

func TestCacheAndInvalidate(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(Entry{
		Key:     "k",
		Type:    TypeString,
		Default: "",
		Scope:   ScopePlatform,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	store := &stubStore{platform: map[string]string{"k": "v"}}
	cache := NewMemoryCache()
	resolver, _ := NewResolver(reg, store, cache, 60*time.Second)

	v1, err := resolver.Resolve(context.Background(), "k", Scope{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	v2, err := resolver.Resolve(context.Background(), "k", Scope{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if v1 != "v" || v2 != "v" {
		t.Fatalf("unexpected values: %q %q", v1, v2)
	}
	if store.platformCalls != 1 {
		t.Fatalf("expected 1 platform call, got %d", store.platformCalls)
	}

	if err := resolver.Invalidate(context.Background(), "k", Scope{}); err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	_, _ = resolver.Resolve(context.Background(), "k", Scope{})
	if store.platformCalls != 2 {
		t.Fatalf("expected 2 platform calls after invalidate, got %d", store.platformCalls)
	}
}

func TestResolveUnregisteredKeyReturnsError(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(Entry{
		Key:     "known.key",
		Type:    TypeString,
		Default: "v",
		Scope:   ScopePlatform,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	store := &stubStore{platform: map[string]string{}}
	resolver, _ := NewResolver(reg, store, nil, 0)

	_, err := resolver.Resolve(context.Background(), "unknown.key", Scope{})
	if err == nil {
		t.Fatalf("expected error for unregistered key, got nil")
	}
	if !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("error should mention 'not registered', got: %v", err)
	}
}
