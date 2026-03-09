package shell

import (
	"context"
	"testing"
	"time"

	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/session"
)

func TestLoadLatestRestoreStateExpiredClearsBinding(t *testing.T) {
	store := newMemoryStateStore()
	registry := NewMemorySessionRestoreRegistry()
	now := time.Now().UTC()
	state := SessionRestoreState{
		Version:   shellStateVersion,
		Revision:  nextRestoreRevision(now),
		OrgID:     "org-a",
		SessionID: "sess-expired",
		Cwd:       "/workspace/demo",
		CreatedAt: now.Format(time.RFC3339Nano),
		ExpiresAt: now.Add(-time.Minute).Format(time.RFC3339Nano),
	}
	if err := saveRestoreState(context.Background(), store, registry, state); err != nil {
		t.Fatalf("save restore state: %v", err)
	}
	if _, err := loadLatestRestoreState(context.Background(), store, registry, "org-a", "sess-expired"); err == nil {
		t.Fatal("expected expired restore state to be hidden")
	}
	if _, err := registry.GetLatestRestoreRevision(context.Background(), "org-a", "sess-expired"); err == nil {
		t.Fatal("expected expired binding cleared")
	}
}

func TestManagerSweepExpiredRestoreStatesClearsMissingBinding(t *testing.T) {
	pool := &fakePool{agent: &fakeAgent{}}
	compute := session.NewManager(session.ManagerConfig{MaxSessions: 10, Pool: pool, MaxLifetimeSeconds: 3600})
	store := newMemoryStateStore()
	registry := NewMemorySessionRestoreRegistry()
	mgr := NewManager(compute, nil, store, registry, nil, logging.NewJSONLogger("test", nil), Config{})
	if err := registry.BindLatestRestoreRevision(context.Background(), "org-a", "sess-missing", "rev-missing"); err != nil {
		t.Fatalf("bind restore revision: %v", err)
	}
	if err := mgr.SweepExpiredRestoreStates(context.Background()); err != nil {
		t.Fatalf("sweep restore states: %v", err)
	}
	if _, err := registry.GetLatestRestoreRevision(context.Background(), "org-a", "sess-missing"); err == nil {
		t.Fatal("expected missing binding cleared")
	}
}
