package toolruntime

import (
	"sort"
	"strings"
)

type ProviderConfig struct {
	GroupName    string
	ProviderName string
	BaseURL      *string
	APIKeyValue  *string
}

type EnvConfig struct {
	SandboxBaseURL   string
	MemoryBaseURL    string
	MemoryRootAPIKey string
}

type ResolveInput struct {
	HasConversationSearch  bool
	ArtifactStoreAvailable bool
	Env                    EnvConfig
	PlatformProviders      []ProviderConfig
}

type BuiltinAvailability struct {
	toolNames        []string
	SandboxBaseURL   string
	MemoryBaseURL    string
	MemoryRootAPIKey string
	DocumentWrite    bool
}

func ResolveBuiltin(input ResolveInput) BuiltinAvailability {
	available := map[string]struct{}{
		"web_search":       {},
		"web_fetch":        {},
		"timeline_title":   {},
		"spawn_agent":      {},
		"summarize_thread": {},
		"echo":             {},
		"noop":             {},
	}
	if input.HasConversationSearch {
		available["conversation_search"] = struct{}{}
	}

	sandboxBaseURL := normalizeBaseURL(input.Env.SandboxBaseURL)
	if sandboxBaseURL == "" {
		if provider := findProvider(input.PlatformProviders, "sandbox"); provider != nil && provider.BaseURL != nil {
			sandboxBaseURL = normalizeBaseURL(*provider.BaseURL)
		}
	}
	if sandboxBaseURL != "" {
		for _, name := range []string{"python_execute", "exec_command", "write_stdin"} {
			available[name] = struct{}{}
		}
	}

	memoryBaseURL := strings.TrimSpace(input.Env.MemoryBaseURL)
	memoryRootAPIKey := strings.TrimSpace(input.Env.MemoryRootAPIKey)
	if memoryBaseURL == "" || memoryRootAPIKey == "" {
		if provider := findProvider(input.PlatformProviders, "memory"); provider != nil {
			if memoryBaseURL == "" && provider.BaseURL != nil {
				memoryBaseURL = strings.TrimSpace(*provider.BaseURL)
			}
			if memoryRootAPIKey == "" && provider.APIKeyValue != nil {
				memoryRootAPIKey = strings.TrimSpace(*provider.APIKeyValue)
			}
		}
	}
	if memoryBaseURL != "" && memoryRootAPIKey != "" {
		for _, name := range []string{"memory_search", "memory_read", "memory_write", "memory_forget"} {
			available[name] = struct{}{}
		}
	}

	if input.ArtifactStoreAvailable {
		available["document_write"] = struct{}{}
	}

	names := make([]string, 0, len(available))
	for name := range available {
		names = append(names, name)
	}
	sort.Strings(names)

	return BuiltinAvailability{
		toolNames:        names,
		SandboxBaseURL:   sandboxBaseURL,
		MemoryBaseURL:    memoryBaseURL,
		MemoryRootAPIKey: memoryRootAPIKey,
		DocumentWrite:    input.ArtifactStoreAvailable,
	}
}

func (a BuiltinAvailability) ToolNames() []string {
	out := make([]string, len(a.toolNames))
	copy(out, a.toolNames)
	return out
}

func (a BuiltinAvailability) ToolNameSet() map[string]struct{} {
	out := make(map[string]struct{}, len(a.toolNames))
	for _, name := range a.toolNames {
		out[name] = struct{}{}
	}
	return out
}

func findProvider(providers []ProviderConfig, groupName string) *ProviderConfig {
	for i := range providers {
		if strings.TrimSpace(providers[i].GroupName) == groupName {
			return &providers[i]
		}
	}
	return nil
}

func normalizeBaseURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}
