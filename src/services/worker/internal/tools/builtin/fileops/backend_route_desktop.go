//go:build desktop

package fileops

import (
	"strings"

	sharedtoolruntime "arkloop/services/shared/toolruntime"
)

func useSandboxBackend(snapshot *sharedtoolruntime.RuntimeSnapshot) bool {
	if snapshot == nil || strings.TrimSpace(snapshot.SandboxBaseURL) == "" {
		return false
	}
	return strings.TrimSpace(snapshot.DesktopExecutionMode) == "vm"
}
