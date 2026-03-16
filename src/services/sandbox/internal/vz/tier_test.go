//go:build darwin

package vz

import (
	"testing"

	"arkloop/services/sandbox/internal/session"
)

func TestResourcesFor_Lite(t *testing.T) {
	r := resourcesFor(session.TierLite)
	if r.CPUCount != 1 {
		t.Fatalf("expected CPUCount=1, got %d", r.CPUCount)
	}
	if r.MemoryMiB != 256 {
		t.Fatalf("expected MemoryMiB=256, got %d", r.MemoryMiB)
	}
}

func TestResourcesFor_Pro(t *testing.T) {
	r := resourcesFor(session.TierPro)
	if r.CPUCount != 1 {
		t.Fatalf("expected CPUCount=1, got %d", r.CPUCount)
	}
	if r.MemoryMiB != 1024 {
		t.Fatalf("expected MemoryMiB=1024, got %d", r.MemoryMiB)
	}
}

func TestResourcesFor_Browser(t *testing.T) {
	r := resourcesFor(session.TierBrowser)
	if r.CPUCount != 1 {
		t.Fatalf("expected CPUCount=1, got %d", r.CPUCount)
	}
	if r.MemoryMiB != 512 {
		t.Fatalf("expected MemoryMiB=512, got %d", r.MemoryMiB)
	}
}

func TestResourcesFor_UnknownFallsBackToLite(t *testing.T) {
	r := resourcesFor("unknown")
	lite := resourcesFor(session.TierLite)
	if r != lite {
		t.Fatalf("expected unknown tier to match lite defaults %+v, got %+v", lite, r)
	}
}
