//go:build desktop

package http

import (
	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
)

// 桌面模式下所有请求共享同一个 actor，无 JWT 验证和吊销检查。
func desktopOwnerActor() *actor {
	return &actor{
		AccountID:   auth.DesktopAccountID,
		UserID:      auth.DesktopUserID,
		AccountRole: auth.DesktopRole,
		Permissions: auth.PermissionsForRole(auth.DesktopRole),
	}
}

func authenticateActor(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	_ string,
	_ *auth.Service,
	_ *data.AccountMembershipRepository,
) (*actor, bool) {
	_, ok := parseBearerToken(w, r, "")
	if !ok {
		return nil, false
	}
	return desktopOwnerActor(), true
}

func resolveActor(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	_ string,
	_ *auth.Service,
	_ *data.AccountMembershipRepository,
	_ *data.APIKeysRepository,
	_ *audit.Writer,
) (*actor, bool) {
	_, ok := parseBearerToken(w, r, "")
	if !ok {
		return nil, false
	}
	return desktopOwnerActor(), true
}
