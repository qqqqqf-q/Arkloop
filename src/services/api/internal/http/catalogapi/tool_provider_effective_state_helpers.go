//go:build !desktop

package catalogapi

import sharedtoolruntime "arkloop/services/shared/toolruntime"

func desktopSandboxAvailable() bool {
	return false
}

func desktopLocalACPAvailable() bool {
	return false
}

func desktopLocalMemoryAvailable() bool {
	return false
}

func resolveDesktopToolProviderRuntimeStatus(def toolProviderDefinition, snapshot sharedtoolruntime.RuntimeSnapshot) (toolProviderRuntimeStatus, bool) {
	_ = snapshot
	return toolProviderRuntimeStatus{}, false
}
