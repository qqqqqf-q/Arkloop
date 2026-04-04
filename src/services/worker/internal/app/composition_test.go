//go:build !desktop

package app

import (
	"context"
	"os"
	"reflect"
	"testing"

	"arkloop/services/shared/objectstore"
	"arkloop/services/worker/internal/llm"
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

type fakeArtifactStore struct{}

func (fakeArtifactStore) Put(_ context.Context, _ string, _ []byte) error {
	return nil
}

func (fakeArtifactStore) PutObject(_ context.Context, _ string, _ []byte, _ objectstore.PutOptions) error {
	return nil
}

func (fakeArtifactStore) Get(_ context.Context, _ string) ([]byte, error) {
	return nil, nil
}

func (fakeArtifactStore) GetWithContentType(_ context.Context, _ string) ([]byte, string, error) {
	return nil, "", nil
}

func (fakeArtifactStore) Head(_ context.Context, _ string) (objectstore.ObjectInfo, error) {
	return objectstore.ObjectInfo{}, nil
}

func (fakeArtifactStore) Delete(_ context.Context, _ string) error {
	return nil
}

func (fakeArtifactStore) ListPrefix(_ context.Context, _ string) ([]objectstore.ObjectInfo, error) {
	return nil, nil
}

func TestRegisterStoredArtifactTools(t *testing.T) {
	registry := tools.NewRegistry()
	executors := map[string]tools.Executor{}

	specs, registered, err := registerStoredArtifactTools(registry, executors, nil, fakeArtifactStore{})
	if err != nil {
		t.Fatalf("register stored artifact tools: %v", err)
	}
	if !registered {
		t.Fatal("expected stored artifact tools to register")
	}
	if len(specs) != 2 {
		t.Fatalf("expected 2 llm specs, got %d", len(specs))
	}
	for _, name := range []string{"create_artifact", "document_write"} {
		if _, ok := registry.Get(name); !ok {
			t.Fatalf("expected tool %s to be registered", name)
		}
		if executors[name] == nil {
			t.Fatalf("expected executor for %s", name)
		}
	}
}

func TestRegisterStoredArtifactToolsSkipsNilStore(t *testing.T) {
	registry := tools.NewRegistry()
	executors := map[string]tools.Executor{}
	baseSpecs := []llm.ToolSpec{{Name: "existing"}}

	specs, registered, err := registerStoredArtifactTools(registry, executors, baseSpecs, nil)
	if err != nil {
		t.Fatalf("register stored artifact tools: %v", err)
	}
	if registered {
		t.Fatal("expected no registration without store")
	}
	if !reflect.DeepEqual(specs, baseSpecs) {
		t.Fatalf("unexpected specs: got %v want %v", specs, baseSpecs)
	}
	if len(executors) != 0 {
		t.Fatalf("expected no executors, got %d", len(executors))
	}
}
