//go:build desktop

package fileops

import (
	"testing"

	shareddesktop "arkloop/services/shared/desktop"
	sharedtoolruntime "arkloop/services/shared/toolruntime"
)

func TestResolveBackendUsesExecutionModeOnDesktop(t *testing.T) {
	previousMode := shareddesktop.GetExecutionMode()
	t.Cleanup(func() {
		shareddesktop.SetExecutionMode(previousMode)
	})

	snapshot := &sharedtoolruntime.RuntimeSnapshot{
		SandboxBaseURL:      "http://sandbox.internal",
		SandboxAuthToken:    "token",
		DesktopExecutionMode: "local",
	}

	shareddesktop.SetExecutionMode("vm")
	if _, ok := ResolveBackend(snapshot, "/workspace", "run-1", "", "", "").(*LocalBackend); !ok {
		t.Fatal("expected local backend when snapshot execution mode is local")
	}

	snapshot.DesktopExecutionMode = "vm"
	shareddesktop.SetExecutionMode("local")
	if _, ok := ResolveBackend(snapshot, "/workspace", "run-1", "", "", "").(*SandboxExecBackend); !ok {
		t.Fatal("expected sandbox backend when snapshot execution mode is vm")
	}
}
