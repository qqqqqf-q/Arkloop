package mcpinstall

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestServerConfigFromInstallAppliesSharedHostFilteringInputs(t *testing.T) {
	launchSpec, err := json.Marshal(map[string]any{
		"transport": "stdio",
		"command":   "node",
		"args":      []string{"server.js"},
	})
	if err != nil {
		t.Fatalf("marshal launch spec: %v", err)
	}

	server, err := ServerConfigFromInstall(EnabledInstall{
		AccountID:       uuid.New(),
		InstallKey:      "demo",
		Transport:       "stdio",
		LaunchSpecJSON:  launchSpec,
		HostRequirement: "cloud_worker",
	}, map[string]string{"Authorization": "Bearer token"}, 10_000)
	if err != nil {
		t.Fatalf("server config from install: %v", err)
	}
	if server.Command != "node" {
		t.Fatalf("unexpected command: %q", server.Command)
	}
	if got := server.Headers["Authorization"]; got != "Bearer token" {
		t.Fatalf("unexpected auth header: %q", got)
	}
	if err := CheckHostRequirement(server, "cloud_worker"); err != nil {
		t.Fatalf("expected shared host requirement check to pass: %v", err)
	}
	if err := CheckHostRequirement(server, "remote_http"); err == nil {
		t.Fatal("expected stdio config to fail remote_http requirement")
	}
}

func TestServerConfigFromInstallMergesSecretEnv(t *testing.T) {
	launchSpec, err := json.Marshal(map[string]any{
		"transport": "stdio",
		"command":   "node",
		"env": map[string]any{
			"VISIBLE": "1",
		},
	})
	if err != nil {
		t.Fatalf("marshal launch spec: %v", err)
	}

	server, err := ServerConfigFromInstallWithAuth(EnabledInstall{
		AccountID:      uuid.New(),
		InstallKey:     "demo",
		Transport:      "stdio",
		LaunchSpecJSON: launchSpec,
	}, AuthPayload{
		Env: map[string]string{"GITHUB_TOKEN": "secret"},
	}, 10_000)
	if err != nil {
		t.Fatalf("server config from install: %v", err)
	}
	if got := server.Env["VISIBLE"]; got != "1" {
		t.Fatalf("unexpected visible env: %q", got)
	}
	if got := server.Env["GITHUB_TOKEN"]; got != "secret" {
		t.Fatalf("unexpected secret env: %q", got)
	}
}

func TestServerConfigFromInstallReadsWorkingDirAndParentEnvFlag(t *testing.T) {
	launchSpec, err := json.Marshal(map[string]any{
		"transport":          "stdio",
		"command":            "driver",
		"working_dir":        "/tmp/plugin",
		"inherit_parent_env": true,
	})
	if err != nil {
		t.Fatalf("marshal launch spec: %v", err)
	}
	server, err := ServerConfigFromInstall(EnabledInstall{
		AccountID:      uuid.New(),
		InstallKey:     "demo",
		Transport:      "stdio",
		LaunchSpecJSON: launchSpec,
	}, nil, 10_000)
	if err != nil {
		t.Fatalf("server config from install: %v", err)
	}
	if server.Cwd == nil || *server.Cwd != "/tmp/plugin" {
		t.Fatalf("unexpected cwd: %#v", server.Cwd)
	}
	if !server.InheritParentEnv {
		t.Fatalf("expected inherit_parent_env")
	}
}

func TestDesktopLocalRequirementRejectedOutsideDesktop(t *testing.T) {
	if desktopHostRequirementsAvailable {
		t.Skip("desktop build allows desktop_local requirements")
	}
	err := CheckHostRequirement(ServerConfig{Transport: "stdio", Command: "driver"}, "desktop_local")
	if err == nil {
		t.Fatal("expected desktop_local requirement to be rejected")
	}
}

func TestDecodeAuthPayloadSupportsLegacyHeaderMap(t *testing.T) {
	auth, err := DecodeAuthPayload([]byte(`{"Authorization":"Bearer token"}`))
	if err != nil {
		t.Fatalf("decode auth payload: %v", err)
	}
	if got := auth.Headers["Authorization"]; got != "Bearer token" {
		t.Fatalf("unexpected auth header: %q", got)
	}
}
