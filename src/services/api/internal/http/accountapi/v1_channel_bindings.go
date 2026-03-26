package accountapi

import (
	"encoding/json"
	nethttp "net/http"
	"strings"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	httpkit "arkloop/services/api/internal/http/httpkit"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type channelBindingResponse struct {
	BindingID                string  `json:"binding_id"`
	ChannelIdentityID        string  `json:"channel_identity_id"`
	DisplayName              *string `json:"display_name"`
	PlatformSubjectID        string  `json:"platform_subject_id"`
	IsOwner                  bool    `json:"is_owner"`
	HeartbeatEnabled         bool    `json:"heartbeat_enabled"`
	HeartbeatIntervalMinutes int     `json:"heartbeat_interval_minutes"`
	HeartbeatModel           *string `json:"heartbeat_model"`
}

type updateChannelBindingRequest struct {
	MakeOwner                bool    `json:"make_owner"`
	HeartbeatEnabled         *bool   `json:"heartbeat_enabled"`
	HeartbeatIntervalMinutes *int    `json:"heartbeat_interval_minutes"`
	HeartbeatModel           *string `json:"heartbeat_model"`
}

func handleChannelBindingsSubresource(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	channelID uuid.UUID,
	bindingID *uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	channelsRepo *data.ChannelsRepository,
	channelIdentityLinksRepo *data.ChannelIdentityLinksRepository,
	channelIdentitiesRepo *data.ChannelIdentitiesRepository,
	apiKeysRepo *data.APIKeysRepository,
	pool data.DB,
) bool {
	if authService == nil || channelsRepo == nil || channelIdentityLinksRepo == nil || channelIdentitiesRepo == nil || pool == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return true
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return true
	}
	if !httpkit.RequirePerm(actor, auth.PermDataChannelsManage, w, traceID) {
		return true
	}

	ch, err := channelsRepo.GetByID(r.Context(), channelID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return true
	}
	if ch == nil || ch.AccountID != actor.AccountID {
		httpkit.WriteError(w, nethttp.StatusNotFound, "channels.not_found", "channel not found", traceID, nil)
		return true
	}

	if bindingID == nil {
		if r.Method != nethttp.MethodGet {
			httpkit.WriteMethodNotAllowed(w, r)
			return true
		}
		list, err := channelIdentityLinksRepo.ListBindings(r.Context(), actor.AccountID, channelID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return true
		}
		resp := make([]channelBindingResponse, 0, len(list))
		for _, item := range list {
			resp = append(resp, toChannelBindingResponse(item))
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
		return true
	}

	switch r.Method {
	case nethttp.MethodPatch:
		updateChannelBinding(w, r, traceID, actor.AccountID, channelID, *bindingID, channelsRepo, channelIdentityLinksRepo, channelIdentitiesRepo, pool)
	case nethttp.MethodDelete:
		deleteChannelBinding(w, r, traceID, actor.AccountID, channelID, *bindingID, channelIdentityLinksRepo)
	default:
		httpkit.WriteMethodNotAllowed(w, r)
	}
	return true
}

func updateChannelBinding(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	accountID uuid.UUID,
	channelID uuid.UUID,
	bindingID uuid.UUID,
	channelsRepo *data.ChannelsRepository,
	channelIdentityLinksRepo *data.ChannelIdentityLinksRepository,
	channelIdentitiesRepo *data.ChannelIdentitiesRepository,
	pool data.DB,
) {
	var req updateChannelBindingRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}
	if req.HeartbeatIntervalMinutes != nil && *req.HeartbeatIntervalMinutes <= 0 {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "heartbeat_interval_minutes must be positive", traceID, nil)
		return
	}

	tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	linksRepo := channelIdentityLinksRepo.WithTx(tx)
	channelsRepoTx := channelsRepo.WithTx(tx)
	identitiesRepoTx := channelIdentitiesRepo.WithTx(tx)

	binding, err := linksRepo.GetBinding(r.Context(), accountID, channelID, bindingID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if binding == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "channel_bindings.not_found", "binding not found", traceID, nil)
		return
	}

	if req.MakeOwner {
		if binding.UserID == nil || *binding.UserID == uuid.Nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "binding user not available", traceID, nil)
			return
		}
		nextOwner := binding.UserID
		if _, err := channelsRepoTx.Update(r.Context(), channelID, accountID, data.ChannelUpdate{OwnerUserID: &nextOwner}); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
	}

	if req.HeartbeatEnabled != nil || req.HeartbeatIntervalMinutes != nil || req.HeartbeatModel != nil {
		enabled, intervalMinutes, model, err := identitiesRepoTx.GetHeartbeatConfig(r.Context(), binding.ChannelIdentityID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if req.HeartbeatEnabled != nil {
			enabled = *req.HeartbeatEnabled
		}
		if req.HeartbeatIntervalMinutes != nil {
			intervalMinutes = *req.HeartbeatIntervalMinutes
		}
		if req.HeartbeatModel != nil {
			model = strings.TrimSpace(*req.HeartbeatModel)
		}
		if err := identitiesRepoTx.UpdateHeartbeatConfig(r.Context(), binding.ChannelIdentityID, enabled, intervalMinutes, model); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
	}

	updated, err := linksRepo.GetBinding(r.Context(), accountID, channelID, bindingID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if updated == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "channel_bindings.not_found", "binding not found", traceID, nil)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toChannelBindingResponse(*updated))
}

func deleteChannelBinding(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	accountID uuid.UUID,
	channelID uuid.UUID,
	bindingID uuid.UUID,
	channelIdentityLinksRepo *data.ChannelIdentityLinksRepository,
) {
	binding, err := channelIdentityLinksRepo.GetBinding(r.Context(), accountID, channelID, bindingID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if binding == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "channel_bindings.not_found", "binding not found", traceID, nil)
		return
	}
	if binding.IsOwner {
		httpkit.WriteError(w, nethttp.StatusConflict, "channel_bindings.owner_unbind_blocked", "owner cannot be unlinked directly", traceID, nil)
		return
	}
	if err := channelIdentityLinksRepo.DeleteBinding(r.Context(), accountID, channelID, bindingID); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func toChannelBindingResponse(item data.ChannelBinding) channelBindingResponse {
	var heartbeatModel *string
	model := strings.TrimSpace(item.HeartbeatModel)
	if model != "" {
		heartbeatModel = &model
	}
	return channelBindingResponse{
		BindingID:                item.BindingID.String(),
		ChannelIdentityID:        item.ChannelIdentityID.String(),
		DisplayName:              item.DisplayName,
		PlatformSubjectID:        item.PlatformSubjectID,
		IsOwner:                  item.IsOwner,
		HeartbeatEnabled:         item.HeartbeatEnabled,
		HeartbeatIntervalMinutes: item.HeartbeatIntervalMinutes,
		HeartbeatModel:           heartbeatModel,
	}
}

func marshalChannelBindingResponse(item data.ChannelBinding) json.RawMessage {
	body, _ := json.Marshal(toChannelBindingResponse(item))
	return body
}
