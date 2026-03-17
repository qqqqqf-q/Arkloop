package accountapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	apiKeysRepo *data.APIKeysRepository,
	secretsRepo *data.SecretsRepository,
	pool data.DB,
	appBaseURL string,
) func(nethttp.ResponseWriter, *nethttp.Request) {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodPost:
			createChannel(w, r, authService, membershipRepo, channelsRepo, apiKeysRepo, secretsRepo, pool, appBaseURL)
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
	apiKeysRepo *data.APIKeysRepository,
	secretsRepo *data.SecretsRepository,
	pool data.DB,
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
			updateChannel(w, r, traceID, channelID, authService, membershipRepo, channelsRepo, apiKeysRepo, secretsRepo, pool)
		case nethttp.MethodDelete:
			deleteChannel(w, r, traceID, channelID, authService, membershipRepo, channelsRepo, apiKeysRepo, secretsRepo, pool)
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
	apiKeysRepo *data.APIKeysRepository,
	secretsRepo *data.SecretsRepository,
	pool data.DB,
	appBaseURL string,
) {
	traceID := observability.TraceIDFromContext(r.Context())
	if authService == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if channelsRepo == nil || pool == nil {
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

	var personaID *uuid.UUID
	if req.PersonaID != nil {
		pid, err := uuid.Parse(*req.PersonaID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid persona_id", traceID, nil)
			return
		}
		personaID = &pid
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

	ch, err := channelsRepo.WithTx(tx).Create(r.Context(), actor.AccountID, req.ChannelType, personaID, credentialsID, webhookSecret, webhookURL, req.ConfigJSON)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
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
	apiKeysRepo *data.APIKeysRepository,
	secretsRepo *data.SecretsRepository,
	pool data.DB,
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

	upd := data.ChannelUpdate{
		IsActive:   req.IsActive,
		ConfigJSON: req.ConfigJSON,
	}

	if req.PersonaID != nil {
		raw := strings.TrimSpace(*req.PersonaID)
		if raw == "" {
			var nilUUID *uuid.UUID
			upd.PersonaID = &nilUUID
		} else {
			pid, err := uuid.Parse(raw)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "invalid persona_id", traceID, nil)
				return
			}
			pp := &pid
			upd.PersonaID = &pp
		}
	}

	if req.BotToken != nil && secretsRepo != nil && pool != nil {
		token := strings.TrimSpace(*req.BotToken)
		if token != "" {
			secret, err := secretsRepo.Upsert(r.Context(), actor.UserID, data.ChannelSecretName(channelID), token)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			cp := &secret.ID
			upd.CredentialsID = &cp
		}
	}

	updated, err := channelsRepo.Update(r.Context(), channelID, actor.AccountID, upd)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if updated == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "channels.not_found", "channel not found", traceID, nil)
		return
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

	if ch.CredentialsID != nil && secretsRepo != nil {
		_ = secretsRepo.Delete(r.Context(), actor.UserID, data.ChannelSecretName(channelID))
	}

	if err := channelsRepo.Delete(r.Context(), channelID, actor.AccountID); err != nil {
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
