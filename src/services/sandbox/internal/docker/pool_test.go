package docker

import (
	"testing"

	"github.com/docker/docker/api/types/container"
)

func TestBuildCreatePlan_DefaultNetwork(t *testing.T) {
	plan := buildCreatePlan(Config{
		Image:          "arkloop/sandbox-agent:latest",
		NetworkName:    "",
		GuestAgentPort: 8080,
	}, "lite")

	if plan.dialByContainerIP {
		t.Fatalf("expected dialByContainerIP=false")
	}
	if plan.attachNetworkName != defaultAgentNetworkName {
		t.Fatalf("expected attachNetworkName=%q, got %q", defaultAgentNetworkName, plan.attachNetworkName)
	}
	if plan.hostCfg.NetworkMode != container.NetworkMode(defaultAgentNetworkName) {
		t.Fatalf("expected network mode %q, got %q", defaultAgentNetworkName, plan.hostCfg.NetworkMode)
	}
	if len(plan.hostCfg.PortBindings) == 0 {
		t.Fatalf("expected port bindings to be set")
	}
	bindings, ok := plan.hostCfg.PortBindings[plan.exposedPort]
	if !ok || len(bindings) != 1 {
		t.Fatalf("expected exactly one binding for %s", plan.exposedPort)
	}
	if bindings[0].HostIP != "127.0.0.1" {
		t.Fatalf("expected HostIP=127.0.0.1, got %q", bindings[0].HostIP)
	}
	if bindings[0].HostPort != "0" {
		t.Fatalf("expected HostPort=0, got %q", bindings[0].HostPort)
	}

	if len(plan.hostCfg.CapAdd) != 0 {
		t.Fatalf("expected CapAdd empty, got %v", plan.hostCfg.CapAdd)
	}
	if len(plan.hostCfg.CapDrop) != 1 || plan.hostCfg.CapDrop[0] != "ALL" {
		t.Fatalf("expected CapDrop=[ALL], got %v", plan.hostCfg.CapDrop)
	}
}

func TestBuildCreatePlan_CustomNetwork(t *testing.T) {
	plan := buildCreatePlan(Config{
		Image:          "arkloop/sandbox-agent:latest",
		AllowEgress:    true,
		NetworkName:    defaultAgentNetworkName,
		GuestAgentPort: 8080,
	}, "lite")

	if !plan.dialByContainerIP {
		t.Fatalf("expected dialByContainerIP=true")
	}
	if plan.attachNetworkName != defaultAgentNetworkName {
		t.Fatalf("expected attachNetworkName=%q, got %q", defaultAgentNetworkName, plan.attachNetworkName)
	}
	if plan.hostCfg.NetworkMode != container.NetworkMode(defaultAgentNetworkName) {
		t.Fatalf("expected network mode %q, got %q", defaultAgentNetworkName, plan.hostCfg.NetworkMode)
	}
	if len(plan.hostCfg.PortBindings) != 0 {
		t.Fatalf("expected port bindings empty, got %v", plan.hostCfg.PortBindings)
	}

	if len(plan.hostCfg.CapAdd) != 0 {
		t.Fatalf("expected CapAdd empty, got %v", plan.hostCfg.CapAdd)
	}
	if len(plan.hostCfg.CapDrop) != 1 || plan.hostCfg.CapDrop[0] != "ALL" {
		t.Fatalf("expected CapDrop=[ALL], got %v", plan.hostCfg.CapDrop)
	}
}

func TestBuildCreatePlan_DefaultNetworkWithEgress(t *testing.T) {
	plan := buildCreatePlan(Config{
		Image:          "arkloop/sandbox-agent:latest",
		AllowEgress:    true,
		GuestAgentPort: 8080,
	}, "lite")

	if plan.attachNetworkName != defaultAgentNetworkName {
		t.Fatalf("expected attachNetworkName=%q, got %q", defaultAgentNetworkName, plan.attachNetworkName)
	}
	if plan.hostCfg.NetworkMode != container.NetworkMode(defaultAgentNetworkName) {
		t.Fatalf("expected network mode %q, got %q", defaultAgentNetworkName, plan.hostCfg.NetworkMode)
	}
	if len(plan.hostCfg.PortBindings) == 0 {
		t.Fatalf("expected host port binding in default network mode")
	}
}
