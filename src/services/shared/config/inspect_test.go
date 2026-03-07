package config

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
)

func TestInspectShowsAllLayersWithEnvWinning(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(Entry{
		Key:     "x.k",
		Type:    TypeString,
		Default: "def",
		Scope:   ScopeBoth,
		EnvKeys: []string{"ARKLOOP_TEST_INSPECT_X_K"},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	orgID := uuid.New()
	store := &stubStore{
		platform: map[string]string{"x.k": "platform"},
		org: map[uuid.UUID]map[string]string{
			orgID: {"x.k": "org"},
		},
	}

	if err := os.Setenv("ARKLOOP_TEST_INSPECT_X_K", "env"); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("ARKLOOP_TEST_INSPECT_X_K") })

	inspection, err := Inspect(context.Background(), reg, store, "x.k", Scope{OrgID: &orgID})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if inspection.Effective.Source != "env" || inspection.Effective.Value != "env" {
		t.Fatalf("unexpected effective: %#v", inspection.Effective)
	}
	if inspection.Layers.Env == nil || *inspection.Layers.Env != "env" {
		t.Fatalf("unexpected env layer: %#v", inspection.Layers.Env)
	}
	if inspection.Layers.OrgDB == nil || *inspection.Layers.OrgDB != "org" {
		t.Fatalf("unexpected org layer: %#v", inspection.Layers.OrgDB)
	}
	if inspection.Layers.PlatformDB == nil || *inspection.Layers.PlatformDB != "platform" {
		t.Fatalf("unexpected platform layer: %#v", inspection.Layers.PlatformDB)
	}
	if inspection.Layers.Default != "def" {
		t.Fatalf("unexpected default layer: %q", inspection.Layers.Default)
	}
	if len(inspection.EnvKeys) != 1 || inspection.EnvKeys[0] != "ARKLOOP_TEST_INSPECT_X_K" {
		t.Fatalf("unexpected env keys: %#v", inspection.EnvKeys)
	}
}

func TestInspectWithoutOrgScopeFallsBackToPlatformThenDefault(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(Entry{
		Key:     "x.k",
		Type:    TypeString,
		Default: "def",
		Scope:   ScopeBoth,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	store := &stubStore{platform: map[string]string{"x.k": "platform"}}
	inspection, err := Inspect(context.Background(), reg, store, "x.k", Scope{})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if inspection.Effective.Source != "platform_db" || inspection.Effective.Value != "platform" {
		t.Fatalf("unexpected effective: %#v", inspection.Effective)
	}
	if inspection.Layers.OrgDB != nil {
		t.Fatalf("unexpected org layer: %#v", inspection.Layers.OrgDB)
	}
	if inspection.Layers.PlatformDB == nil || *inspection.Layers.PlatformDB != "platform" {
		t.Fatalf("unexpected platform layer: %#v", inspection.Layers.PlatformDB)
	}
}

func TestInspectWithoutDBValuesReturnsDefault(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(Entry{
		Key:     "x.k",
		Type:    TypeString,
		Default: "def",
		Scope:   ScopeBoth,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	inspection, err := Inspect(context.Background(), reg, &stubStore{}, "x.k", Scope{})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if inspection.Effective.Source != "default" || inspection.Effective.Value != "def" {
		t.Fatalf("unexpected effective: %#v", inspection.Effective)
	}
	if inspection.Layers.Env != nil || inspection.Layers.OrgDB != nil || inspection.Layers.PlatformDB != nil {
		t.Fatalf("unexpected layers: %#v", inspection.Layers)
	}
}
