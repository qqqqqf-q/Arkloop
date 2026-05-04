//go:build desktop

package http

import (
	"errors"
	nethttp "net/http"
	"strings"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
)

func actorFromVerifiedToken(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	token string,
) (*actor, bool) {
	if authService == nil {
		writeAuthNotConfigured(w, traceID)
		return nil, false
	}

	verified, err := authService.VerifyAccessTokenForActor(r.Context(), token)
	if err != nil {
		var expired auth.TokenExpiredError
		if errors.As(err, &expired) {
			WriteError(w, nethttp.StatusUnauthorized, "auth.token_expired", expired.Error(), traceID, nil)
			return nil, false
		}
		var invalid auth.TokenInvalidError
		if errors.As(err, &invalid) {
			WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_token", invalid.Error(), traceID, nil)
			return nil, false
		}
		var notFound auth.UserNotFoundError
		if errors.As(err, &notFound) {
			WriteError(w, nethttp.StatusUnauthorized, "auth.user_not_found", "user not found", traceID, nil)
			return nil, false
		}
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return nil, false
	}
	if verified.AccountID == uuid.Nil || strings.TrimSpace(verified.AccountRole) == "" {
		WriteError(w, nethttp.StatusForbidden, "auth.no_account_membership", "user has no account membership", traceID, nil)
		return nil, false
	}
	return &actor{
		AccountID:   verified.AccountID,
		UserID:      verified.UserID,
		AccountRole: verified.AccountRole,
		Permissions: auth.PermissionsForRole(verified.AccountRole),
	}, true
}

func authenticateActor(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	_ *data.AccountMembershipRepository,
) (*actor, bool) {
	token, ok := parseBearerToken(w, r, traceID)
	if !ok {
		return nil, false
	}
	return actorFromVerifiedToken(w, r, traceID, authService, token)
}

func resolveActor(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
) (*actor, bool) {
	token, ok := parseBearerToken(w, r, traceID)
	if !ok {
		return nil, false
	}
	if apiKeysRepo != nil && strings.HasPrefix(token, "ak-") {
		return resolveActorFromAPIKey(w, r, traceID, token, membershipRepo, apiKeysRepo, auditWriter)
	}
	return actorFromVerifiedToken(w, r, traceID, authService, token)
}
