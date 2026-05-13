package catalogapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	nethttp "net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	httpkit "arkloop/services/api/internal/http/httpkit"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/mcpinstall"
	"arkloop/services/shared/mcpoauth"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const mcpOAuthFlowTTL = 10 * time.Minute

type mcpOAuthStartRequest struct {
	RedirectURI         *string `json:"redirect_uri,omitempty"`
	ResourceMetadataURL *string `json:"resource_metadata_url,omitempty"`
	Scope               *string `json:"scope,omitempty"`
	ClientID            *string `json:"client_id,omitempty"`
	ClientSecret        *string `json:"client_secret,omitempty"`
}

type mcpOAuthStartResponse struct {
	AuthorizationURL string `json:"authorization_url"`
	State            string `json:"state"`
	ExpiresAt        string `json:"expires_at"`
}

type mcpOAuthStatusResponse struct {
	State       string  `json:"state"`
	Completed   bool    `json:"completed"`
	Expired     bool    `json:"expired"`
	ExpiresAt   string  `json:"expires_at"`
	CompletedAt *string `json:"completed_at,omitempty"`
}

func mcpOAuthCallbackEntry(
	secretsRepo *data.SecretsRepository,
	installsRepo *data.ProfileMCPInstallsRepository,
	profileRepo *data.ProfileRegistriesRepository,
	pool data.DB,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		switch r.Method {
		case nethttp.MethodGet, nethttp.MethodPost:
			handleMCPOAuthCallback(w, r, traceID, secretsRepo, installsRepo, profileRepo, pool)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func startMCPOAuth(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	id uuid.UUID,
	authService *auth.Service,
	installsRepo *data.ProfileMCPInstallsRepository,
	secretsRepo *data.SecretsRepository,
	pool data.DB,
) {
	if authService == nil || installsRepo == nil || secretsRepo == nil || pool == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
	if !ok {
		return
	}
	current, err := installsRepo.GetByID(r.Context(), actor.AccountID, id)
	if err != nil || current == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "mcp_installs.not_found", "install not found", traceID, nil)
		return
	}
	if current.Transport == "stdio" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "oauth is only available for HTTP MCP transports", traceID, nil)
		return
	}
	var req mcpOAuthStartRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}

	server, err := effectiveServerConfigFromInstall(*current, mcpinstall.AuthPayload{})
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "launch_spec is invalid", traceID, nil)
		return
	}
	redirectURI := strings.TrimSpace(derefReqString(req.RedirectURI))
	if redirectURI == "" {
		redirectURI = defaultMCPOAuthRedirectURI(r)
	}
	if redirectURI == "" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "redirect_uri is required", traceID, nil)
		return
	}

	discovery, err := mcpoauth.DiscoverServerInfo(r.Context(), newEffectiveMCPHTTPClient(), server.URL, derefReqString(req.ResourceMetadataURL))
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusBadGateway, "mcp_oauth.discovery_failed", "oauth discovery failed", traceID, nil)
		return
	}
	scope := strings.TrimSpace(derefReqString(req.Scope))
	if scope == "" {
		scope = defaultMCPOAuthScope(discovery)
	}
	clientInfo, err := ensureMCPOAuthClient(r.Context(), discovery, redirectURI, scope, req)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusBadGateway, "mcp_oauth.client_registration_failed", "oauth client registration failed", traceID, nil)
		return
	}
	state, err := newMCPOAuthState()
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	authState, err := mcpoauth.StartAuthorization(discovery, clientInfo, redirectURI, scope, state)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusBadGateway, "mcp_oauth.authorization_failed", "oauth authorization failed", traceID, nil)
		return
	}

	expiresAt := time.Now().UTC().Add(mcpOAuthFlowTTL)
	flowSecret, err := saveMCPOAuthFlowSecret(r.Context(), secretsRepo, actor.UserID, state, authState)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	oauthRepo, err := data.NewMCPOAuthConnectionsRepository(pool)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if _, err := oauthRepo.CreateFlow(r.Context(), data.MCPOAuthFlow{
		AccountID:            actor.AccountID,
		ProfileRef:           current.ProfileRef,
		InstallID:            current.ID,
		State:                state,
		RedirectURI:          redirectURI,
		AuthorizationURL:     authState.AuthorizationURL,
		CodeVerifierSecretID: flowSecret.ID,
		ClientID:             stringPtrIfNotEmpty(clientInfo.ClientID),
		Scope:                stringPtrIfNotEmpty(scope),
		ExpiresAt:            expiresAt,
	}); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	clearInstallErrorAfterOAuthStart(r.Context(), installsRepo, actor.AccountID, current.ID)

	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, mcpOAuthStartResponse{
		AuthorizationURL: authState.AuthorizationURL,
		State:            state,
		ExpiresAt:        expiresAt.Format(time.RFC3339),
	})
}

