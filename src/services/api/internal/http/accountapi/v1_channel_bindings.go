package accountapi

import (
	"context"
	"encoding/json"
	"fmt"
	nethttp "net/http"
	"strings"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	httpkit "arkloop/services/api/internal/http/httpkit"
	"arkloop/services/shared/runkind"

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
	HeartbeatTargetCount     int     `json:"heartbeat_target_count"`
}

type updateChannelBindingRequest struct {
	MakeOwner                     bool    `json:"make_owner"`
	HeartbeatEnabled              *bool   `json:"heartbeat_enabled"`
	HeartbeatIntervalMinutes      *int    `json:"heartbeat_interval_minutes"`
	HeartbeatModel                *string `json:"heartbeat_model"`
	HeartbeatTargetPlatformChatID *string `json:"heartbeat_target_platform_chat_id"`
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
	personasRepo *data.PersonasRepository,
	channelIdentityLinksRepo *data.ChannelIdentityLinksRepository,
	channelIdentitiesRepo *data.ChannelIdentitiesRepository,
	channelDMThreadsRepo *data.ChannelDMThreadsRepository,
	channelGroupThreadsRepo *data.ChannelGroupThreadsRepository,
	threadRepo *data.ThreadRepository,
	apiKeysRepo *data.APIKeysRepository,
	pool data.DB,
) bool {
	if authService == nil || channelsRepo == nil || personasRepo == nil || channelIdentityLinksRepo == nil || channelIdentitiesRepo == nil || channelDMThreadsRepo == nil || pool == nil {
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
		updateChannelBinding(w, r, traceID, actor.AccountID, channelID, *bindingID, membershipRepo, channelsRepo, personasRepo, channelIdentityLinksRepo, channelIdentitiesRepo, channelGroupThreadsRepo, threadRepo, pool)
	case nethttp.MethodDelete:
		deleteChannelBinding(w, r, traceID, actor.AccountID, channelID, *bindingID, channelIdentityLinksRepo, channelIdentitiesRepo, channelDMThreadsRepo, pool)
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
	membershipRepo *data.AccountMembershipRepository,
	channelsRepo *data.ChannelsRepository,
	personasRepo *data.PersonasRepository,
	channelIdentityLinksRepo *data.ChannelIdentityLinksRepository,
	channelIdentitiesRepo *data.ChannelIdentitiesRepository,
	channelGroupThreadsRepo *data.ChannelGroupThreadsRepository,
	threadRepo *data.ThreadRepository,
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
	heartbeatTargetPlatformChatID := ""
	if req.HeartbeatTargetPlatformChatID != nil {
		heartbeatTargetPlatformChatID = strings.TrimSpace(*req.HeartbeatTargetPlatformChatID)
		if heartbeatTargetPlatformChatID == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "heartbeat target required", traceID, nil)
			return
		}
	}

	tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	linksRepo := channelIdentityLinksRepo.WithTx(tx)
	channelsRepoTx := channelsRepo.WithTx(tx)
	personasRepoTx := personasRepo.WithTx(tx)

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
		previousOwnerBinding, ownerBindingErr := linksRepo.GetOwnerBinding(r.Context(), accountID, channelID)
		if ownerBindingErr != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		membership, membershipErr := membershipRepo.WithTx(tx).GetByAccountAndUser(r.Context(), accountID, *binding.UserID)
		if membershipErr != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if membership == nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "binding user is not a member of this account", traceID, nil)
			return
		}
		nextOwner := binding.UserID
		if _, err := channelsRepoTx.Update(r.Context(), channelID, accountID, data.ChannelUpdate{OwnerUserID: &nextOwner}); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if previousOwnerBinding != nil && previousOwnerBinding.BindingID != binding.BindingID {
			if err := linksRepo.UpdateHeartbeatConfig(r.Context(), binding.BindingID, previousOwnerBinding.HeartbeatEnabled, previousOwnerBinding.HeartbeatIntervalMinutes, previousOwnerBinding.HeartbeatModel); err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if err := linksRepo.UpdateHeartbeatConfig(r.Context(), previousOwnerBinding.BindingID, false, runkind.DefaultHeartbeatIntervalMinutes, ""); err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
		}
	}

	if req.HeartbeatEnabled != nil || req.HeartbeatIntervalMinutes != nil || req.HeartbeatModel != nil || heartbeatTargetPlatformChatID != "" {
		if !binding.IsOwner {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "owner binding required", traceID, nil)
			return
		}
		enabled := binding.HeartbeatEnabled
		intervalMinutes := binding.HeartbeatIntervalMinutes
		model := binding.HeartbeatModel
		if req.HeartbeatEnabled != nil {
			enabled = *req.HeartbeatEnabled
		}
		if req.HeartbeatIntervalMinutes != nil {
			intervalMinutes = *req.HeartbeatIntervalMinutes
		}
		if req.HeartbeatModel != nil {
			model = strings.TrimSpace(*req.HeartbeatModel)
		}
		if heartbeatTargetPlatformChatID != "" {
			if req.HeartbeatEnabled != nil && !*req.HeartbeatEnabled {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "heartbeat target requires enabled heartbeat", traceID, nil)
				return
			}
			enabled = true
		}
		triggerRepo := data.ScheduledTriggersRepository{}
		if !enabled {
			if err := linksRepo.UpdateHeartbeatConfig(r.Context(), binding.BindingID, false, intervalMinutes, model); err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if err := triggerRepo.DeleteHeartbeatsByChannel(r.Context(), tx, binding.ChannelID); err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
		} else {
			channel, channelErr := channelsRepoTx.GetByID(r.Context(), channelID)
			if channelErr != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if channel == nil || channel.AccountID != accountID {
				httpkit.WriteError(w, nethttp.StatusNotFound, "channels.not_found", "channel not found", traceID, nil)
				return
			}
			if channel.PersonaID == nil || *channel.PersonaID == uuid.Nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "channel persona not configured", traceID, nil)
				return
			}
			persona, personaErr := personasRepoTx.GetByIDForAccount(r.Context(), accountID, *channel.PersonaID)
			if personaErr != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if persona == nil || strings.TrimSpace(persona.PersonaKey) == "" {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "channel persona not found", traceID, nil)
				return
			}
			triggerModel := firstNonEmptySelector(model, resolveChannelBurstDefaultModel(channel.ConfigJSON))
			if err := linksRepo.UpdateHeartbeatConfig(r.Context(), binding.BindingID, true, intervalMinutes, model); err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if heartbeatTargetPlatformChatID != "" {
				if channelGroupThreadsRepo == nil || threadRepo == nil {
					httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
					return
				}
				if err := upsertChannelHeartbeatTarget(r.Context(), tx, *channel, *persona, heartbeatTargetPlatformChatID, triggerModel, intervalMinutes, channelIdentitiesRepo, channelGroupThreadsRepo, threadRepo, personasRepo); err != nil {
					httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
					return
				}
			} else {
				targetCount, countErr := triggerRepo.CountHeartbeatTargetsByChannel(r.Context(), tx, binding.ChannelID)
				if countErr != nil {
					httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
					return
				}
				if targetCount == 0 {
					httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "heartbeat target required", traceID, nil)
					return
				}
				if err := triggerRepo.SyncHeartbeatConfigByChannel(r.Context(), tx, binding.ChannelID, triggerModel, intervalMinutes); err != nil {
					httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
					return
				}
			}
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
	channelIdentitiesRepo *data.ChannelIdentitiesRepository,
	channelDMThreadsRepo *data.ChannelDMThreadsRepository,
	pool data.DB,
) {
	tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	linksRepo := channelIdentityLinksRepo.WithTx(tx)
	dmThreadsRepo := channelDMThreadsRepo.WithTx(tx)

	binding, err := linksRepo.GetBinding(r.Context(), accountID, channelID, bindingID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if binding == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "channel_bindings.not_found", "binding not found", traceID, nil)
		return
	}
	if err := dmThreadsRepo.DeleteByChannelIdentity(r.Context(), channelID, binding.ChannelIdentityID); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if err := linksRepo.DeleteBinding(r.Context(), accountID, channelID, bindingID); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	remainingBindings, err := linksRepo.ListBindingsByIdentity(r.Context(), binding.ChannelIdentityID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if len(remainingBindings) == 0 {
		if err := channelIdentitiesRepo.WithTx(tx).UpdateUserID(r.Context(), binding.ChannelIdentityID, nil); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
	}
	if binding.IsOwner || binding.HeartbeatEnabled {
		triggerRepo := data.ScheduledTriggersRepository{}
		var triggerErr error
		if binding.IsOwner {
			triggerErr = triggerRepo.DeleteHeartbeatsByChannel(r.Context(), tx, binding.ChannelID)
		} else {
			triggerErr = triggerRepo.DeleteHeartbeat(r.Context(), tx, binding.ChannelID, binding.ChannelIdentityID)
		}
		if triggerErr != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func upsertChannelHeartbeatTarget(
	ctx context.Context,
	tx pgx.Tx,
	ch data.Channel,
	persona data.Persona,
	platformChatID string,
	model string,
	intervalMinutes int,
	channelIdentitiesRepo *data.ChannelIdentitiesRepository,
	channelGroupThreadsRepo *data.ChannelGroupThreadsRepository,
	threadRepo *data.ThreadRepository,
	personasRepo *data.PersonasRepository,
) error {
	platformChatID = strings.TrimSpace(platformChatID)
	if platformChatID == "" {
		return fmt.Errorf("heartbeat target platform chat id is required")
	}
	groupIdentity, err := channelIdentitiesRepo.WithTx(tx).Upsert(ctx, ch.ChannelType, platformChatID, nil, nil, nil)
	if err != nil {
		return err
	}

	projectID := derefUUID(persona.ProjectID)
	if projectID == uuid.Nil {
		ownerUserID := channelOwnerUserID(ch)
		if ownerUserID == nil || *ownerUserID == uuid.Nil {
			return fmt.Errorf("channel owner is required")
		}
		projectID, err = personasRepo.WithTx(tx).GetOrCreateDefaultProjectIDByOwner(ctx, ch.AccountID, *ownerUserID)
		if err != nil {
			return err
		}
	}

	groupRepo := channelGroupThreadsRepo.WithTx(tx)
	threadRepoTx := threadRepo.WithTx(tx)
	threadMap, err := groupRepo.GetByBinding(ctx, ch.ID, platformChatID, persona.ID)
	if err != nil {
		return err
	}
	if threadMap != nil {
		if existing, _ := threadRepoTx.GetByID(ctx, threadMap.ThreadID); existing != nil {
			return data.ScheduledTriggersRepository{}.UpsertHeartbeat(ctx, tx, ch.AccountID, ch.ID, groupIdentity.ID, persona.PersonaKey, model, intervalMinutes)
		}
		_ = groupRepo.DeleteByBinding(ctx, ch.ID, platformChatID, persona.ID)
	}

	title := heartbeatTargetThreadTitle(ch.ChannelType, platformChatID)
	thread, err := threadRepoTx.Create(ctx, ch.AccountID, channelOwnerUserID(ch), projectID, &title, false)
	if err != nil {
		return err
	}
	_, _ = threadRepoTx.UpdateFields(ctx, thread.ID, data.ThreadUpdateFields{
		SetTitleLocked: true,
		TitleLocked:    true,
	})
	if _, err := groupRepo.Create(ctx, ch.ID, platformChatID, persona.ID, thread.ID); err != nil {
		return err
	}
	return data.ScheduledTriggersRepository{}.UpsertHeartbeat(ctx, tx, ch.AccountID, ch.ID, groupIdentity.ID, persona.PersonaKey, model, intervalMinutes)
}

func heartbeatTargetThreadTitle(channelType, platformChatID string) string {
	switch strings.TrimSpace(channelType) {
	case "qq":
		return "QQ群 " + platformChatID
	case "telegram":
		return "Telegram 群 " + platformChatID
	case "discord":
		return "Discord 频道 " + platformChatID
	case "weixin":
		return "微信群 " + platformChatID
	default:
		return strings.TrimSpace(channelType) + " " + platformChatID
	}
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
		HeartbeatTargetCount:     item.HeartbeatTargetCount,
	}
}

func marshalChannelBindingResponse(item data.ChannelBinding) json.RawMessage {
	body, _ := json.Marshal(toChannelBindingResponse(item))
	return body
}
