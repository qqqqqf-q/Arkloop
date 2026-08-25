package main

import (
	"context"
	"errors"
	"testing"

	"arkloop/services/sandbox/internal/app"
	"arkloop/services/shared/objectstore"
)

type fakeLifecycleStore struct {
	days int
	err  error
	set  bool
}

func (s *fakeLifecycleStore) SetLifecycleExpirationDays(_ context.Context, days int) error {
	s.set = true
	s.days = days
	return s.err
}

func TestApplyStateStoreLifecycle(t *testing.T) {
	store := &fakeLifecycleStore{}
	cfg := app.DefaultConfig()
	cfg.RestoreTTLDays = 7

	if err := applyStateStoreLifecycle(context.Background(), cfg, store); err != nil {
		t.Fatalf("apply lifecycle failed: %v", err)
	}
	if !store.set || store.days != 7 {
		t.Fatalf("unexpected lifecycle call: %#v", store)
	}
}

func TestApplyStateStoreLifecycleSkipWhenDisabled(t *testing.T) {
	store := &fakeLifecycleStore{}
	cfg := app.DefaultConfig()
	cfg.RestoreTTLDays = 0

	if err := applyStateStoreLifecycle(context.Background(), cfg, store); err != nil {
		t.Fatalf("apply lifecycle failed: %v", err)
	}
	if store.set {
		t.Fatalf("lifecycle should be skipped, got %#v", store)
	}
}

func TestApplyStateStoreLifecycleReturnsError(t *testing.T) {
	store := &fakeLifecycleStore{err: errors.New("boom")}
	cfg := app.DefaultConfig()
	cfg.RestoreTTLDays = 3

	if err := applyStateStoreLifecycle(context.Background(), cfg, store); err == nil {
		t.Fatal("expected lifecycle error")
	}
}

func TestBuildStorageBucketOpenerFilesystem(t *testing.T) {
	cfg := app.DefaultConfig()
	cfg.StorageBackend = objectstore.BackendFilesystem
	cfg.StorageRoot = t.TempDir()

	opener, err := buildStorageBucketOpener(cfg)
	if err != nil {
		t.Fatalf("build storage bucket opener: %v", err)
	}
	if opener == nil {
		t.Fatal("expected opener")
	}
	store, err := opener.Open(context.Background(), objectstore.ArtifactBucket)
	if err != nil {
		t.Fatalf("open artifact bucket: %v", err)
	}
	if _, ok := store.(*objectstore.FilesystemStore); !ok {
		t.Fatalf("unexpected store type: %T", store)
	}
}
