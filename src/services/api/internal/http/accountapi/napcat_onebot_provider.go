//go:build windows

package accountapi

import (
	shareddesktop "arkloop/services/shared/desktop"
	"arkloop/services/shared/napcat"
)

func wireNapCatOneBotProvider(mgr *napcat.Manager) {
	if mgr == nil {
		return
	}
	shareddesktop.SetOneBotHTTPEndpointProvider(mgr.OneBotHTTPEndpoint)
}
