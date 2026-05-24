package syncer

import (
	"testing"
	"time"
)

func TestBuildDaemonSourceKnownNames(t *testing.T) {
	opts := DaemonOptions{IdleThreshold: 5 * time.Minute}
	for _, name := range []string{"window", "clipboard", "keyboard", "mouse"} {
		src, err := buildDaemonSource(name, opts)
		if err != nil {
			t.Fatalf("buildDaemonSource(%q) error: %v", name, err)
		}
		if src.Name() != name {
			t.Fatalf("expected name=%q, got %q", name, src.Name())
		}
	}
}

func TestBuildDaemonSourceUnknown(t *testing.T) {
	opts := DaemonOptions{}
	_, err := buildDaemonSource("nonexistent", opts)
	if err == nil {
		t.Fatal("expected error for unknown source")
	}
}

func TestBuildSourceKnownNames(t *testing.T) {
	for _, name := range []string{"codex", "chrome"} {
		src, err := buildSource(name)
		if err != nil {
			t.Fatalf("buildSource(%q) error: %v", name, err)
		}
		if src.Name() != name {
			t.Fatalf("expected name=%q, got %q", name, src.Name())
		}
	}
}

func TestBuildSourceUnknown(t *testing.T) {
	_, err := buildSource("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown source")
	}
}