func clearInstallErrorAfterOAuthStart(ctx context.Context, installsRepo *data.ProfileMCPInstallsRepository, accountID uuid.UUID, installID uuid.UUID) {
	status := data.MCPDiscoveryStatusNeedsCheck
	empty := ""
	_, _ = installsRepo.Patch(ctx, accountID, installID, data.MCPInstallPatch{
		DiscoveryStatus:  &status,
		LastErrorCode:    &empty,
		LastErrorMessage: &empty,
	})
}

func getMCPOAuthStatus(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	id uuid.UUID,
	authService *auth.Service,
	pool data.DB,
) {
	if authService == nil || pool == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
	if !ok {
		return
	}
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if state == "" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "state is required", traceID, nil)
		return
	}
	oauthRepo, err := data.NewMCPOAuthConnectionsRepository(pool)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	flow, err := oauthRepo.GetFlowByState(r.Context(), state)
	if err != nil || flow == nil || flow.AccountID != actor.AccountID || flow.InstallID != id {
		httpkit.WriteError(w, nethttp.StatusNotFound, "mcp_oauth.not_found", "oauth flow not found", traceID, nil)
		return
	}
	var completedAt *string
	if flow.CompletedAt != nil {
		value := flow.CompletedAt.UTC().Format(time.RFC3339)
		completedAt = &value
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, mcpOAuthStatusResponse{
		State:       flow.State,
		Completed:   flow.CompletedAt != nil,
		Expired:     !time.Now().Before(flow.ExpiresAt),
		ExpiresAt:   flow.ExpiresAt.UTC().Format(time.RFC3339),
		CompletedAt: completedAt,
	})
}

func handleMCPOAuthCallback(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	secretsRepo *data.SecretsRepository,
	installsRepo *data.ProfileMCPInstallsRepository,
	profileRepo *data.ProfileRegistriesRepository,
	pool data.DB,
) {
	if secretsRepo == nil || installsRepo == nil || profileRepo == nil || pool == nil {
		httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "database.not_configured", "database not configured", traceID, nil)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if r.Method == nethttp.MethodPost && (code == "" || state == "") {
		var body struct {
			Code  string `json:"code"`
			State string `json:"state"`
		}
		if err := httpkit.DecodeJSON(r, &body); err == nil {
			code = strings.TrimSpace(body.Code)
			state = strings.TrimSpace(body.State)
		}
	}
	if oauthErr := strings.TrimSpace(r.URL.Query().Get("error")); oauthErr != "" {
		httpkit.WriteError(w, nethttp.StatusBadRequest, "mcp_oauth.denied", oauthErr, traceID, nil)
		return
	}
	if code == "" || state == "" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "code and state are required", traceID, nil)
		return
	}

	oauthRepo, err := data.NewMCPOAuthConnectionsRepository(pool)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	flow, err := oauthRepo.GetFlowByState(r.Context(), state)
	if err != nil || flow == nil || flow.CompletedAt != nil || !time.Now().Before(flow.ExpiresAt) {
		httpkit.WriteError(w, nethttp.StatusUnauthorized, "mcp_oauth.invalid_state", "invalid oauth state", traceID, nil)
		return
	}
	authState, err := loadMCPOAuthFlowState(r.Context(), secretsRepo, flow.CodeVerifierSecretID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if authState.State != state {
		httpkit.WriteError(w, nethttp.StatusUnauthorized, "mcp_oauth.invalid_state", "invalid oauth state", traceID, nil)
		return
	}
	tokens, err := mcpoauth.ExchangeAuthorization(r.Context(), newEffectiveMCPHTTPClient(), authState.Discovery, authState.Client, code, authState.CodeVerifier, flow.RedirectURI)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusBadGateway, "mcp_oauth.exchange_failed", "oauth token exchange failed", traceID, nil)
		return
	}
	authState.Tokens = tokens
	authState.CodeVerifier = ""

	profile, err := profileRepo.Get(r.Context(), flow.ProfileRef)
	if err != nil || profile == nil || profile.OwnerUserID == nil || *profile.OwnerUserID == uuid.Nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	install, err := installsRepo.GetByID(r.Context(), flow.AccountID, flow.InstallID)
	if err != nil || install == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "mcp_installs.not_found", "install not found", traceID, nil)
		return
	}

	tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	txSecrets := secretsRepo.WithTx(tx)
	txInstalls := installsRepo.WithTx(tx)
	txOAuth := oauthRepo.WithTx(tx)
	payload := &mcpinstall.AuthPayload{OAuth: &authState}
	secretID, err := upsertMCPAuthHeadersSecret(r.Context(), txSecrets, *profile.OwnerUserID, install.InstallKey, payload)
	if err != nil || secretID == uuid.Nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if _, err := txInstalls.Patch(r.Context(), flow.AccountID, flow.InstallID, data.MCPInstallPatch{
		AuthHeadersSecretID: &secretID,
		DiscoveryStatus:     stringPtrIfNotEmpty(data.MCPDiscoveryStatusNeedsCheck),
	}); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	conn, err := txOAuth.UpsertConnection(r.Context(), data.MCPOAuthConnection{
		AccountID:     flow.AccountID,
		ProfileRef:    flow.ProfileRef,
		InstallID:     flow.InstallID,
		TokenSecretID: secretID,
		ClientID:      stringPtrIfNotEmpty(authState.Client.ClientID),
		Scope:         stringPtrIfNotEmpty(authState.Scope),
		ExpiresAt:     tokenExpiryPtr(authState.Tokens),
	})
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if _, err := txOAuth.CompleteFlow(r.Context(), state, conn.ID); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}

	notifyMCPChanged(r.Context(), pool, flow.AccountID)
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func ensureMCPOAuthClient(ctx context.Context, discovery mcpoauth.DiscoveryState, redirectURI string, scope string, req mcpOAuthStartRequest) (mcpoauth.ClientInformation, error) {
	if clientID := strings.TrimSpace(derefReqString(req.ClientID)); clientID != "" {
		return mcpoauth.ClientInformation{
			ClientMetadata: mcpoauth.ClientMetadata{
				RedirectURIs:            []string{redirectURI},
				TokenEndpointAuthMethod: mcpoauth.ClientAuthNone,
				GrantTypes:              []string{"authorization_code", "refresh_token"},
				ResponseTypes:           []string{"code"},
				ClientName:              "Arkloop",
				Scope:                   scope,
			},
			ClientID:     clientID,
			ClientSecret: strings.TrimSpace(derefReqString(req.ClientSecret)),
		}, nil
	}
	return mcpoauth.RegisterClient(ctx, newEffectiveMCPHTTPClient(), discovery, mcpoauth.ClientMetadata{
		RedirectURIs:            []string{redirectURI},
		TokenEndpointAuthMethod: mcpoauth.ClientAuthNone,
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		ClientName:              "Arkloop",
		Scope:                   scope,
	}, scope)
}

