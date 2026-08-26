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

	t.Setenv("ARKLOOP_MEMORY_PROVIDER", "")
	t.Setenv("ARKLOOP_NOWLEDGE_BASE_URL", "")
	t.Setenv("ARKLOOP_NOWLEDGE_API_KEY", "")

	snapshot, err := BuildRuntimeSnapshot(context.Background(), SnapshotInput{
		ConfigResolver:         stubResolver{values: map[string]string{}},
		HasConversationSearch:  true,
		ArtifactStoreAvailable: true,
		LoadPlatformProviders: func(context.Context) ([]ProviderConfig, error) {
			return []ProviderConfig{
				{GroupName: "memory", ProviderName: "memory.nowledge", BaseURL: &memoryBaseURL, APIKeyValue: &memoryKey},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("BuildRuntimeSnapshot returned error: %v", err)
	}
	if snapshot.MemoryBaseURL != memoryBaseURL {
		t.Fatalf("unexpected memory base url: %q", snapshot.MemoryBaseURL)
	}
	if snapshot.MemoryAPIKey != memoryKey {
		t.Fatalf("unexpected memory key: %q", snapshot.MemoryAPIKey)
	}
	if !snapshot.BuiltinAvailable("exec_command") {
		t.Fatal("expected exec_command builtin to be visible")
	}
	if !snapshot.BuiltinAvailable("memory_search") {
		t.Fatal("expected memory_search builtin to be visible")
	}
}

func TestBuildRuntimeSnapshotUsesNowledgeResolverConfig(t *testing.T) {
	t.Setenv("ARKLOOP_MEMORY_PROVIDER", "")
	t.Setenv("ARKLOOP_NOWLEDGE_BASE_URL", "")
	t.Setenv("ARKLOOP_NOWLEDGE_API_KEY", "")
	t.Setenv("ARKLOOP_NOWLEDGE_REQUEST_TIMEOUT_MS", "")

	snapshot, err := BuildRuntimeSnapshot(context.Background(), SnapshotInput{
		ConfigResolver: stubResolver{values: map[string]string{
			"nowledge.base_url":           "http://nowledge.internal",
			"nowledge.api_key":            "nowledge-key",
			"nowledge.request_timeout_ms": "41000",
		}},
	})
	if err != nil {
		t.Fatalf("BuildRuntimeSnapshot returned error: %v", err)
	}
	if snapshot.MemoryProvider != "nowledge" {
		t.Fatalf("unexpected memory provider: %q", snapshot.MemoryProvider)
	}
	if snapshot.MemoryBaseURL != "http://nowledge.internal" {
		t.Fatalf("unexpected memory base url: %q", snapshot.MemoryBaseURL)
	}
	if snapshot.MemoryAPIKey != "nowledge-key" {
		t.Fatalf("unexpected memory api key: %q", snapshot.MemoryAPIKey)
	}
	if snapshot.MemoryRequestTimeoutMs != 41000 {
		t.Fatalf("unexpected timeout: %d", snapshot.MemoryRequestTimeoutMs)
	}
	if !snapshot.BuiltinAvailable("memory_search") {
		t.Fatal("expected memory_search builtin to be visible")
	}
}
