package catalogapi

import (
	"context"
	"strings"

	"arkloop/services/api/internal/data"
	sharedmcpinstall "arkloop/services/shared/mcpinstall"
)

func runMCPInstallCheck(ctx context.Context, item data.ProfileMCPInstall, headers map[string]string) (string, string, string) {
	server, err := effectiveServerConfigFromInstall(item, headers)
	if err != nil {
		return "protocol_error", "invalid_launch_spec", err.Error()
	}
	if err := checkEffectiveMCPHostRequirement(server, strings.TrimSpace(item.HostRequirement)); err != nil {
		return "install_missing", "host_requirement", err.Error()
	}
	tools, err := listEffectiveMCPServerTools(ctx, server)
	if err != nil {
		message := err.Error()
		switch {
		case strings.Contains(message, "401"), strings.Contains(message, "403"), strings.Contains(strings.ToLower(message), "auth"):
			return "auth_invalid", "auth_invalid", message
		case strings.Contains(strings.ToLower(message), "protocol"):
			return "protocol_error", "protocol_error", message
		default:
			return "connect_failed", "connect_failed", message
		}
	}
	if len(tools) == 0 {
		return "discovered_empty", "discovered_empty", "no tools discovered"
	}
	return "ready", "", ""
}

func effectiveServerConfigFromInstall(item data.ProfileMCPInstall, headers map[string]string) (effectiveMCPServerConfig, error) {
	install := sharedmcpinstall.EnabledInstall{
		ID:               item.ID,
		AccountID:        item.AccountID,
		ProfileRef:       item.ProfileRef,
		InstallKey:       item.InstallKey,
		DisplayName:      item.DisplayName,
		SourceKind:       item.SourceKind,
		SourceURI:        item.SourceURI,
		SyncMode:         item.SyncMode,
		Transport:        item.Transport,
		LaunchSpecJSON:   item.LaunchSpecJSON,
		HostRequirement:  item.HostRequirement,
		DiscoveryStatus:  item.DiscoveryStatus,
		LastErrorCode:    item.LastErrorCode,
		LastErrorMessage: item.LastErrorMessage,
		LastCheckedAt:    item.LastCheckedAt,
	}
	return sharedmcpinstall.ServerConfigFromInstall(install, headers, effectiveMCPDefaultTimeoutMs)
}

func checkEffectiveMCPHostRequirement(server effectiveMCPServerConfig, requirement string) error {
	return sharedmcpinstall.CheckHostRequirement(server, requirement)
}
