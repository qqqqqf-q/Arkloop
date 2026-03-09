package toolruntime

import (
	"context"
	"os"
	"sort"
	"strings"

	sharedconfig "arkloop/services/shared/config"
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
	BrowserEnabled         bool
	Env                    EnvConfig
	PlatformProviders      []ProviderConfig
}

type RuntimeSnapshot struct {
	BrowserEnabled    bool
	SandboxBaseURL    string
	SandboxAuthToken  string
	MemoryBaseURL     string
	MemoryRootAPIKey  string
	PlatformProviders []ProviderConfig

	builtinAvailability BuiltinAvailability
}

type SnapshotInput struct {
	ConfigResolver          sharedconfig.Resolver
	LoadPlatformProviders   func(context.Context) ([]ProviderConfig, error)
	HasConversationSearch   bool
	ArtifactStoreAvailable  bool
	SandboxAuthTokenEnvName string
}

type BuiltinAvailability struct {
	toolNames        []string
	SandboxBaseURL   string
	MemoryBaseURL    string
	MemoryRootAPIKey string
	DocumentWrite    bool
}

const defaultSandboxAuthTokenEnv = "ARKLOOP_SANDBOX_AUTH_TOKEN"

func BuildRuntimeSnapshot(ctx context.Context, input SnapshotInput) (RuntimeSnapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	providers := []ProviderConfig{}
	if input.LoadPlatformProviders != nil {
		loaded, err := input.LoadPlatformProviders(ctx)
		if err != nil {
			return RuntimeSnapshot{}, err
		}
		providers = loaded
	}

	browserEnabled := resolveBrowserEnabled(ctx, input.ConfigResolver)
	availability := ResolveBuiltin(ResolveInput{
		HasConversationSearch:  input.HasConversationSearch,
		ArtifactStoreAvailable: input.ArtifactStoreAvailable,
		BrowserEnabled:         browserEnabled,
		Env: EnvConfig{
			SandboxBaseURL:   strings.TrimSpace(os.Getenv("ARKLOOP_SANDBOX_BASE_URL")),
			MemoryBaseURL:    strings.TrimSpace(os.Getenv("ARKLOOP_OPENVIKING_BASE_URL")),
			MemoryRootAPIKey: strings.TrimSpace(os.Getenv("ARKLOOP_OPENVIKING_ROOT_API_KEY")),
		},
		PlatformProviders: providers,
	})

	authTokenEnvName := strings.TrimSpace(input.SandboxAuthTokenEnvName)
	if authTokenEnvName == "" {
		authTokenEnvName = defaultSandboxAuthTokenEnv
	}

	return RuntimeSnapshot{
		BrowserEnabled:      browserEnabled,
		SandboxBaseURL:      availability.SandboxBaseURL,
		SandboxAuthToken:    strings.TrimSpace(os.Getenv(authTokenEnvName)),
		MemoryBaseURL:       availability.MemoryBaseURL,
		MemoryRootAPIKey:    availability.MemoryRootAPIKey,
		PlatformProviders:   copyProviders(providers),
		builtinAvailability: availability,
	}, nil
}

func ResolveBuiltin(input ResolveInput) BuiltinAvailability {
	available := map[string]struct{}{
		"web_search":       {},
		"web_fetch":        {},
		"timeline_title":   {},
		"spawn_agent":      {},
		"summarize_thread": {},
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
		if input.BrowserEnabled {
			available["browser"] = struct{}{}
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

func (s RuntimeSnapshot) BuiltinToolNames() []string {
	return s.builtinAvailability.ToolNames()
}

func (s RuntimeSnapshot) BuiltinToolNameSet() map[string]struct{} {
	return s.builtinAvailability.ToolNameSet()
}

func (s RuntimeSnapshot) BuiltinAvailable(toolName string) bool {
	_, ok := s.BuiltinToolNameSet()[strings.TrimSpace(toolName)]
	return ok
}

func resolveBrowserEnabled(ctx context.Context, resolver sharedconfig.Resolver) bool {
	if resolver == nil {
		return false
	}
	value, err := resolver.Resolve(ctx, "browser.enabled", sharedconfig.Scope{})
	if err != nil {
		return false
	}
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func copyProviders(src []ProviderConfig) []ProviderConfig {
	if len(src) == 0 {
		return nil
	}
	out := make([]ProviderConfig, len(src))
	copy(out, src)
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
