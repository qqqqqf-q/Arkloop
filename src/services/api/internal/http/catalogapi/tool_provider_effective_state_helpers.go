//go:build !desktop

package catalogapi

import sharedtoolruntime "arkloop/services/shared/toolruntime"

func resolveDesktopToolProviderRuntimeStatus(def toolProviderDefinition, snapshot sharedtoolruntime.RuntimeSnapshot) (toolProviderRuntimeStatus, bool) {
	_ = snapshot
	return toolProviderRuntimeStatus{}, false
}
