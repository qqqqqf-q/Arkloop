package toolruntime

import (
	"context"
	"testing"

	sharedconfig "arkloop/services/shared/config"
)

type stubResolver struct {
	values map[string]string
}

func (r stubResolver) Resolve(_ context.Context, key string, _ sharedconfig.Scope) (string, error) {
	return r.values[key], nil
}

func (r stubResolver) ResolvePrefix(_ context.Context, _ string, _ sharedconfig.Scope) (map[string]string, error) {
	return nil, nil
}

func TestBuildRuntimeSnapshotUsesResolverAndProviderLoader(t *testing.T) {
	memoryBaseURL := "http://memory.internal"
	memoryKey := "memory-key"
	sandboxBaseURL := "http://sandbox.internal/"

	t.Setenv("ARKLOOP_SANDBOX_AUTH_TOKEN", "sandbox-token")
	t.Setenv("ARKLOOP_SANDBOX_BASE_URL", "")
	t.Setenv("ARKLOOP_OPENVIKING_BASE_URL", "")
	t.Setenv("ARKLOOP_OPENVIKING_ROOT_API_KEY", "")

	snapshot, err := BuildRuntimeSnapshot(context.Background(), SnapshotInput{
		ConfigResolver:         stubResolver{values: map[string]string{"browser.enabled": "true"}},
		HasConversationSearch:  true,
		ArtifactStoreAvailable: true,
		LoadPlatformProviders: func(context.Context) ([]ProviderConfig, error) {
			return []ProviderConfig{
				{GroupName: "sandbox", ProviderName: "sandbox.docker", BaseURL: &sandboxBaseURL},
				{GroupName: "memory", ProviderName: "memory.openviking", BaseURL: &memoryBaseURL, APIKeyValue: &memoryKey},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("BuildRuntimeSnapshot returned error: %v", err)
	}
	if !snapshot.BrowserEnabled {
		t.Fatal("expected browser enabled")
	}
	if snapshot.SandboxBaseURL != "http://sandbox.internal" {
		t.Fatalf("unexpected sandbox base url: %q", snapshot.SandboxBaseURL)
	}
	if snapshot.SandboxAuthToken != "sandbox-token" {
		t.Fatalf("unexpected sandbox auth token: %q", snapshot.SandboxAuthToken)
	}
	if snapshot.MemoryBaseURL != memoryBaseURL {
		t.Fatalf("unexpected memory base url: %q", snapshot.MemoryBaseURL)
	}
	if snapshot.MemoryRootAPIKey != memoryKey {
		t.Fatalf("unexpected memory key: %q", snapshot.MemoryRootAPIKey)
	}
	if !snapshot.BuiltinAvailable("browser") {
		t.Fatal("expected browser builtin to be visible")
	}
	if !snapshot.BuiltinAvailable("memory_search") {
		t.Fatal("expected memory_search builtin to be visible")
	}
}
