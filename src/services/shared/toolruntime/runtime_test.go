package toolruntime

import (
	"context"
	"reflect"
	"testing"
)

func TestResolveBuiltinArtifactToolsReflectStorageAvailability(t *testing.T) {
	resolved := ResolveBuiltin(ResolveInput{})
	if _, ok := resolved.ToolNameSet()["visualize_read_me"]; !ok {
		t.Fatal("visualize_read_me should be present without artifact store")
	}
	if _, ok := resolved.ToolNameSet()["artifact_guidelines"]; !ok {
		t.Fatal("artifact_guidelines should be present without artifact store")
	}
	if _, ok := resolved.ToolNameSet()["show_widget"]; !ok {
		t.Fatal("show_widget should be present without artifact store")
	}
	if _, ok := resolved.ToolNameSet()["create_artifact"]; ok {
		t.Fatal("create_artifact should be absent without artifact store")
	}
	if _, ok := resolved.ToolNameSet()["document_write"]; ok {
		t.Fatal("document_write should be absent without artifact store")
	}

	resolved = ResolveBuiltin(ResolveInput{ArtifactStoreAvailable: true})
	if _, ok := resolved.ToolNameSet()["create_artifact"]; !ok {
		t.Fatal("create_artifact should be present with artifact store")
	}
	if _, ok := resolved.ToolNameSet()["document_write"]; !ok {
		t.Fatal("document_write should be present with artifact store")
	}
}

func TestRuntimeSnapshotWithMergedBuiltinToolNames(t *testing.T) {
	snap := RuntimeSnapshot{}
	snap.builtinAvailability = BuiltinAvailability{toolNames: []string{"grep"}}
	merged := snap.WithMergedBuiltinToolNames("memory_search", "memory_read", "")
	if !merged.BuiltinAvailable("grep") {
		t.Fatal("expected grep preserved")
	}
	if !merged.BuiltinAvailable("memory_search") || !merged.BuiltinAvailable("memory_read") {
		t.Fatalf("unexpected set: %v", merged.BuiltinToolNames())
	}
}

func TestResolveBuiltinMemoryToolsWithURLOnly(t *testing.T) {
	resolved := ResolveBuiltin(ResolveInput{
		Env: EnvConfig{MemoryBaseURL: "http://memory.internal"},
	})
	if resolved.MemoryBaseURL != "http://memory.internal" {
		t.Fatalf("unexpected memory base url: %q", resolved.MemoryBaseURL)
	}
	if resolved.MemoryRootAPIKey != "" {
		t.Fatalf("expected empty key, got %q", resolved.MemoryRootAPIKey)
	}
	if _, ok := resolved.ToolNameSet()["memory_search"]; !ok {
		t.Fatal("memory_search should be available with URL only")
	}
}

