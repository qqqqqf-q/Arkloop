package toolruntime

import (
	"context"
	"os"
	"sort"
	"strconv"
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
	SandboxBaseURL         string
	MemoryProvider         string
	MemoryBaseURL          string
	MemoryAPIKey           string
	MemoryRootAPIKey       string
	MemoryRequestTimeoutMs int
}

type ResolveInput struct {
	HasConversationSearch  bool
	HasGroupHistorySearch  bool
	ArtifactStoreAvailable bool
	BrowserEnabled         bool
	Env                    EnvConfig
	PlatformProviders      []ProviderConfig
}

type RuntimeSnapshot struct {
	BrowserEnabled         bool
	SandboxBaseURL         string
	SandboxAuthToken       string
	ACPHostKind            string
	DesktopExecutionMode   string
	MemoryProvider         string
	MemoryBaseURL          string
	MemoryAPIKey           string
	MemoryRootAPIKey       string
	MemoryRequestTimeoutMs int
	PlatformProviders      []ProviderConfig

	builtinAvailability BuiltinAvailability
}

type SnapshotInput struct {
	ConfigResolver          sharedconfig.Resolver
	LoadPlatformProviders   func(context.Context) ([]ProviderConfig, error)
	HasConversationSearch   bool
	HasGroupHistorySearch   bool
	ArtifactStoreAvailable  bool
	SandboxAuthTokenEnvName string
}

