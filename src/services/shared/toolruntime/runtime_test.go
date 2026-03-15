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
		BrowserEnabled:         true,
		Env: EnvConfig{
			MemoryBaseURL:     memoryBaseURL,
			WebSearchProvider: "searxng",
			WebSearchBaseURL:  "http://searxng:8080",
			WebFetchProvider:  "basic",
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
		"acp_agent",
		"ask_user",
		"browser",
		"close_agent",
		"conversation_search",
		"document_write",
		"exec_command",
		"interrupt_agent",
		"memory_forget",
		"memory_read",
		"memory_search",
		"memory_write",
		"python_execute",
		"resume_agent",
		"send_input",
		"spawn_agent",
		"summarize_thread",
		"timeline_title",
		"wait_agent",
		"web_fetch",
		"web_search",
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
		},
	})
	if _, ok := resolved.ToolNameSet()["web_search"]; !ok {
		t.Fatal("web_search should be present with platform provider")
	}
	if _, ok := resolved.ToolNameSet()["web_fetch"]; !ok {
		t.Fatal("web_fetch should be present with platform provider")
	}
}

func TestResolveBuiltinAddsWebToolsFromEnv(t *testing.T) {
	resolved := ResolveBuiltin(ResolveInput{
		Env: EnvConfig{
			WebSearchProvider: "tavily",
			WebSearchAPIKey:   "tvly-test-key",
		},
	})
	if _, ok := resolved.ToolNameSet()["web_search"]; !ok {
		t.Fatal("web_search should be present with env provider")
	}
	if _, ok := resolved.ToolNameSet()["web_fetch"]; ok {
		t.Fatal("web_fetch should be absent without configuration")
	}
}

func TestResolveBuiltinHidesWebToolsWhenEnvIncomplete(t *testing.T) {
	resolved := ResolveBuiltin(ResolveInput{
		Env: EnvConfig{
			WebSearchProvider: "tavily",
			WebFetchProvider:  "jina",
		},
	})
	if _, ok := resolved.ToolNameSet()["web_search"]; ok {
		t.Fatal("web_search should be absent when tavily has no API key")
	}
	if _, ok := resolved.ToolNameSet()["web_fetch"]; ok {
		t.Fatal("web_fetch should be absent when jina has no API key")
	}
}
