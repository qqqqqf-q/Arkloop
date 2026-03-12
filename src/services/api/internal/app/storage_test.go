//go:build !desktop

package app

import (
	"context"
	"testing"

	"arkloop/services/shared/objectstore"
)

func TestBuildStorageBucketOpenerFilesystem(t *testing.T) {
	opener, err := buildStorageBucketOpener(Config{
		StorageBackend: objectstore.BackendFilesystem,
		StorageRoot:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("build storage bucket opener: %v", err)
	}
	if opener == nil {
		t.Fatal("expected opener")
	}
	store, err := opener.Open(context.Background(), "arkloop")
	if err != nil {
		t.Fatalf("open bucket: %v", err)
	}
	if _, ok := store.(*objectstore.FilesystemStore); !ok {
		t.Fatalf("unexpected store type: %T", store)
	}
}