func TestResolveBuiltinUsesEnvAndProviders(t *testing.T) {
	memoryBaseURL := " http://memory.internal "
	memoryAPIKey := " provider-key "
	sandboxBaseURL := " http://sandbox.internal/ "
	resolved := ResolveBuiltin(ResolveInput{
		HasConversationSearch:  true,
		ArtifactStoreAvailable: true,
		BrowserEnabled:         true,
		Env: EnvConfig{
			MemoryBaseURL: memoryBaseURL,
		},
		PlatformProviders: []ProviderConfig{
			{GroupName: "web_search", ProviderName: "web_search.searxng", BaseURL: strPtr("http://searxng:8080")},
			{GroupName: "web_fetch", ProviderName: "web_fetch.basic"},
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
		"acp_agent",
		"artifact_guidelines",
		"ask_user",
		"browser",
		"close_agent",
		"conversation_search",
		"create_artifact",
		"document_write",
		"edit",
		"exec_command",
		"glob",
		"grep",
		"interrupt_agent",
		"memory_forget",
		"memory_read",
		"memory_search",
		"memory_write",
		"python_execute",
		"read_file",
		"resume_agent",
		"send_input",
		"show_widget",
		"spawn_agent",
		"summarize_thread",
		"timeline_title",
		"visualize_read_me",
		"wait_agent",
		"web_fetch",
		"web_search",
		"write_file",
		"write_stdin",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected tool names: got %v want %v", got, want)
	}
}

func TestResolveBuiltinHidesBrowserWhenDisabled(t *testing.T) {
	sandboxBaseURL := "http://sandbox.internal"
	resolved := ResolveBuiltin(ResolveInput{
		Env: EnvConfig{SandboxBaseURL: sandboxBaseURL},
	})
	if _, ok := resolved.ToolNameSet()["browser"]; ok {
		t.Fatal("browser should be absent when BrowserEnabled=false")
	}
}

func TestResolveBuiltinHidesWebToolsWhenNotConfigured(t *testing.T) {
	resolved := ResolveBuiltin(ResolveInput{})
	if _, ok := resolved.ToolNameSet()["web_search"]; ok {
		t.Fatal("web_search should be absent without configuration")
	}
	if _, ok := resolved.ToolNameSet()["web_fetch"]; ok {
		t.Fatal("web_fetch should be absent without configuration")
	}
}

func TestResolveBuiltinAddsWebToolsFromPlatformProviders(t *testing.T) {
	resolved := ResolveBuiltin(ResolveInput{
		PlatformProviders: []ProviderConfig{
			{GroupName: "web_search", ProviderName: "web_search.searxng"},
			{GroupName: "web_fetch", ProviderName: "web_fetch.jina"},
			{
				GroupName:    "image_understanding",
				ProviderName: "image_understanding.minimax",
				APIKeyValue:  strPtr("api-key"),
			},
		},
	})
	if _, ok := resolved.ToolNameSet()["web_search"]; !ok {
		t.Fatal("web_search should be present with platform provider")
	}
	if _, ok := resolved.ToolNameSet()["web_fetch"]; !ok {
		t.Fatal("web_fetch should be present with platform provider")
	}
	if _, ok := resolved.ToolNameSet()["understand_image"]; !ok {
		t.Fatal("understand_image should be present with platform provider")
	}
}

func TestResolveBuiltinDoesNotAddWebToolsFromEnv(t *testing.T) {
	resolved := ResolveBuiltin(ResolveInput{
		Env: EnvConfig{
			MemoryBaseURL: "http://memory.internal",
		},
	})
	if _, ok := resolved.ToolNameSet()["web_search"]; ok {
		t.Fatal("web_search should not be present from env only")
	}
	if _, ok := resolved.ToolNameSet()["web_fetch"]; ok {
		t.Fatal("web_fetch should be absent without configuration")
	}
}

func TestRuntimeSnapshotMergeBuiltinToolNamesFromPreservesStubAndAddsBuiltins(t *testing.T) {
	envLayer, err := BuildRuntimeSnapshot(context.Background(), SnapshotInput{})
	if err != nil {
		t.Fatalf("BuildRuntimeSnapshot: %v", err)
	}
	stub := RuntimeSnapshot{ACPHostKind: "local"}
	merged := stub.MergeBuiltinToolNamesFrom(envLayer)
	if merged.ACPHostKind != "local" {
		t.Fatalf("lost stub ACPHostKind, got %q", merged.ACPHostKind)
	}
	if !merged.BuiltinAvailable("grep") {
		t.Fatal("expected grep from env merge (static filesystem tools)")
	}
}

func TestResolveBuiltinWebFetchJinaRequiresProviderConfig(t *testing.T) {
	resolved := ResolveBuiltin(ResolveInput{
		PlatformProviders: []ProviderConfig{
			{GroupName: "web_fetch", ProviderName: "web_fetch.jina", APIKeyValue: strPtr("jina-key")},
		},
	})
	if _, ok := resolved.ToolNameSet()["web_fetch"]; !ok {
		t.Fatal("web_fetch should be present when jina provider is configured")
	}
}

func TestResolveBuiltinAddsACPFromProviderConfig(t *testing.T) {
	resolved := ResolveBuiltin(ResolveInput{
		PlatformProviders: []ProviderConfig{
			{
				GroupName:    "acp",
				ProviderName: "acp.opencode",
				ConfigJSON: map[string]any{
					"host_kind": "local",
				},
			},
		},
	})
	if _, ok := resolved.ToolNameSet()["acp_agent"]; !ok {
		t.Fatal("acp_agent should be present when ACP provider config resolves a host")
	}
	if resolved.ACPHostKind != "local" {
		t.Fatalf("unexpected ACP host kind: %q", resolved.ACPHostKind)
	}
}

func TestResolveBuiltinSkipsImageUnderstandingWithoutAPIKey(t *testing.T) {
	resolved := ResolveBuiltin(ResolveInput{
		PlatformProviders: []ProviderConfig{
			{
				GroupName:    "image_understanding",
				ProviderName: "image_understanding.minimax",
			},
		},
	})
	if _, ok := resolved.ToolNameSet()["understand_image"]; ok {
		t.Fatal("understand_image should be absent without API key")
	}

	apiKey := "key"
	resolved = ResolveBuiltin(ResolveInput{
		PlatformProviders: []ProviderConfig{
			{
				GroupName:    "image_understanding",
				ProviderName: "image_understanding.minimax",
				APIKeyValue:  &apiKey,
			},
		},
	})
	if _, ok := resolved.ToolNameSet()["understand_image"]; !ok {
		t.Fatal("understand_image should be present when image_understanding provider has API key")
	}
}

func strPtr(value string) *string {
	return &value
}
