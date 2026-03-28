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
	ConfigJSON   map[string]any
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
	ACPHostKind       string
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
	ACPHostKind      string
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
		ACPHostKind:         availability.ACPHostKind,
		MemoryBaseURL:       availability.MemoryBaseURL,
		MemoryRootAPIKey:    availability.MemoryRootAPIKey,
		PlatformProviders:   copyProviders(providers),
		builtinAvailability: availability,
	}, nil
}

// MergeBuiltinToolNamesFrom 合并 s 与 other 的「托管 builtin 工具名」集合。
// Desktop 手写 Snapshot 只带 Sandbox/ACP；需与 BuildRuntimeSnapshot 产物合并后，
// filterAllowlistByRuntime 才能依据环境识别 web_search / web_fetch 等。
func (s RuntimeSnapshot) MergeBuiltinToolNamesFrom(other RuntimeSnapshot) RuntimeSnapshot {
	left := s.BuiltinToolNameSet()
	right := other.BuiltinToolNameSet()
	union := make(map[string]struct{}, len(left)+len(right))
	for k := range left {
		union[k] = struct{}{}
	}
	for k := range right {
		union[k] = struct{}{}
	}
	names := make([]string, 0, len(union))
	for n := range union {
		names = append(names, n)
	}
	sort.Strings(names)
	out := s
	out.builtinAvailability = BuiltinAvailability{toolNames: names}
	return out
}

// WithMergedBuiltinToolNames unions extra tool names into snapshot builtin availability.
// Desktop uses this when memory executors are bound but BuildRuntimeSnapshot would omit memory_* (e.g. local SQLite).
func (s RuntimeSnapshot) WithMergedBuiltinToolNames(extra ...string) RuntimeSnapshot {
	set := s.BuiltinToolNameSet()
	for _, n := range extra {
		n = strings.TrimSpace(n)
		if n != "" {
			set[n] = struct{}{}
		}
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	out := s
	out.builtinAvailability = BuiltinAvailability{toolNames: names}
	return out
}

func ResolveBuiltin(input ResolveInput) BuiltinAvailability {
	available := map[string]struct{}{
		"visualize_read_me":   {},
		"artifact_guidelines": {},
		"edit":                {},
		"close_agent":         {},
		"glob":                {},
		"interrupt_agent":     {},
		"grep":                {},
		"read":                {},
		"resume_agent":        {},
		"send_input":          {},
		"show_widget":         {},
		"timeline_title":      {},
		"spawn_agent":         {},
		"summarize_thread":    {},
		"ask_user":            {},
		"wait_agent":          {},
		"write_file":          {},
	}

	if findProvider(input.PlatformProviders, "web_search") != nil {
		available["web_search"] = struct{}{}
	}
	if findProvider(input.PlatformProviders, "web_fetch") != nil {
		available["web_fetch"] = struct{}{}
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

	acpHostKind := resolveACPHostKind(sandboxBaseURL, input.PlatformProviders)
	if acpHostKind != "" {
		available["acp_agent"] = struct{}{}
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
	if memoryBaseURL != "" {
		for _, name := range []string{"memory_search", "memory_read", "memory_write", "memory_forget"} {
			available[name] = struct{}{}
		}
	}

	if input.ArtifactStoreAvailable {
		available["create_artifact"] = struct{}{}
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
		ACPHostKind:      acpHostKind,
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
	name := strings.TrimSpace(toolName)
	_, ok := s.BuiltinToolNameSet()[name]
	if ok {
		return true
	}
	switch name {
	case "acp_agent":
		return strings.TrimSpace(s.ACPHostKind) != ""
	case "exec_command", "write_stdin":
		return strings.TrimSpace(s.SandboxBaseURL) != "" || strings.TrimSpace(s.ACPHostKind) == "local"
	default:
		return false
	}
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
	for i := range out {
		out[i].ConfigJSON = copyJSONMap(src[i].ConfigJSON)
	}
	return out
}

func copyJSONMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func findProvider(providers []ProviderConfig, groupName string) *ProviderConfig {
	for i := range providers {
		if strings.TrimSpace(providers[i].GroupName) != groupName {
			continue
		}
		if groupName == "image_understanding" {
			if providers[i].APIKeyValue == nil || strings.TrimSpace(*providers[i].APIKeyValue) == "" {
				continue
			}
		}
		return &providers[i]
	}
	return nil
}

func resolveACPHostKind(sandboxBaseURL string, providers []ProviderConfig) string {
	if provider := findProvider(providers, "acp"); provider != nil {
		if hostKind := normalizedACPHostKind(provider.ConfigJSON); hostKind != "" {
			return hostKind
		}
	}
	if sandboxBaseURL != "" {
		return "sandbox"
	}
	return ""
}

func normalizedACPHostKind(config map[string]any) string {
	if len(config) == 0 {
		return ""
	}
	value, _ := config["host_kind"].(string)
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "local", "sandbox":
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return ""
	}
}

func normalizeBaseURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}
