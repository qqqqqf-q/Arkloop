package http

import (
	"context"
	nethttp "net/http"
	"time"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
)

const apiKeyLastUsedUpdateTimeout = 2 * time.Second

func resolveActorFromAPIKey(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	rawKey string,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
) (*actor, bool) {
	keyHash := data.HashAPIKey(rawKey)

	apiKey, err := apiKeysRepo.GetByHash(r.Context(), keyHash)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return nil, false
	}
	if apiKey == nil || apiKey.RevokedAt != nil {
		WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_api_key", "invalid or revoked API key", traceID, nil)
		return nil, false
	}

	if membershipRepo == nil {
		WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return nil, false
	}

	membership, err := membershipRepo.GetByOrgAndUser(r.Context(), apiKey.AccountID, apiKey.UserID)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return nil, false
	}
	if membership == nil {
		WriteError(w, nethttp.StatusForbidden, "auth.no_account_membership", "user has no account membership", traceID, nil)
		return nil, false
	}

	keyID := apiKey.ID
	accountID := apiKey.AccountID
	userID := apiKey.UserID

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), apiKeyLastUsedUpdateTimeout)
		defer cancel()

		_ = apiKeysRepo.UpdateLastUsed(ctx, keyID)
	}()

	if auditWriter != nil {
		auditWriter.WriteAPIKeyUsed(r.Context(), traceID, accountID, userID, keyID, "api_key.used")
	}

	normalizedScopes, _ := auth.NormalizePermissions(apiKey.Scopes)
	effectivePermissions := auth.IntersectPermissions(auth.PermissionsForRole(membership.Role), normalizedScopes)

	return &actor{
		AccountID:   membership.AccountID,
		UserID:      apiKey.UserID,
		AccountRole: membership.Role,
		Permissions: effectivePermissions,
	}, true
}
