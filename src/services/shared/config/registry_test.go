package config

import (
	"strings"
	"testing"
)

func TestRegistryRegisterAcceptsNumberType(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(Entry{
		Key:     "k",
		Type:    TypeNumber,
		Default: "",
		Scope:   ScopePlatform,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
}

func TestRegistryRegisterRejectsUnknownType(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(Entry{
		Key:     "k",
		Type:    "unknown",
		Default: "",
		Scope:   ScopePlatform,
	}); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRegistryRegisterDuplicateSameMetadataOK(t *testing.T) {
	reg := NewRegistry()
	e := Entry{
		Key:     "app.name",
		Type:    TypeString,
		Default: "MyApp",
		Scope:   ScopePlatform,
	}
	if err := reg.Register(e); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := reg.Register(e); err != nil {
		t.Fatalf("second register identical entry: %v", err)
	}
}

func TestRegistryRegisterDuplicateDifferentMetadataError(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(Entry{
		Key:     "app.name",
		Type:    TypeString,
		Default: "MyApp",
		Scope:   ScopePlatform,
	}); err != nil {
		t.Fatalf("first register: %v", err)
	}

	err := reg.Register(Entry{
		Key:     "app.name",
		Type:    TypeInt,
		Default: "0",
		Scope:   ScopePlatform,
	})
	if err == nil {
		t.Fatalf("expected error for duplicate with different type")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("expected error containing 'already registered', got: %v", err)
	}
}

func TestRegistryRegisterEmptyKeyError(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register(Entry{
		Key:     "",
		Type:    TypeString,
		Default: "",
		Scope:   ScopePlatform,
	})
	if err == nil {
		t.Fatalf("expected error for empty key")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected error containing 'empty', got: %v", err)
	}
}

func TestRegistryRegisterWhitespaceOnlyKeyError(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register(Entry{
		Key:     "   ",
		Type:    TypeString,
		Default: "",
		Scope:   ScopePlatform,
	})
	if err == nil {
		t.Fatalf("expected error for whitespace-only key")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected error containing 'empty', got: %v", err)
	}
}

func TestRegistryRegisterKeyWithInternalWhitespaceError(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register(Entry{
		Key:     "a b",
		Type:    TypeString,
		Default: "",
		Scope:   ScopePlatform,
	})
	if err == nil {
		t.Fatalf("expected error for key with internal whitespace")
	}
	if !strings.Contains(err.Error(), "whitespace") {
		t.Fatalf("expected error containing 'whitespace', got: %v", err)
	}
}

func TestRegistryRegisterKeyTrimsLeadingTrailingSpaces(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register(Entry{
		Key:     " foo.bar ",
		Type:    TypeString,
		Default: "",
		Scope:   ScopePlatform,
	})
	if err != nil {
		t.Fatalf("register with leading/trailing spaces: %v", err)
	}

	_, ok := reg.Get("foo.bar")
	if !ok {
		t.Fatalf("expected to find entry with key 'foo.bar'")
	}
}

func TestRegistryRegisterInvalidScopeError(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register(Entry{
		Key:     "k",
		Type:    TypeString,
		Default: "",
		Scope:   "invalid",
	})
	if err == nil {
		t.Fatalf("expected error for invalid scope")
	}
	if !strings.Contains(err.Error(), "scope") {
		t.Fatalf("expected error containing 'scope', got: %v", err)
	}
}

func TestRegistryGet(t *testing.T) {
	reg := NewRegistry()
	e := Entry{
		Key:     "db.host",
		Type:    TypeString,
		Default: "localhost",
		Scope:   ScopeProject,
	}
	if err := reg.Register(e); err != nil {
		t.Fatalf("register: %v", err)
	}

	retrieved, ok := reg.Get("db.host")
	if !ok {
		t.Fatalf("expected to find registered entry")
	}
	if retrieved.Key != "db.host" {
		t.Fatalf("expected key 'db.host', got %q", retrieved.Key)
	}

	_, ok = reg.Get("nonexistent")
	if ok {
		t.Fatalf("expected not to find nonexistent key")
	}
}

func TestRegistryListOrdering(t *testing.T) {
	reg := NewRegistry()
	keys := []string{"c", "a", "b"}
	for _, k := range keys {
		if err := reg.Register(Entry{
			Key:     k,
			Type:    TypeString,
			Default: "",
			Scope:   ScopePlatform,
		}); err != nil {
			t.Fatalf("register %s: %v", k, err)
		}
	}

	list := reg.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(list))
	}

	expected := []string{"a", "b", "c"}
	for i, e := range list {
		if e.Key != expected[i] {
			t.Fatalf("at index %d: expected %q, got %q", i, expected[i], e.Key)
		}
	}
}

func TestRegistryListByPrefix(t *testing.T) {
	reg := NewRegistry()
	entries := []Entry{
		{Key: "app.name", Type: TypeString, Default: "", Scope: ScopePlatform},
		{Key: "app.version", Type: TypeString, Default: "", Scope: ScopePlatform},
		{Key: "db.host", Type: TypeString, Default: "", Scope: ScopeProject},
	}
	for _, e := range entries {
		if err := reg.Register(e); err != nil {
			t.Fatalf("register %s: %v", e.Key, err)
		}
	}

	list := reg.ListByPrefix("app.")
	if len(list) != 2 {
		t.Fatalf("expected 2 entries with prefix 'app.', got %d", len(list))
	}

	if list[0].Key != "app.name" {
		t.Fatalf("expected first key 'app.name', got %q", list[0].Key)
	}
	if list[1].Key != "app.version" {
		t.Fatalf("expected second key 'app.version', got %q", list[1].Key)
	}
}

func TestRegistryListByPrefixEmptyString(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(Entry{
		Key:     "k",
		Type:    TypeString,
		Default: "",
		Scope:   ScopePlatform,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	list := reg.ListByPrefix("")
	if list != nil {
		t.Fatalf("expected nil for empty prefix, got %v", list)
	}
}

func TestRegistryListByPrefixNoMatch(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(Entry{
		Key:     "app.name",
		Type:    TypeString,
		Default: "",
		Scope:   ScopePlatform,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	list := reg.ListByPrefix("zzz.")
	if list == nil {
		t.Fatalf("expected empty slice, got nil")
	}
	if len(list) != 0 {
		t.Fatalf("expected empty slice, got %d entries", len(list))
	}
}

func TestRegistryNilReceiver(t *testing.T) {
	var r *Registry = nil

	_, ok := r.Get("k")
	if ok {
		t.Fatalf("expected false for Get on nil receiver")
	}

	list := r.List()
	if list != nil {
		t.Fatalf("expected nil for List on nil receiver, got %v", list)
	}

	err := r.Register(Entry{
		Key:     "k",
		Type:    TypeString,
		Default: "",
		Scope:   ScopePlatform,
	})
	if err == nil {
		t.Fatalf("expected error for Register on nil receiver")
	}
}
