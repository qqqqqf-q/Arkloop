package accountapi

import (
	"encoding/json"
	nethttp "net/http"
	"strings"
	"time"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	httpkit "arkloop/services/api/internal/http/httpkit"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const bindCodeTTL = 24 * time.Hour

type createBindCodeRequest struct {
	ChannelType *string `json:"channel_type"`
}

type bindCodeResponse struct {
	ID          string  `json:"id"`
	Token       string  `json:"token"`
	ChannelType *string `json:"channel_type"`
	ExpiresAt   string  `json:"expires_at"`
	CreatedAt   string  `json:"created_at"`
}

type channelIdentityResponse struct {
	ID                string          `json:"id"`
	ChannelType       string          `json:"channel_type"`
	PlatformSubjectID string          `json:"platform_subject_id"`
	DisplayName       *string         `json:"display_name"`
	AvatarURL         *string         `json:"avatar_url"`
	Metadata          json.RawMessage `json:"metadata"`
	CreatedAt         string          `json:"created_at"`
}

func channelBindsEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	bindCodesRepo *data.ChannelBindCodesRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodPost:
			createBindCode(w, r, authService, membershipRepo, bindCodesRepo, apiKeysRepo)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func channelIdentitiesEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	identitiesRepo *data.ChannelIdentitiesRepository,
	apiKeysRepo *data.APIKeysRepository,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodGet:
			listMyChannelIdentities(w, r, authService, membershipRepo, identitiesRepo, apiKeysRepo)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func channelIdentityEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	identitiesRepo *data.ChannelIdentitiesRepository,
	channelIdentityLinksRepo *data.ChannelIdentityLinksRepository,
	apiKeysRepo *data.APIKeysRepository,
	pool data.DB,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/me/channel-identities/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			httpkit.WriteNotFound(w, r)
			return
		}

		identityID, err := uuid.Parse(tail)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid identity id", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodDelete:
			unbindChannelIdentity(w, r, traceID, identityID, authService, membershipRepo, identitiesRepo, channelIdentityLinksRepo, apiKeysRepo, pool)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func createBindCode(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	bindCodesRepo *data.ChannelBindCodesRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if bindCodesRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
	if !ok {
		return
	}

	var req createBindCodeRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		// body 可为空
		req = createBindCodeRequest{}
	}

	if req.ChannelType != nil {
		ct := strings.TrimSpace(strings.ToLower(*req.ChannelType))
		if _, ok := validChannelTypes[ct]; !ok {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "unsupported channel_type", traceID, nil)
			return
		}
		req.ChannelType = &ct
	}

	bc, err := bindCodesRepo.Create(r.Context(), actor.UserID, req.ChannelType, bindCodeTTL)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toBindCodeResponse(bc))
}

func listMyChannelIdentities(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	identitiesRepo *data.ChannelIdentitiesRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if identitiesRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
	if !ok {
		return
	}

	identities, err := identitiesRepo.ListByUserID(r.Context(), actor.UserID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	resp := make([]channelIdentityResponse, 0, len(identities))
	for _, ci := range identities {
		resp = append(resp, toChannelIdentityResponse(ci))
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
}

func unbindChannelIdentity(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	identityID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	identitiesRepo *data.ChannelIdentitiesRepository,
	channelIdentityLinksRepo *data.ChannelIdentityLinksRepository,
	apiKeysRepo *data.APIKeysRepository,
	pool data.DB,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if identitiesRepo == nil || channelIdentityLinksRepo == nil || pool == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
	if !ok {
		return
	}

	ci, err := identitiesRepo.GetByID(r.Context(), identityID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if ci == nil || ci.UserID == nil || *ci.UserID != actor.UserID {
		httpkit.WriteError(w, nethttp.StatusNotFound, "channel_identities.not_found", "channel identity not found", traceID, nil)
		return
	}

	bindings, err := channelIdentityLinksRepo.ListBindingsByIdentity(r.Context(), identityID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	for _, binding := range bindings {
		if binding.IsOwner {
			httpkit.WriteError(w, nethttp.StatusConflict, "channel_bindings.owner_unbind_blocked", "owner cannot be unlinked directly", traceID, nil)
			return
		}
	}

	tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	if err := channelIdentityLinksRepo.WithTx(tx).DeleteByIdentity(r.Context(), identityID); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if err := identitiesRepo.WithTx(tx).UpdateUserID(r.Context(), identityID, nil); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func toBindCodeResponse(bc data.ChannelBindCode) bindCodeResponse {
	return bindCodeResponse{
		ID:          bc.ID.String(),
		Token:       bc.Token,
		ChannelType: bc.ChannelType,
		ExpiresAt:   bc.ExpiresAt.UTC().Format(time.RFC3339Nano),
		CreatedAt:   bc.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func toChannelIdentityResponse(ci data.ChannelIdentity) channelIdentityResponse {
	metadata := ci.Metadata
	if metadata == nil {
		metadata = json.RawMessage(`{}`)
	}
	return channelIdentityResponse{
		ID:                ci.ID.String(),
		ChannelType:       ci.ChannelType,
		PlatformSubjectID: ci.PlatformSubjectID,
		DisplayName:       ci.DisplayName,
		AvatarURL:         ci.AvatarURL,
		Metadata:          metadata,
		CreatedAt:         ci.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}
