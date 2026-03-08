package toolruntime

import (
	"reflect"
	"testing"
)

func TestResolveBuiltinIncludesDocumentWriteOnlyWhenArtifactStoreAvailable(t *testing.T) {
	resolved := ResolveBuiltin(ResolveInput{})
	if _, ok := resolved.ToolNameSet()["document_write"]; ok {
		t.Fatal("document_write should be absent without artifact store")
	}

	resolved = ResolveBuiltin(ResolveInput{ArtifactStoreAvailable: true})
	if _, ok := resolved.ToolNameSet()["document_write"]; !ok {
		t.Fatal("document_write should be present with artifact store")
	}
}

func TestResolveBuiltinUsesEnvAndProviders(t *testing.T) {
	memoryBaseURL := " http://memory.internal "
	memoryAPIKey := " provider-key "
	sandboxBaseURL := " http://sandbox.internal/ "
	resolved := ResolveBuiltin(ResolveInput{
		HasConversationSearch:  true,
		ArtifactStoreAvailable: true,
		Env: EnvConfig{
			MemoryBaseURL: memoryBaseURL,
		},
		PlatformProviders: []ProviderConfig{
			{GroupName: "memory", ProviderName: "memory.openviking", APIKeyValue: &memoryAPIKey},
			{GroupName: "sandbox", ProviderName: "sandbox.docker", BaseURL: &sandboxBaseURL},
		},
	})

	if resolved.MemoryBaseURL != "http://memory.internal" {
		t.Fatalf("unexpected memory base url: %q", resolved.MemoryBaseURL)
	}
	if resolved.MemoryRootAPIKey != "provider-key" {
		t.Fatalf("unexpected memory api key: %q", resolved.MemoryRootAPIKey)
	}
	if resolved.SandboxBaseURL != "http://sandbox.internal" {
		t.Fatalf("unexpected sandbox base url: %q", resolved.SandboxBaseURL)
	}

	got := resolved.ToolNames()
	want := []string{
		"conversation_search",
		"document_write",
		"echo",
		"exec_command",
		"memory_forget",
		"memory_read",
		"memory_search",
		"memory_write",
		"noop",
		"python_execute",
		"spawn_agent",
		"summarize_thread",
		"timeline_title",
		"web_fetch",
		"web_search",
		"write_stdin",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected tool names: got %v want %v", got, want)
	}
}