func saveMCPOAuthFlowSecret(ctx context.Context, repo *data.SecretsRepository, userID uuid.UUID, state string, authState mcpoauth.AuthState) (data.Secret, error) {
	encoded, err := json.Marshal(authState)
	if err != nil {
		return data.Secret{}, err
	}
	return repo.Upsert(ctx, userID, "mcp_oauth_flow:"+state, string(encoded))
}

func loadMCPOAuthFlowState(ctx context.Context, repo *data.SecretsRepository, secretID uuid.UUID) (mcpoauth.AuthState, error) {
	plain, err := repo.DecryptByID(ctx, secretID)
	if err != nil {
		return mcpoauth.AuthState{}, err
	}
	if plain == nil {
		return mcpoauth.AuthState{}, fmt.Errorf("mcp oauth flow secret not found")
	}
	var state mcpoauth.AuthState
	if err := json.Unmarshal([]byte(*plain), &state); err != nil {
		return mcpoauth.AuthState{}, err
	}
	return state, nil
}

func defaultMCPOAuthRedirectURI(r *nethttp.Request) string {
	if base := strings.TrimSpace(os.Getenv("ARKLOOP_MCP_OAUTH_REDIRECT_BASE_URL")); base != "" {
		return strings.TrimRight(base, "/") + "/v1/mcp-oauth/callback"
	}
	scheme := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}
	if host == "" {
		return ""
	}
	return (&url.URL{Scheme: scheme, Host: host, Path: "/v1/mcp-oauth/callback"}).String()
}

func defaultMCPOAuthScope(discovery mcpoauth.DiscoveryState) string {
	if discovery.ResourceMetadata == nil {
		return ""
	}
	return strings.Join(discovery.ResourceMetadata.ScopesSupported, " ")
}

func newMCPOAuthState() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func stringPtrIfNotEmpty(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func tokenExpiryPtr(tokens mcpoauth.Tokens) *time.Time {
	if tokens.ExpiresAt.IsZero() {
		return nil
	}
	value := tokens.ExpiresAt.UTC()
	return &value
}
