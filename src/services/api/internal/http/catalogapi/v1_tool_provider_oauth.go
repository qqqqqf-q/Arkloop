package catalogapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	nethttp "net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	httpkit "arkloop/services/api/internal/http/httpkit"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	toolProviderOAuthFlowTTL = 10 * time.Minute
	xaiOAuthClientID         = "b1a00492-073a-47ea-816f-4c329264a828"
	xaiOAuthIssuer           = "https://auth.x.ai"
	xaiOAuthScope            = "openid profile email offline_access grok-cli:access api:access"
	xaiOAuthLoopbackAddr     = "127.0.0.1:56121"
	xaiOAuthLoopbackCallback = "http://127.0.0.1:56121/callback"
)

var toolProviderOAuthLoopback struct {
	mu      sync.Mutex
	started bool
}

type toolProviderOAuthStartRequest struct {
	RedirectURI *string `json:"redirect_uri,omitempty"`
	Scope       *string `json:"scope,omitempty"`
	ClientID    *string `json:"client_id,omitempty"`
}

type toolProviderOAuthStartResponse struct {
	AuthorizationURL string `json:"authorization_url"`
	State            string `json:"state"`
	ExpiresAt        string `json:"expires_at"`
}

type toolProviderOAuthStatusResponse struct {
	State       string  `json:"state"`
	Completed   bool    `json:"completed"`
	Expired     bool    `json:"expired"`
	ExpiresAt   string  `json:"expires_at"`
	CompletedAt *string `json:"completed_at,omitempty"`
}

type xaiOAuthDiscovery struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	Issuer                string `json:"issuer"`
}

type toolProviderOAuthFlowSecret struct {
	CodeVerifier  string            `json:"code_verifier"`
	CodeChallenge string            `json:"code_challenge"`
	TokenEndpoint string            `json:"token_endpoint"`
	Extra         map[string]string `json:"extra,omitempty"`
}

type xaiTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	Scope        string `json:"scope,omitempty"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	ObtainedAt   string `json:"obtained_at"`
}

func toolProviderOAuthCallbackEntry(
	secretsRepo *data.SecretsRepository,
	pool data.DB,
	directPool *pgxpool.Pool,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		switch r.Method {
		case nethttp.MethodGet, nethttp.MethodPost:
			handleToolProviderOAuthCallback(w, r, traceID, secretsRepo, pool, directPool)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func startToolProviderOAuth(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	groupName string,
	providerName string,
	authService *auth.Service,
	secretsRepo *data.SecretsRepository,
	pool data.DB,
	projectRepo *data.ProjectRepository,
) {
	if authService == nil || secretsRepo == nil || pool == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	if groupName != "x_search" || providerName != "x_search.xai" {
		httpkit.WriteNotFound(w, r)
		return
	}
	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
	if !ok {
		return
	}
	ownerKind, ownerUserID, ok := resolveToolProviderScope(r.Context(), w, r, traceID, actor, projectRepo)
	if !ok {
		return
	}
	var req toolProviderOAuthStartRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}
	clientID := strings.TrimSpace(derefReqString(req.ClientID))
	if clientID == "" {
		clientID = xaiOAuthClientID
	}
	redirectURI := strings.TrimSpace(derefReqString(req.RedirectURI))
	if redirectURI == "" {
		redirectURI = defaultToolProviderOAuthRedirectURI(r, clientID)
	}
	if redirectURI == "" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "redirect_uri is required", traceID, nil)
		return
	}
	if redirectURI == xaiOAuthLoopbackCallback {
		if err := ensureToolProviderOAuthLoopbackCallback(secretsRepo, pool, nil); err != nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "tool_provider_oauth.callback_unavailable", "oauth callback unavailable", traceID, nil)
			return
		}
	}
	discovery, err := discoverXAIOAuth(r.Context())
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusBadGateway, "tool_provider_oauth.discovery_failed", "oauth discovery failed", traceID, nil)
		return
	}
	state, err := newOAuthRandom()
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	verifier, challenge, err := newPKCEPair()
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	scope := strings.TrimSpace(derefReqString(req.Scope))
	if scope == "" {
		scope = xaiOAuthScope
	}
	authURL := buildXAIAuthorizationURL(discovery.AuthorizationEndpoint, clientID, redirectURI, scope, state, challenge)
	expiresAt := time.Now().UTC().Add(toolProviderOAuthFlowTTL)
	flowSecret, err := saveToolProviderOAuthFlowSecret(r.Context(), secretsRepo, ownerKind, ownerUserID, state, toolProviderOAuthFlowSecret{
		CodeVerifier:  verifier,
		CodeChallenge: challenge,
		TokenEndpoint: discovery.TokenEndpoint,
	})
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	oauthRepo, err := data.NewToolProviderOAuthRepository(pool)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if _, err := oauthRepo.CreateFlow(r.Context(), data.ToolProviderOAuthFlow{
		OwnerKind:            ownerKind,
		OwnerUserID:          ownerUserID,
		GroupName:            groupName,
		ProviderName:         providerName,
		State:                state,
		RedirectURI:          redirectURI,
		AuthorizationURL:     authURL,
		CodeVerifierSecretID: flowSecret.ID,
		ClientID:             stringPtrIfNotEmpty(clientID),
		Scope:                stringPtrIfNotEmpty(scope),
		ExpiresAt:            expiresAt,
	}); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toolProviderOAuthStartResponse{
		AuthorizationURL: authURL,
		State:            state,
		ExpiresAt:        expiresAt.Format(time.RFC3339),
	})
}

func getToolProviderOAuthStatus(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	groupName string,
	providerName string,
	authService *auth.Service,
	pool data.DB,
	projectRepo *data.ProjectRepository,
) {
	if authService == nil || pool == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
	if !ok {
		return
	}
	ownerKind, ownerUserID, ok := resolveToolProviderScope(r.Context(), w, r, traceID, actor, projectRepo)
	if !ok {
		return
	}
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if state == "" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "state is required", traceID, nil)
		return
	}
	oauthRepo, err := data.NewToolProviderOAuthRepository(pool)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	flow, err := oauthRepo.GetFlowByState(r.Context(), state)
	if err != nil || flow == nil || flow.GroupName != groupName || flow.ProviderName != providerName ||
		flow.OwnerKind != ownerKind || !sameOwnerUserID(flow.OwnerUserID, ownerUserID) {
		httpkit.WriteError(w, nethttp.StatusNotFound, "tool_provider_oauth.not_found", "oauth flow not found", traceID, nil)
		return
	}
	var completedAt *string
	if flow.CompletedAt != nil {
		value := flow.CompletedAt.UTC().Format(time.RFC3339)
		completedAt = &value
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toolProviderOAuthStatusResponse{
		State:       flow.State,
		Completed:   flow.CompletedAt != nil,
		Expired:     !time.Now().Before(flow.ExpiresAt),
		ExpiresAt:   flow.ExpiresAt.UTC().Format(time.RFC3339),
		CompletedAt: completedAt,
	})
}

func handleToolProviderOAuthCallback(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	secretsRepo *data.SecretsRepository,
	pool data.DB,
	directPool *pgxpool.Pool,
) {
	if secretsRepo == nil || pool == nil {
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
		httpkit.WriteError(w, nethttp.StatusBadRequest, "tool_provider_oauth.denied", oauthErr, traceID, nil)
		return
	}
	if code == "" || state == "" {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "code and state are required", traceID, nil)
		return
	}
	oauthRepo, err := data.NewToolProviderOAuthRepository(pool)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	flow, err := oauthRepo.GetFlowByState(r.Context(), state)
	if err != nil || flow == nil || flow.CompletedAt != nil || !time.Now().Before(flow.ExpiresAt) {
		httpkit.WriteError(w, nethttp.StatusUnauthorized, "tool_provider_oauth.invalid_state", "invalid oauth state", traceID, nil)
		return
	}
	secret, err := loadToolProviderOAuthFlowSecret(r.Context(), secretsRepo, flow.CodeVerifierSecretID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	tokens, err := exchangeXAIToken(r.Context(), secret.TokenEndpoint, derefReqString(flow.ClientID), flow.RedirectURI, code, secret)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusBadGateway, "tool_provider_oauth.exchange_failed", "oauth token exchange failed", traceID, nil)
		return
	}
	encoded, err := json.Marshal(tokens)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	txSecrets := secretsRepo.WithTx(tx)
	var tokenSecret data.Secret
	secretName := "tool_provider_oauth:" + flow.ProviderName
	if flow.OwnerKind == "platform" {
		tokenSecret, err = txSecrets.UpsertPlatform(r.Context(), secretName, string(encoded))
	} else if flow.OwnerUserID != nil {
		tokenSecret, err = txSecrets.Upsert(r.Context(), *flow.OwnerUserID, secretName, string(encoded))
	}
	if err != nil || tokenSecret.ID == uuid.Nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	txOAuth := oauthRepo.WithTx(tx)
	conn, err := txOAuth.UpsertConnection(r.Context(), data.ToolProviderOAuthConnection{
		OwnerKind:     flow.OwnerKind,
		OwnerUserID:   flow.OwnerUserID,
		GroupName:     flow.GroupName,
		ProviderName:  flow.ProviderName,
		TokenSecretID: tokenSecret.ID,
		ClientID:      flow.ClientID,
		Scope:         flow.Scope,
		ExpiresAt:     xaiTokenExpiryPtr(tokens),
	})
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if err := txOAuth.CompleteFlow(r.Context(), state, conn.ID); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	notifyPayload := "platform"
	if flow.OwnerKind == "user" && flow.OwnerUserID != nil {
		notifyPayload = flow.OwnerUserID.String()
	}
	notifyToolProviderChanged(r.Context(), directPool, pool, notifyPayload)
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func discoverXAIOAuth(ctx context.Context) (xaiOAuthDiscovery, error) {
	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, xaiOAuthIssuer+"/.well-known/openid-configuration", nil)
	if err != nil {
		return xaiOAuthDiscovery{}, err
	}
	resp, err := nethttp.DefaultClient.Do(req)
	if err != nil {
		return xaiOAuthDiscovery{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return xaiOAuthDiscovery{}, fmt.Errorf("xai discovery status %d", resp.StatusCode)
	}
	var discovery xaiOAuthDiscovery
	if err := json.Unmarshal(body, &discovery); err != nil {
		return xaiOAuthDiscovery{}, err
	}
	if strings.TrimSpace(discovery.AuthorizationEndpoint) == "" || strings.TrimSpace(discovery.TokenEndpoint) == "" {
		return xaiOAuthDiscovery{}, fmt.Errorf("xai discovery missing endpoints")
	}
	return discovery, nil
}

func buildXAIAuthorizationURL(endpoint string, clientID string, redirectURI string, scope string, state string, challenge string) string {
	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", clientID)
	values.Set("redirect_uri", redirectURI)
	values.Set("scope", scope)
	values.Set("state", state)
	values.Set("code_challenge", challenge)
	values.Set("code_challenge_method", "S256")
	values.Set("plan", "generic")
	values.Set("referrer", "arkloop")
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return endpoint + "?" + values.Encode()
	}
	parsed.RawQuery = values.Encode()
	return parsed.String()
}

func exchangeXAIToken(ctx context.Context, endpoint string, clientID string, redirectURI string, code string, secret toolProviderOAuthFlowSecret) (xaiTokenResponse, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("redirect_uri", redirectURI)
	values.Set("client_id", clientID)
	values.Set("code_verifier", secret.CodeVerifier)
	values.Set("code_challenge", secret.CodeChallenge)
	values.Set("code_challenge_method", "S256")
	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodPost, endpoint, bytes.NewBufferString(values.Encode()))
	if err != nil {
		return xaiTokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := nethttp.DefaultClient.Do(req)
	if err != nil {
		return xaiTokenResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return xaiTokenResponse{}, fmt.Errorf("xai token status %d", resp.StatusCode)
	}
	var tokens xaiTokenResponse
	if err := json.Unmarshal(body, &tokens); err != nil {
		return xaiTokenResponse{}, err
	}
	if strings.TrimSpace(tokens.AccessToken) == "" {
		return xaiTokenResponse{}, fmt.Errorf("xai token response missing access_token")
	}
	now := time.Now().UTC()
	tokens.ObtainedAt = now.Format(time.RFC3339)
	if tokens.ExpiresIn > 0 && strings.TrimSpace(tokens.ExpiresAt) == "" {
		tokens.ExpiresAt = now.Add(time.Duration(tokens.ExpiresIn) * time.Second).Format(time.RFC3339)
	}
	return tokens, nil
}

func saveToolProviderOAuthFlowSecret(ctx context.Context, repo *data.SecretsRepository, ownerKind string, ownerUserID *uuid.UUID, state string, secret toolProviderOAuthFlowSecret) (data.Secret, error) {
	encoded, err := json.Marshal(secret)
	if err != nil {
		return data.Secret{}, err
	}
	name := "tool_provider_oauth_flow:" + state
	if ownerKind == "platform" {
		return repo.UpsertPlatform(ctx, name, string(encoded))
	}
	if ownerUserID == nil {
		return data.Secret{}, fmt.Errorf("owner_user_id is required")
	}
	return repo.Upsert(ctx, *ownerUserID, name, string(encoded))
}

func loadToolProviderOAuthFlowSecret(ctx context.Context, repo *data.SecretsRepository, secretID uuid.UUID) (toolProviderOAuthFlowSecret, error) {
	plain, err := repo.DecryptByID(ctx, secretID)
	if err != nil {
		return toolProviderOAuthFlowSecret{}, err
	}
	if plain == nil {
		return toolProviderOAuthFlowSecret{}, fmt.Errorf("tool provider oauth flow secret not found")
	}
	var secret toolProviderOAuthFlowSecret
	if err := json.Unmarshal([]byte(*plain), &secret); err != nil {
		return toolProviderOAuthFlowSecret{}, err
	}
	return secret, nil
}

func defaultToolProviderOAuthRedirectURI(r *nethttp.Request, clientID string) string {
	if base := strings.TrimSpace(os.Getenv("ARKLOOP_TOOL_PROVIDER_OAUTH_REDIRECT_BASE_URL")); base != "" {
		return strings.TrimRight(base, "/") + "/v1/tool-provider-oauth/callback"
	}
	if strings.TrimSpace(clientID) == xaiOAuthClientID {
		return xaiOAuthLoopbackCallback
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
	return (&url.URL{Scheme: scheme, Host: host, Path: "/v1/tool-provider-oauth/callback"}).String()
}

func ensureToolProviderOAuthLoopbackCallback(
	secretsRepo *data.SecretsRepository,
	pool data.DB,
	directPool *pgxpool.Pool,
) error {
	toolProviderOAuthLoopback.mu.Lock()
	defer toolProviderOAuthLoopback.mu.Unlock()
	if toolProviderOAuthLoopback.started {
		return nil
	}
	listener, err := net.Listen("tcp", xaiOAuthLoopbackAddr)
	if err != nil {
		return err
	}
	mux := nethttp.NewServeMux()
	mux.HandleFunc("/callback", toolProviderOAuthCallbackEntry(secretsRepo, pool, directPool))
	server := &nethttp.Server{Handler: mux}
	toolProviderOAuthLoopback.started = true
	go func() {
		if err := server.Serve(listener); err != nil && err != nethttp.ErrServerClosed {
			toolProviderOAuthLoopback.mu.Lock()
			toolProviderOAuthLoopback.started = false
			toolProviderOAuthLoopback.mu.Unlock()
		}
	}()
	return nil
}

func newPKCEPair() (string, string, error) {
	verifier, err := newOAuthRandom()
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

func newOAuthRandom() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func sameOwnerUserID(left *uuid.UUID, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func xaiTokenExpiryPtr(tokens xaiTokenResponse) *time.Time {
	if tokens.ExpiresAt == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, tokens.ExpiresAt)
	if err != nil {
		return nil
	}
	value := parsed.UTC()
	return &value
}
