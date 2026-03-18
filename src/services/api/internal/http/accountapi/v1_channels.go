package accountapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	nethttp "net/http"
	"strings"
	"time"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	httpkit "arkloop/services/api/internal/http/httpkit"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/telegrambot"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var validChannelTypes = map[string]struct{}{
	"telegram": {},
	"discord":  {},
	"feishu":   {},
}

type channelResponse struct {
	ID          string          `json:"id"`
	AccountID   string          `json:"account_id"`
	ChannelType string          `json:"channel_type"`
	PersonaID   *string         `json:"persona_id"`
	WebhookURL  *string         `json:"webhook_url"`
	IsActive    bool            `json:"is_active"`
	ConfigJSON  json.RawMessage `json:"config_json"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

type createChannelRequest struct {
	ChannelType string          `json:"channel_type"`
	BotToken    string          `json:"bot_token"`
	PersonaID   *string         `json:"persona_id"`
	ConfigJSON  json.RawMessage `json:"config_json"`
}

type updateChannelRequest struct {
	BotToken   *string          `json:"bot_token"`
	PersonaID  *string          `json:"persona_id"`
	IsActive   *bool            `json:"is_active"`
	ConfigJSON *json.RawMessage `json:"config_json"`
}

func channelsEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	channelsRepo *data.ChannelsRepository,
	personasRepo *data.PersonasRepository,
	apiKeysRepo *data.APIKeysRepository,
	secretsRepo *data.SecretsRepository,
	pool data.DB,
	appBaseURL string,
	telegramClient *telegrambot.Client,
	telegramMode string,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodPost:
			createChannel(w, r, authService, membershipRepo, channelsRepo, personasRepo, apiKeysRepo, secretsRepo, pool, appBaseURL, telegramClient, telegramMode)
		case nethttp.MethodGet:
			listChannels(w, r, authService, membershipRepo, channelsRepo, apiKeysRepo)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func channelEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	channelsRepo *data.ChannelsRepository,
	personasRepo *data.PersonasRepository,
	apiKeysRepo *data.APIKeysRepository,
	secretsRepo *data.SecretsRepository,
	pool data.DB,
	telegramClient *telegrambot.Client,
	telegramMode string,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())

		tail := strings.TrimPrefix(r.URL.Path, "/v1/channels/")
		tail = strings.Trim(tail, "/")
		if tail == "" {
			httpkit.WriteNotFound(w, r)
			return
		}

		channelID, err := uuid.Parse(tail)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid channel id", traceID, nil)
			return
		}

		switch r.Method {
		case nethttp.MethodGet:
			getChannel(w, r, traceID, channelID, authService, membershipRepo, channelsRepo, apiKeysRepo)
		case nethttp.MethodPatch:
			updateChannel(w, r, traceID, channelID, authService, membershipRepo, channelsRepo, personasRepo, apiKeysRepo, secretsRepo, pool, telegramClient, telegramMode)
		case nethttp.MethodDelete:
			deleteChannel(w, r, traceID, channelID, authService, membershipRepo, channelsRepo, apiKeysRepo, secretsRepo, pool, telegramClient, telegramMode)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func createChannel(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	channelsRepo *data.ChannelsRepository,
	personasRepo *data.PersonasRepository,
	apiKeysRepo *data.APIKeysRepository,
	secretsRepo *data.SecretsRepository,
	pool data.DB,
	appBaseURL string,
	telegramClient *telegrambot.Client,
	telegramMode string,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if channelsRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermDataChannelsManage, w, traceID) {
		return
	}

	var req createChannelRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	req.ChannelType = strings.TrimSpace(strings.ToLower(req.ChannelType))
	if _, ok := validChannelTypes[req.ChannelType]; !ok {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "unsupported channel_type", traceID, nil)
		return
	}

	req.BotToken = strings.TrimSpace(req.BotToken)
	if req.BotToken == "" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "bot_token must not be empty", traceID, nil)
		return
	}

	normalizedConfig, _, err := normalizeChannelConfigJSON(req.ChannelType, req.ConfigJSON)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
		return
	}

	var personaID *uuid.UUID
	if req.PersonaID != nil {
		pid, err := uuid.Parse(*req.PersonaID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid persona_id", traceID, nil)
			return
		}
		personaID = &pid
		if personasRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
			return
		}
		persona, err := personasRepo.GetByIDForAccount(r.Context(), actor.AccountID, pid)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if persona == nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "persona not found", traceID, nil)
			return
		}
	}
	if req.ChannelType == "telegram" && personaID != nil {
		resolvedPersonaID, err := ensureProjectScopedChannelPersona(r.Context(), personasRepo, actor.AccountID, actor.UserID, personaID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		personaID = resolvedPersonaID
	}

	existing, err := channelsRepo.GetByAccountAndType(r.Context(), actor.AccountID, req.ChannelType)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if existing != nil {
		httpkit.WriteError(w, nethttp.StatusConflict, "channels.duplicate", "channel already exists for this platform", traceID, nil)
		return
	}

	channelID := uuid.New()
	webhookSecret, err := generateChannelWebhookSecret()
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	webhookURL := buildWebhookURL(appBaseURL, req.ChannelType, channelID)

	tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	var credentialsID *uuid.UUID
	if secretsRepo != nil {
		secret, err := secretsRepo.WithTx(tx).Create(r.Context(), actor.UserID, data.ChannelSecretName(channelID), req.BotToken)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		credentialsID = &secret.ID
	}

	ch, err := channelsRepo.WithTx(tx).Create(r.Context(), channelID, actor.AccountID, req.ChannelType, personaID, credentialsID, webhookSecret, webhookURL, normalizedConfig)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	if req.ChannelType == "telegram" && ch.IsActive {
		if _, _, _, err := mustValidateTelegramActivation(r.Context(), actor.AccountID, personasRepo, ch.PersonaID, ch.ConfigJSON); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	if req.ChannelType == "telegram" && ch.IsActive {
		if err := configureTelegramActivationRemote(r.Context(), telegramClient, req.BotToken, ch, telegramMode); err != nil {
			falseVal := false
			_, _ = channelsRepo.Update(r.Context(), channelID, actor.AccountID, data.ChannelUpdate{IsActive: &falseVal})
			httpkit.WriteError(w, nethttp.StatusBadGateway, "channels.telegram_remote_failed", err.Error(), traceID, nil)
			return
		}
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toChannelResponse(ch))
}

func listChannels(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	channelsRepo *data.ChannelsRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if channelsRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermDataChannelsManage, w, traceID) {
		return
	}

	channels, err := channelsRepo.ListByAccount(r.Context(), actor.AccountID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	resp := make([]channelResponse, 0, len(channels))
	for _, ch := range channels {
		resp = append(resp, toChannelResponse(ch))
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
}

func getChannel(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	channelID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	channelsRepo *data.ChannelsRepository,
	apiKeysRepo *data.APIKeysRepository,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if channelsRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermDataChannelsManage, w, traceID) {
		return
	}

	ch, err := channelsRepo.GetByID(r.Context(), channelID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if ch == nil || ch.AccountID != actor.AccountID {
		httpkit.WriteError(w, nethttp.StatusNotFound, "channels.not_found", "channel not found", traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toChannelResponse(*ch))
}

func updateChannel(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	channelID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	channelsRepo *data.ChannelsRepository,
	personasRepo *data.PersonasRepository,
	apiKeysRepo *data.APIKeysRepository,
	secretsRepo *data.SecretsRepository,
	pool data.DB,
	telegramClient *telegrambot.Client,
	telegramMode string,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if channelsRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermDataChannelsManage, w, traceID) {
		return
	}

	ch, err := channelsRepo.GetByID(r.Context(), channelID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if ch == nil || ch.AccountID != actor.AccountID {
		httpkit.WriteError(w, nethttp.StatusNotFound, "channels.not_found", "channel not found", traceID, nil)
		return
	}

	var req updateChannelRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	upd := data.ChannelUpdate{IsActive: req.IsActive}

	desiredPersonaID := ch.PersonaID
	desiredConfigJSON := ch.ConfigJSON
	desiredIsActive := ch.IsActive
	if req.IsActive != nil {
		desiredIsActive = *req.IsActive
	}

	if req.PersonaID != nil {
		raw := strings.TrimSpace(*req.PersonaID)
		if raw == "" {
			var nilUUID *uuid.UUID
			upd.PersonaID = &nilUUID
			desiredPersonaID = nil
		} else {
			pid, err := uuid.Parse(raw)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid persona_id", traceID, nil)
				return
			}
			pp := &pid
			upd.PersonaID = &pp
			desiredPersonaID = pp
			if personasRepo == nil {
				httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
				return
			}
			persona, err := personasRepo.GetByIDForAccount(r.Context(), actor.AccountID, pid)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if persona == nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "persona not found", traceID, nil)
				return
			}
		}
	}

	if req.ConfigJSON != nil {
		normalizedConfig, _, err := normalizeChannelConfigJSON(ch.ChannelType, *req.ConfigJSON)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
			return
		}
		upd.ConfigJSON = &normalizedConfig
		desiredConfigJSON = normalizedConfig
	}
	if ch.ChannelType == "telegram" && desiredPersonaID != nil {
		resolvedPersonaID, err := ensureProjectScopedChannelPersona(r.Context(), personasRepo, actor.AccountID, actor.UserID, desiredPersonaID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if resolvedPersonaID != nil && *resolvedPersonaID != derefUUID(desiredPersonaID) {
			desiredPersonaID = resolvedPersonaID
			upd.PersonaID = &resolvedPersonaID
		}
	}

	var nextToken string
	if req.BotToken != nil {
		nextToken = strings.TrimSpace(*req.BotToken)
	}
	if nextToken == "" && ch.CredentialsID != nil && secretsRepo != nil {
		currentToken, err := secretsRepo.DecryptByID(r.Context(), *ch.CredentialsID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if currentToken != nil {
			nextToken = strings.TrimSpace(*currentToken)
		}
	}

	tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	if req.BotToken != nil && strings.TrimSpace(*req.BotToken) != "" {
		secret, err := secretsRepo.WithTx(tx).Upsert(r.Context(), actor.UserID, data.ChannelSecretName(channelID), strings.TrimSpace(*req.BotToken))
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		cp := &secret.ID
		upd.CredentialsID = &cp
	}

	// Validate telegram activation requirements before the transaction.
	needsActivate := false
	needsDeactivate := false
	var desiredChannel data.Channel

	if ch.ChannelType == "telegram" && desiredIsActive {
		desiredChannel = *ch
		desiredChannel.PersonaID = desiredPersonaID
		desiredChannel.ConfigJSON = desiredConfigJSON
		desiredChannel.IsActive = true
		if _, _, _, err := mustValidateTelegramActivation(r.Context(), actor.AccountID, personasRepo, desiredPersonaID, desiredConfigJSON); err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
			return
		}
		if nextToken == "" {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "telegram channel requires bot_token before activation", traceID, nil)
			return
		}
		needsActivate = !ch.IsActive || (req.BotToken != nil && strings.TrimSpace(*req.BotToken) != "")
	}

	if ch.ChannelType == "telegram" && ch.IsActive && !desiredIsActive {
		if nextToken == "" {
			httpkit.WriteError(w, nethttp.StatusBadGateway, "channels.telegram_remote_failed", "telegram token unavailable", traceID, nil)
			return
		}
		needsDeactivate = true
	}

	updated, err := channelsRepo.WithTx(tx).Update(r.Context(), channelID, actor.AccountID, upd)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if updated == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "channels.not_found", "channel not found", traceID, nil)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	// Telegram API calls happen after tx.Commit to avoid holding DB connections
	// during external network requests. On failure, roll back the channel state.
	if needsActivate {
		if err := configureTelegramActivationRemote(r.Context(), telegramClient, nextToken, desiredChannel, telegramMode); err != nil {
			falseVal := false
			_, _ = channelsRepo.Update(r.Context(), channelID, actor.AccountID, data.ChannelUpdate{IsActive: &falseVal})
			httpkit.WriteError(w, nethttp.StatusBadGateway, "channels.telegram_remote_failed", err.Error(), traceID, nil)
			return
		}
	}
	if needsDeactivate {
		if err := disableTelegramActivationRemote(r.Context(), telegramClient, nextToken, telegramMode); err != nil {
			trueVal := true
			_, _ = channelsRepo.Update(r.Context(), channelID, actor.AccountID, data.ChannelUpdate{IsActive: &trueVal})
			httpkit.WriteError(w, nethttp.StatusBadGateway, "channels.telegram_remote_failed", err.Error(), traceID, nil)
			return
		}
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toChannelResponse(*updated))
}

func deleteChannel(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	channelID uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	channelsRepo *data.ChannelsRepository,
	apiKeysRepo *data.APIKeysRepository,
	secretsRepo *data.SecretsRepository,
	pool data.DB,
	telegramClient *telegrambot.Client,
	telegramMode string,
) {
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if channelsRepo == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}

	actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, nil)
	if !ok {
		return
	}
	if !httpkit.RequirePerm(actor, auth.PermDataChannelsManage, w, traceID) {
		return
	}

	ch, err := channelsRepo.GetByID(r.Context(), channelID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if ch == nil || ch.AccountID != actor.AccountID {
		httpkit.WriteError(w, nethttp.StatusNotFound, "channels.not_found", "channel not found", traceID, nil)
		return
	}

	token := ""
	if ch.CredentialsID != nil && secretsRepo != nil {
		currentToken, err := secretsRepo.DecryptByID(r.Context(), *ch.CredentialsID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if currentToken != nil {
			token = strings.TrimSpace(*currentToken)
		}
	}

	// Best-effort Telegram cleanup before the transaction — don't block
	// the delete if Telegram API is unreachable.
	if ch.ChannelType == "telegram" && ch.IsActive && token != "" {
		_ = disableTelegramActivationRemote(r.Context(), telegramClient, token, telegramMode)
	}

	tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	if ch.CredentialsID != nil && secretsRepo != nil {
		if err := secretsRepo.WithTx(tx).Delete(r.Context(), actor.UserID, data.ChannelSecretName(channelID)); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
	}

	if err := channelsRepo.WithTx(tx).Delete(r.Context(), channelID, actor.AccountID); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func toChannelResponse(ch data.Channel) channelResponse {
	var personaID *string
	if ch.PersonaID != nil {
		s := ch.PersonaID.String()
		personaID = &s
	}
	configJSON := ch.ConfigJSON
	if configJSON == nil {
		configJSON = json.RawMessage(`{}`)
	}
	return channelResponse{
		ID:          ch.ID.String(),
		AccountID:   ch.AccountID.String(),
		ChannelType: ch.ChannelType,
		PersonaID:   personaID,
		WebhookURL:  ch.WebhookURL,
		IsActive:    ch.IsActive,
		ConfigJSON:  configJSON,
		CreatedAt:   ch.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:   ch.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func buildWebhookURL(appBaseURL, channelType string, channelID uuid.UUID) string {
	base := strings.TrimRight(appBaseURL, "/")
	if base == "" {
		base = "http://localhost:19001"
	}
	return fmt.Sprintf("%s/v1/channels/%s/%s/webhook", base, channelType, channelID.String())
}

func generateChannelWebhookSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func ensureProjectScopedChannelPersona(
	ctx context.Context,
	personasRepo *data.PersonasRepository,
	accountID uuid.UUID,
	userID uuid.UUID,
	personaID *uuid.UUID,
) (*uuid.UUID, error) {
	if personaID == nil || *personaID == uuid.Nil {
		return nil, nil
	}
	if personasRepo == nil {
		return nil, fmt.Errorf("personas repo not configured")
	}

	persona, err := personasRepo.GetByIDForAccount(ctx, accountID, *personaID)
	if err != nil {
		return nil, err
	}
	if persona == nil {
		return nil, fmt.Errorf("persona not found")
	}
	if persona.ProjectID != nil && *persona.ProjectID != uuid.Nil {
		id := persona.ID
		return &id, nil
	}

	projectID, err := personasRepo.GetOrCreateDefaultProjectIDByOwner(ctx, accountID, userID)
	if err != nil {
		return nil, err
	}
	existing, err := personasRepo.GetByKeyVersionInProject(ctx, projectID, persona.PersonaKey, persona.Version)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		id := existing.ID
		return &id, nil
	}

	cloned, err := personasRepo.CloneToProject(ctx, projectID, *persona)
	if err != nil {
		var conflict data.PersonaConflictError
		if errors.As(err, &conflict) {
			existing, getErr := personasRepo.GetByKeyVersionInProject(ctx, projectID, persona.PersonaKey, persona.Version)
			if getErr != nil {
				return nil, getErr
			}
			if existing != nil {
				id := existing.ID
				return &id, nil
			}
		}
		return nil, err
	}
	id := cloned.ID
	return &id, nil
}
