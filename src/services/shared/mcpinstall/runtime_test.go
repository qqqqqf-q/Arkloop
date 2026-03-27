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
