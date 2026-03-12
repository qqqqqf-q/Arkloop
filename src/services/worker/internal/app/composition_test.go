//go:build !desktop

package app

import (
	"context"
	"os"
	"reflect"
	"testing"

	"arkloop/services/shared/objectstore"
	"arkloop/services/worker/internal/tools"
)

func TestResolveBaseToolAllowlistNamesIgnoresEnvAllowlist(t *testing.T) {
	t.Setenv("ARKLOOP_TOOL_ALLOWLIST", "tool_b")

	registry := tools.NewRegistry()
	for _, spec := range []tools.AgentToolSpec{
		{Name: "tool_a", Version: "1", Description: "a", RiskLevel: tools.RiskLevelLow},
		{Name: "tool_b", Version: "1", Description: "b", RiskLevel: tools.RiskLevelLow},
	} {
		if err := registry.Register(spec); err != nil {
			t.Fatalf("register tool: %v", err)
		}
	}

	got := resolveBaseToolAllowlistNames(context.Background(), registry)
	want := []string{"tool_a", "tool_b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected allowlist names: got %v want %v", got, want)
	}

	if raw := os.Getenv("ARKLOOP_TOOL_ALLOWLIST"); raw == "" {
		t.Fatal("expected env to stay set during test")
	}
}

func TestBuildStorageBucketOpenerFromEnvPrefersFilesystem(t *testing.T) {
	t.Setenv(objectstore.StorageRootEnv, t.TempDir())
	t.Setenv("ARKLOOP_S3_ENDPOINT", "http://seaweedfs:8333")
	t.Setenv("ARKLOOP_S3_ACCESS_KEY", "key")
	t.Setenv("ARKLOOP_S3_SECRET_KEY", "secret")

	opener, err := buildStorageBucketOpenerFromEnv()
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

func TestBuildMessageAttachmentStoreFilesystem(t *testing.T) {
	t.Setenv(objectstore.StorageBackendEnv, objectstore.BackendFilesystem)
	t.Setenv(objectstore.StorageRootEnv, t.TempDir())
	t.Setenv("ARKLOOP_S3_BUCKET", "arkloop")

	store, err := buildMessageAttachmentStore(context.Background())
	if err != nil {
		t.Fatalf("build message attachment store: %v", err)
	}
	if _, ok := store.(*objectstore.FilesystemStore); !ok {
		t.Fatalf("unexpected store type: %T", store)
	}
}
