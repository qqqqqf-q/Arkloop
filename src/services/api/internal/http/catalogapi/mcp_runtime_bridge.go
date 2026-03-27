package catalogapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"arkloop/services/api/internal/data"
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
	server := effectiveMCPServerConfig{
		ServerID:      strings.TrimSpace(item.InstallKey),
		AccountID:     item.AccountID.String(),
		Transport:     strings.TrimSpace(item.Transport),
		CallTimeoutMs: effectiveMCPDefaultTimeoutMs,
		Env:           map[string]string{},
		Headers:       map[string]string{},
	}
	if len(headers) > 0 {
		for key, value := range headers {
			server.Headers[strings.TrimSpace(key)] = value
		}
	}
	if len(item.LaunchSpecJSON) == 0 {
		return server, fmt.Errorf("launch spec missing")
	}
	var spec map[string]any
	if err := json.Unmarshal(item.LaunchSpecJSON, &spec); err != nil {
		return server, fmt.Errorf("launch spec is invalid")
	}
	parsed, err := parseEffectiveMCPServerConfig(server.ServerID, spec)
	if err != nil {
		return server, err
	}
	parsed.AccountID = server.AccountID
	for key, value := range server.Headers {
		if parsed.Headers == nil {
			parsed.Headers = map[string]string{}
		}
		parsed.Headers[key] = value
	}
	return parsed, nil
}

func checkEffectiveMCPHostRequirement(server effectiveMCPServerConfig, requirement string) error {
	requirement = strings.TrimSpace(requirement)
	if requirement == "" {
		return nil
	}
	switch requirement {
	case "remote_http":
		if server.Transport == "stdio" {
			return fmt.Errorf("remote_http host does not support stdio launch specs")
		}
		return nil
	case "cloud_worker":
		if server.Transport != "stdio" {
			return nil
		}
		if server.Command == "" {
			return fmt.Errorf("stdio command missing")
		}
		return nil
	case "desktop_local", "desktop_sidecar":
		return nil
	default:
		return nil
	}
}
