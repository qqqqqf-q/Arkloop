package acp

import (
	"fmt"
	"strings"

	sharedtoolruntime "arkloop/services/shared/toolruntime"
)

type AuthStrategy string

const (
	AuthStrategyProviderNative AuthStrategy = "provider_native"
	AuthStrategyArkloopProxy   AuthStrategy = "arkloop_proxy"

	DefaultProviderID = "acp.opencode"
	ProviderGroupACP  = "acp"
)

const (
	CapabilityPrompt        = "prompt"
	CapabilityStreamUpdates = "stream_updates"
	CapabilityCancel        = "cancel"
	CapabilitySessionReuse  = "session_reuse"
)

type ResolvedProvider struct {
	ID           string
	Command      string
	Args         []string
	HostKind     HostKind
	AuthStrategy AuthStrategy
	Capabilities []string
	EnvTemplate  map[string]string
}

type ResolvedInvocation struct {
	Provider ResolvedProvider
	Cwd      string
	Env      map[string]string
}

var builtinProviders = map[string]ResolvedProvider{
	DefaultProviderID: {
		ID:           DefaultProviderID,
		Command:      "opencode",
		Args:         []string{"acp"},
		Capabilities: []string{CapabilityPrompt, CapabilityStreamUpdates, CapabilityCancel, CapabilitySessionReuse},
		EnvTemplate:  map[string]string{},
	},
}

func ResolveProviderInvocation(
	requestedProvider string,
	activeConfigs map[string]sharedtoolruntime.ProviderConfig,
	snapshot *sharedtoolruntime.RuntimeSnapshot,
	workDir string,
) (ResolvedInvocation, error) {
	activeCfg, hasActive := activeConfigs[ProviderGroupACP]
	providerID := strings.TrimSpace(requestedProvider)
	if providerID == "" && hasActive {
		providerID = strings.TrimSpace(activeCfg.ProviderName)
	}
	if providerID == "" {
		providerID = DefaultProviderID
	}

	provider, ok := builtinProviders[providerID]
	if !ok {
		return ResolvedInvocation{}, fmt.Errorf("unknown provider: %s", providerID)
	}

	useActiveConfig := hasActive && strings.EqualFold(strings.TrimSpace(activeCfg.ProviderName), providerID)
	if useActiveConfig {
		if hostKind := parseHostKind(activeCfg.ConfigJSON); hostKind != "" {
			provider.HostKind = hostKind
		}
		if strategy := parseAuthStrategy(activeCfg.ConfigJSON); strategy != "" {
			provider.AuthStrategy = strategy
		}
	}

	if provider.HostKind == "" {
		provider.HostKind = defaultHostKind(snapshot)
	}
	if provider.HostKind == "" {
		return ResolvedInvocation{}, fmt.Errorf("no ACP host available")
	}

	if provider.AuthStrategy == "" {
		provider.AuthStrategy = defaultAuthStrategy(provider.HostKind, snapshot)
	}

	invocation := ResolvedInvocation{
		Provider: provider,
		Cwd:      defaultCwd(provider.HostKind, workDir),
		Env:      copyStringMap(provider.EnvTemplate),
	}

	if useActiveConfig {
		if cwd := parseCwd(activeCfg.ConfigJSON); cwd != "" {
			invocation.Cwd = cwd
		}
		for k, v := range parseEnvOverrides(activeCfg.ConfigJSON) {
			invocation.Env[k] = v
		}
	}

	return invocation, nil
}

func defaultHostKind(snapshot *sharedtoolruntime.RuntimeSnapshot) HostKind {
	if snapshot == nil {
		return ""
	}
	switch HostKind(strings.TrimSpace(snapshot.ACPHostKind)) {
	case HostKindLocal, HostKindSandbox:
		return HostKind(strings.TrimSpace(snapshot.ACPHostKind))
	default:
		if strings.TrimSpace(snapshot.SandboxBaseURL) != "" {
			return HostKindSandbox
		}
		return ""
	}
}

func defaultAuthStrategy(hostKind HostKind, snapshot *sharedtoolruntime.RuntimeSnapshot) AuthStrategy {
	if hostKind == HostKindLocal {
		return AuthStrategyProviderNative
	}
	if snapshot != nil {
		sandboxURL := strings.ToLower(strings.TrimSpace(snapshot.SandboxBaseURL))
		if strings.Contains(sandboxURL, "localhost") || strings.Contains(sandboxURL, "127.0.0.1") {
			return AuthStrategyProviderNative
		}
	}
	return AuthStrategyArkloopProxy
}

func defaultCwd(hostKind HostKind, workDir string) string {
	if trimmed := strings.TrimSpace(workDir); trimmed != "" {
		return trimmed
	}
	if hostKind == HostKindLocal {
		return "."
	}
	return "/workspace"
}

func parseHostKind(config map[string]any) HostKind {
	if len(config) == 0 {
		return ""
	}
	value, _ := config["host_kind"].(string)
	switch strings.TrimSpace(strings.ToLower(value)) {
	case string(HostKindLocal):
		return HostKindLocal
	case string(HostKindSandbox):
		return HostKindSandbox
	default:
		return ""
	}
}

func parseAuthStrategy(config map[string]any) AuthStrategy {
	if len(config) == 0 {
		return ""
	}
	value, _ := config["auth_strategy"].(string)
	switch strings.TrimSpace(strings.ToLower(value)) {
	case string(AuthStrategyProviderNative):
		return AuthStrategyProviderNative
	case string(AuthStrategyArkloopProxy):
		return AuthStrategyArkloopProxy
	default:
		return ""
	}
}

func parseCwd(config map[string]any) string {
	value, _ := config["cwd"].(string)
	return strings.TrimSpace(value)
}

func parseEnvOverrides(config map[string]any) map[string]string {
	raw, ok := config["env_overrides"]
	if !ok {
		return nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		if key := strings.TrimSpace(k); key != "" {
			if value, ok := v.(string); ok {
				out[key] = value
			}
		}
	}
	return out
}

func copyStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
