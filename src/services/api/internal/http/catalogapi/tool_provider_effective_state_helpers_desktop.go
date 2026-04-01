//go:build desktop

package catalogapi

import (
	"net/http"
	"strings"
	"time"

	"arkloop/services/shared/desktop"
	sharedtoolruntime "arkloop/services/shared/toolruntime"
)

const desktopDockerSandboxAddr = "127.0.0.1:19002"

var desktopSandboxHealthCheck = probeDesktopSandboxHealth

func SetDesktopSandboxHealthProbeForTest(probe func(addr string) bool) func() {
	prev := desktopSandboxHealthCheck
	if probe == nil {
		desktopSandboxHealthCheck = probeDesktopSandboxHealth
	} else {
		desktopSandboxHealthCheck = probe
	}
	return func() {
		desktopSandboxHealthCheck = prev
	}
}

func desktopSandboxAvailable() bool {
	return desktop.GetExecutionMode() == "vm" && desktopCurrentSandboxAvailable()
}

func desktopLocalACPAvailable() bool {
	return desktop.GetExecutionMode() == "local"
}

func desktopLocalMemoryAvailable() bool {
	return desktop.GetMemoryRuntime() == "local"
}

func resolveDesktopToolProviderRuntimeStatus(def toolProviderDefinition, snapshot sharedtoolruntime.RuntimeSnapshot) (toolProviderRuntimeStatus, bool) {
	switch def.GroupName {
	case "sandbox":
		switch def.ProviderName {
		case "sandbox.docker":
			if desktopDockerSandboxAvailable() {
				return toolProviderRuntimeStatus{
					Status: toolProviderRuntimeStatusAvailable,
					Source: toolProviderRuntimeSourceSandbox,
				}, true
			}
		case "sandbox.firecracker":
			if desktopFirecrackerAvailable() {
				return toolProviderRuntimeStatus{
					Status: toolProviderRuntimeStatusAvailable,
					Source: toolProviderRuntimeSourceSandbox,
				}, true
			}
		}
		return toolProviderRuntimeStatus{
			Status: toolProviderRuntimeStatusUnavailable,
			Source: toolProviderRuntimeSourceNone,
		}, true
	case "acp":
		if desktopSandboxAvailable() {
			return toolProviderRuntimeStatus{
				Status: toolProviderRuntimeStatusAvailable,
				Source: toolProviderRuntimeSourceSandbox,
			}, true
		}
		if desktopLocalACPAvailable() {
			return toolProviderRuntimeStatus{
				Status: toolProviderRuntimeStatusAvailable,
				Source: toolProviderRuntimeSourceLocal,
			}, true
		}
		return toolProviderRuntimeStatus{
			Status: toolProviderRuntimeStatusUnavailable,
			Source: toolProviderRuntimeSourceNone,
		}, true
	case "memory":
		switch desktop.GetMemoryRuntime() {
		case "openviking":
			if def.ProviderName != "memory.openviking" {
				return toolProviderRuntimeStatus{
					Status: toolProviderRuntimeStatusUnavailable,
					Source: toolProviderRuntimeSourceNone,
				}, true
			}
			return toolProviderRuntimeStatus{
				Status: toolProviderRuntimeStatusAvailable,
				Source: toolProviderRuntimeSourceProviderConfig,
			}, true
		case "local":
			return toolProviderRuntimeStatus{
				Status: toolProviderRuntimeStatusAvailable,
				Source: toolProviderRuntimeSourceLocal,
			}, true
		default:
			return toolProviderRuntimeStatus{
				Status: toolProviderRuntimeStatusUnavailable,
				Source: toolProviderRuntimeSourceNone,
			}, true
		}
	}
	return toolProviderRuntimeStatus{}, false
}

func desktopCurrentSandboxAvailable() bool {
	addr := strings.TrimSpace(desktop.GetSandboxAddr())
	return addr != "" && desktopSandboxHealthCheck(addr)
}

func desktopDockerSandboxAvailable() bool {
	return desktopSandboxHealthCheck(desktopDockerSandboxAddr)
}

func desktopFirecrackerAvailable() bool {
	addr := strings.TrimSpace(desktop.GetSandboxAddr())
	return addr != "" && addr != desktopDockerSandboxAddr && desktopSandboxHealthCheck(addr)
}

func probeDesktopSandboxHealth(addr string) bool {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return false
	}
	client := http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get("http://" + addr + "/healthz")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}
