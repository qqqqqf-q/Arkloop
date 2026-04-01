package catalogapi

import (
	"context"

	"arkloop/services/api/internal/data"
	sharedtoolruntime "arkloop/services/shared/toolruntime"

	"github.com/google/uuid"
)

const (
	toolProviderRuntimeStatusAvailable   = "available"
	toolProviderRuntimeStatusUnavailable = "unavailable"

	toolProviderRuntimeSourceNone           = "none"
	toolProviderRuntimeSourceProviderConfig = "provider_config"
	toolProviderRuntimeSourceEnv            = "env"
	toolProviderRuntimeSourceSandbox        = "sandbox"
	toolProviderRuntimeSourceLocal          = "local"
)

type toolProviderRuntimeStatus struct {
	Status string
	Source string
}

func buildToolProviderRuntimeStatusMap(
	ctx context.Context,
	pool data.DB,
	ownerKind string,
	ownerUserID *uuid.UUID,
) map[string]toolProviderRuntimeStatus {
	snapshot, err := buildEffectiveRuntimeSnapshotForScope(ctx, pool, ownerKind, ownerUserID, effectiveCatalogPoolReady(pool))
	available := map[string]struct{}{}
	if err == nil {
		available = snapshot.BuiltinToolNameSet()
	}
	statuses := make(map[string]toolProviderRuntimeStatus, len(toolProviderCatalog))
	for _, def := range toolProviderCatalog {
		statuses[def.ProviderName] = resolveToolProviderRuntimeStatus(def, snapshot, available)
	}
	return statuses
}

func resolveToolProviderRuntimeStatus(
	def toolProviderDefinition,
	snapshot sharedtoolruntime.RuntimeSnapshot,
	available map[string]struct{},
) toolProviderRuntimeStatus {
	if status, ok := resolveDesktopToolProviderRuntimeStatus(def, snapshot); ok {
		return status
	}

	selected := runtimeProviderSelected(def.ProviderName, snapshot.PlatformProviders)

	switch def.GroupName {
	case "memory":
		if selected {
			source := toolProviderRuntimeSourceProviderConfig
			if sharedtoolruntime.MemoryAvailableFromEnv() {
				source = toolProviderRuntimeSourceEnv
			}
			return toolProviderRuntimeStatus{Status: toolProviderRuntimeStatusAvailable, Source: source}
		}
	case "sandbox":
		if selected {
			source := toolProviderRuntimeSourceProviderConfig
			if sharedtoolruntime.SandboxAvailableFromEnv() {
				source = toolProviderRuntimeSourceEnv
			}
			return toolProviderRuntimeStatus{Status: toolProviderRuntimeStatusAvailable, Source: source}
		}
	case "acp":
		if selected && snapshot.BuiltinAvailable("acp_agent") {
			source := toolProviderRuntimeSourceProviderConfig
			if sharedtoolruntime.SandboxAvailableFromEnv() || desktopSandboxAvailable() {
				source = toolProviderRuntimeSourceSandbox
			} else if desktopLocalACPAvailable() {
				source = toolProviderRuntimeSourceLocal
			}
			return toolProviderRuntimeStatus{Status: toolProviderRuntimeStatusAvailable, Source: source}
		}
	default:
		if selected && runtimeGroupAvailable(def.GroupName, available) {
			return toolProviderRuntimeStatus{
				Status: toolProviderRuntimeStatusAvailable,
				Source: toolProviderRuntimeSourceProviderConfig,
			}
		}
	}
	return toolProviderRuntimeStatus{
		Status: toolProviderRuntimeStatusUnavailable,
		Source: toolProviderRuntimeSourceNone,
	}
}

func runtimeProviderSelected(providerName string, providers []sharedtoolruntime.ProviderConfig) bool {
	name := providerName
	for _, provider := range providers {
		if provider.ProviderName == name {
			return true
		}
	}
	return false
}

func runtimeGroupAvailable(groupName string, available map[string]struct{}) bool {
	switch groupName {
	case "web_search":
		_, ok := available["web_search"]
		return ok
	case "web_fetch":
		_, ok := available["web_fetch"]
		return ok
	case "read":
		_, ok := available["read"]
		return ok
	default:
		return false
	}
}