type BuiltinAvailability struct {
	toolNames              []string
	SandboxBaseURL         string
	ACPHostKind            string
	MemoryProvider         string
	MemoryBaseURL          string
	MemoryAPIKey           string
	MemoryRootAPIKey       string
	MemoryRequestTimeoutMs int
	DocumentWrite          bool
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
	memoryEnv := resolveMemoryEnvConfig(ctx, input.ConfigResolver)

	availability := ResolveBuiltin(ResolveInput{
		HasConversationSearch:  input.HasConversationSearch,
		HasGroupHistorySearch:  input.HasGroupHistorySearch,
		ArtifactStoreAvailable: input.ArtifactStoreAvailable,
		BrowserEnabled:         browserEnabled,
		Env:                    memoryEnv,
		PlatformProviders:      providers,
	})
	availability = resolveMemoryFromConfig(ctx, input.ConfigResolver, availability)

	authTokenEnvName := strings.TrimSpace(input.SandboxAuthTokenEnvName)
	if authTokenEnvName == "" {
		authTokenEnvName = defaultSandboxAuthTokenEnv
	}

	return RuntimeSnapshot{
		BrowserEnabled:         browserEnabled,
		SandboxBaseURL:         availability.SandboxBaseURL,
		SandboxAuthToken:       strings.TrimSpace(os.Getenv(authTokenEnvName)),
		ACPHostKind:            availability.ACPHostKind,
		MemoryProvider:         availability.MemoryProvider,
		MemoryBaseURL:          availability.MemoryBaseURL,
		MemoryAPIKey:           availability.MemoryAPIKey,
		MemoryRootAPIKey:       availability.MemoryRootAPIKey,
		MemoryRequestTimeoutMs: availability.MemoryRequestTimeoutMs,
		PlatformProviders:      copyProviders(providers),
		builtinAvailability:    availability,
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
		"arkloop_help":        {},
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
	if input.HasGroupHistorySearch {
		available["group_history_search"] = struct{}{}
	}

	sandboxBaseURL := normalizeBaseURL(input.Env.SandboxBaseURL)
	if sandboxBaseURL == "" {
		if provider := findProvider(input.PlatformProviders, "sandbox"); provider != nil && provider.BaseURL != nil {
			sandboxBaseURL = normalizeBaseURL(*provider.BaseURL)
		}
	}
	if sandboxBaseURL != "" {
		for _, name := range []string{"python_execute", "exec_command", "continue_process", "terminate_process", "resize_process"} {
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

	memoryProvider := normalizeMemoryProvider(strings.TrimSpace(input.Env.MemoryProvider))
	memoryBaseURL := strings.TrimSpace(input.Env.MemoryBaseURL)
	memoryAPIKey := strings.TrimSpace(input.Env.MemoryAPIKey)
	memoryRootAPIKey := strings.TrimSpace(input.Env.MemoryRootAPIKey)
	memoryRequestTimeoutMs := input.Env.MemoryRequestTimeoutMs
	if memoryProvider == "" && memoryBaseURL != "" {
		memoryProvider = "openviking"
	}
	if memoryProvider == "" || memoryBaseURL == "" || memoryAPIKey == "" || memoryRootAPIKey == "" {
		if provider := findProvider(input.PlatformProviders, "memory"); provider != nil {
			if memoryProvider == "" {
				memoryProvider = normalizeMemoryProvider(strings.TrimSpace(provider.ProviderName))
			}
			if memoryBaseURL == "" && provider.BaseURL != nil {
				memoryBaseURL = strings.TrimSpace(*provider.BaseURL)
			}
			if provider.APIKeyValue != nil {
				switch memoryProvider {
				case "nowledge":
					if memoryAPIKey == "" {
						memoryAPIKey = strings.TrimSpace(*provider.APIKeyValue)
					}
				default:
					if memoryRootAPIKey == "" {
						memoryRootAPIKey = strings.TrimSpace(*provider.APIKeyValue)
					}
				}
			}
		}
	}
	if memoryProvider == "nowledge" && memoryBaseURL != "" {
		for _, name := range []string{"memory_search", "memory_read", "memory_write", "memory_forget", "memory_thread_search", "memory_thread_fetch"} {
			available[name] = struct{}{}
		}
	} else if memoryBaseURL != "" {
		for _, name := range []string{"memory_search", "memory_read", "memory_write", "memory_edit", "memory_forget"} {
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
		toolNames:              names,
		SandboxBaseURL:         sandboxBaseURL,
		ACPHostKind:            acpHostKind,
		MemoryProvider:         memoryProvider,
		MemoryBaseURL:          memoryBaseURL,
		MemoryAPIKey:           memoryAPIKey,
		MemoryRootAPIKey:       memoryRootAPIKey,
		MemoryRequestTimeoutMs: memoryRequestTimeoutMs,
		DocumentWrite:          input.ArtifactStoreAvailable,
	}
}

func resolveMemoryFromConfig(ctx context.Context, resolver sharedconfig.Resolver, availability BuiltinAvailability) BuiltinAvailability {
	if resolver == nil {
		return availability
	}
	if strings.TrimSpace(availability.MemoryProvider) == "" {
		if baseURL, err := resolver.Resolve(ctx, "nowledge.base_url", sharedconfig.Scope{}); err == nil && strings.TrimSpace(baseURL) != "" {
			availability.MemoryProvider = "nowledge"
			availability.MemoryBaseURL = strings.TrimSpace(baseURL)
			if apiKey, apiErr := resolver.Resolve(ctx, "nowledge.api_key", sharedconfig.Scope{}); apiErr == nil {
				availability.MemoryAPIKey = strings.TrimSpace(apiKey)
			}
			if timeout, timeoutErr := resolver.Resolve(ctx, "nowledge.request_timeout_ms", sharedconfig.Scope{}); timeoutErr == nil {
				availability.MemoryRequestTimeoutMs = parsePositiveInt(timeout)
			}
		}
	}
	if strings.TrimSpace(availability.MemoryProvider) == "" {
		if baseURL, err := resolver.Resolve(ctx, "openviking.base_url", sharedconfig.Scope{}); err == nil && strings.TrimSpace(baseURL) != "" {
			availability.MemoryProvider = "openviking"
			availability.MemoryBaseURL = strings.TrimSpace(baseURL)
			if apiKey, apiErr := resolver.Resolve(ctx, "openviking.root_api_key", sharedconfig.Scope{}); apiErr == nil {
				availability.MemoryRootAPIKey = strings.TrimSpace(apiKey)
			}
		}
	}
	if availability.MemoryBaseURL == "" {
		return availability
	}
	switch availability.MemoryProvider {
	case "nowledge":
		availability.toolNames = appendToolNames(availability.toolNames, "memory_search", "memory_read", "memory_write", "memory_forget", "memory_thread_search", "memory_thread_fetch")
	default:
		availability.toolNames = appendToolNames(availability.toolNames, "memory_search", "memory_read", "memory_write", "memory_edit", "memory_forget")
	}
	return availability
}

func appendToolNames(existing []string, extra ...string) []string {
	set := make(map[string]struct{}, len(existing)+len(extra))
	for _, name := range existing {
		set[name] = struct{}{}
	}
	for _, name := range extra {
		set[name] = struct{}{}
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func resolveMemoryEnvConfig(ctx context.Context, resolver sharedconfig.Resolver) EnvConfig {
	cfg := EnvConfig{
		SandboxBaseURL:         strings.TrimSpace(os.Getenv("ARKLOOP_SANDBOX_BASE_URL")),
		MemoryProvider:         normalizeMemoryProvider(strings.TrimSpace(os.Getenv("ARKLOOP_MEMORY_PROVIDER"))),
		MemoryRequestTimeoutMs: parsePositiveInt(os.Getenv("ARKLOOP_NOWLEDGE_REQUEST_TIMEOUT_MS")),
	}

	nowledgeBaseURL := strings.TrimSpace(os.Getenv("ARKLOOP_NOWLEDGE_BASE_URL"))
	nowledgeAPIKey := strings.TrimSpace(os.Getenv("ARKLOOP_NOWLEDGE_API_KEY"))
	openvikingBaseURL := strings.TrimSpace(os.Getenv("ARKLOOP_OPENVIKING_BASE_URL"))
	openvikingRootAPIKey := strings.TrimSpace(os.Getenv("ARKLOOP_OPENVIKING_ROOT_API_KEY"))

	if resolver != nil {
		if nowledgeBaseURL == "" {
			nowledgeBaseURL = resolveOptionalConfigString(ctx, resolver, "nowledge.base_url")
		}
		if nowledgeAPIKey == "" {
			nowledgeAPIKey = resolveOptionalConfigString(ctx, resolver, "nowledge.api_key")
		}
		if cfg.MemoryRequestTimeoutMs <= 0 {
			cfg.MemoryRequestTimeoutMs = resolveOptionalConfigInt(ctx, resolver, "nowledge.request_timeout_ms")
		}
		if openvikingBaseURL == "" {
			openvikingBaseURL = resolveOptionalConfigString(ctx, resolver, "openviking.base_url")
		}
		if openvikingRootAPIKey == "" {
			openvikingRootAPIKey = resolveOptionalConfigString(ctx, resolver, "openviking.root_api_key")
		}
	}

	switch cfg.MemoryProvider {
	case "nowledge":
		cfg.MemoryBaseURL = nowledgeBaseURL
		cfg.MemoryAPIKey = nowledgeAPIKey
	case "openviking":
		cfg.MemoryBaseURL = openvikingBaseURL
		cfg.MemoryRootAPIKey = openvikingRootAPIKey
	default:
		switch {
		case nowledgeBaseURL != "":
			cfg.MemoryProvider = "nowledge"
			cfg.MemoryBaseURL = nowledgeBaseURL
			cfg.MemoryAPIKey = nowledgeAPIKey
		case openvikingBaseURL != "":
			cfg.MemoryProvider = "openviking"
			cfg.MemoryBaseURL = openvikingBaseURL
			cfg.MemoryRootAPIKey = openvikingRootAPIKey
		}
	}

	return cfg
}

func normalizeMemoryProvider(raw string) string {
	value := strings.TrimSpace(strings.ToLower(raw))
	switch strings.TrimPrefix(value, "memory.") {
	case "nowledge":
		return "nowledge"
	case "openviking":
		return "openviking"
	default:
		return ""
	}
}

func parsePositiveInt(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

func resolveOptionalConfigString(ctx context.Context, resolver sharedconfig.Resolver, key string) string {
	if resolver == nil {
		return ""
	}
	value, err := resolver.Resolve(ctx, key, sharedconfig.Scope{})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func resolveOptionalConfigInt(ctx context.Context, resolver sharedconfig.Resolver, key string) int {
	return parsePositiveInt(resolveOptionalConfigString(ctx, resolver, key))
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
	case "exec_command", "continue_process", "terminate_process", "resize_process":
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
		if groupName == "read" {
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

func SandboxAvailableFromEnv() bool {
	return normalizeBaseURL(os.Getenv("ARKLOOP_SANDBOX_BASE_URL")) != ""
}

func MemoryAvailableFromEnv() bool {
	return strings.TrimSpace(os.Getenv("ARKLOOP_OPENVIKING_BASE_URL")) != ""
}
