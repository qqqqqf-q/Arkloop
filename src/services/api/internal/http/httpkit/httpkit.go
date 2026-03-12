package httpkit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

type ErrorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id"`
	Details any    `json:"details,omitempty"`
}

func WriteError(w nethttp.ResponseWriter, statusCode int, code string, message string, traceID string, details any) {
	if traceID == "" {
		traceID = observability.NewTraceID()
	}

	envelope := ErrorEnvelope{
		Code:    code,
		Message: message,
		TraceID: traceID,
		Details: details,
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		payload = []byte(fmt.Sprintf(`{"code":"internal.error","message":"marshal failed","trace_id":"%s"}`, traceID))
		statusCode = nethttp.StatusInternalServerError
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set(observability.TraceIDHeader, traceID)
	w.WriteHeader(statusCode)
	_, _ = w.Write(payload)
}

type Actor struct {
	AccountID   uuid.UUID
	UserID      uuid.UUID
	AccountRole string
	Permissions []string
}

const (
	apiKeyLastUsedUpdateTimeout = 2 * time.Second
	MaxJSONBodySize             = 1 << 20
)

func (a *Actor) HasPermission(perm string) bool {
	for _, p := range a.Permissions {
		if p == perm {
			return true
		}
	}
	return false
}

func RequirePerm(actor *Actor, perm string, w nethttp.ResponseWriter, traceID string) bool {
	if actor != nil && actor.HasPermission(perm) {
		return true
	}
	WriteError(w, nethttp.StatusForbidden, "auth.forbidden", "access denied", traceID, nil)
	return false
}

func AuthenticateActor(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
) (*Actor, bool) {
	_ = membershipRepo

	if authService == nil {
		WriteAuthNotConfigured(w, traceID)
		return nil, false
	}

	token, ok := ParseBearerToken(w, r, traceID)
	if !ok {
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

	return &Actor{
		AccountID:   verified.AccountID,
		UserID:      verified.UserID,
		AccountRole: verified.AccountRole,
		Permissions: auth.PermissionsForRole(verified.AccountRole),
	}, true
}

func ResolveActor(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
) (*Actor, bool) {
	token, ok := ParseBearerToken(w, r, traceID)
	if !ok {
		return nil, false
	}

	if apiKeysRepo != nil && strings.HasPrefix(token, "ak-") {
		return ResolveActorFromAPIKey(w, r, traceID, token, membershipRepo, apiKeysRepo, auditWriter)
	}

	if authService == nil {
		WriteAuthNotConfigured(w, traceID)
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

	return &Actor{
		AccountID:   verified.AccountID,
		UserID:      verified.UserID,
		AccountRole: verified.AccountRole,
		Permissions: auth.PermissionsForRole(verified.AccountRole),
	}, true
}

func ResolveActorFromAPIKey(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	rawKey string,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
) (*Actor, bool) {
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

	return &Actor{
		AccountID:   membership.AccountID,
		UserID:      apiKey.UserID,
		AccountRole: membership.Role,
		Permissions: effectivePermissions,
	}, true
}

func DecodeJSON(r *nethttp.Request, dst any) error {
	reader := nethttp.MaxBytesReader(nil, r.Body, MaxJSONBodySize)
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}

func WriteJSON(w nethttp.ResponseWriter, traceID string, statusCode int, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_, _ = w.Write(raw)
}

func ParseBearerToken(w nethttp.ResponseWriter, r *nethttp.Request, traceID string) (string, bool) {
	authorization := r.Header.Get("Authorization")
	if strings.TrimSpace(authorization) == "" {
		WriteError(w, nethttp.StatusUnauthorized, "auth.missing_token", "missing Authorization Bearer token", traceID, nil)
		return "", false
	}

	scheme, rest, ok := strings.Cut(authorization, " ")
	if !ok || strings.TrimSpace(rest) == "" || strings.ToLower(scheme) != "bearer" {
		WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_authorization", "Authorization header must be: Bearer <token>", traceID, nil)
		return "", false
	}

	return strings.TrimSpace(rest), true
}

func WriteMethodNotAllowed(w nethttp.ResponseWriter, r *nethttp.Request) {
	traceID := observability.TraceIDFromContext(r.Context())
	WriteError(w, nethttp.StatusMethodNotAllowed, "http.method_not_allowed", "Method Not Allowed", traceID, nil)
}

func WriteAuthNotConfigured(w nethttp.ResponseWriter, traceID string) {
	WriteError(w, nethttp.StatusServiceUnavailable, "auth.not_configured", "auth not configured", traceID, nil)
}

func WriteNotFound(w nethttp.ResponseWriter, r *nethttp.Request) {
	traceID := observability.TraceIDFromContext(r.Context())
	WriteError(w, nethttp.StatusNotFound, "http.method_not_allowed", "Not Found", traceID, nil)
}
