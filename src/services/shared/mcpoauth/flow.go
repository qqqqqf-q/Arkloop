package mcpoauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func RegisterClient(ctx context.Context, httpClient HTTPDoer, discovery DiscoveryState, clientMetadata ClientMetadata, scope string) (ClientInformation, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	endpoint, err := registrationEndpoint(discovery)
	if err != nil {
		return ClientInformation{}, err
	}
	if strings.TrimSpace(scope) != "" {
		clientMetadata.Scope = strings.TrimSpace(scope)
	}

	body, err := json.Marshal(clientMetadata)
	if err != nil {
		return ClientInformation{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return ClientInformation{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return ClientInformation{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if !respOK(resp.StatusCode) {
		return ClientInformation{}, tokenEndpointError(resp)
	}

	var info ClientInformation
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return ClientInformation{}, fmt.Errorf("mcp oauth: decode client information: %w", err)
	}
	if strings.TrimSpace(info.ClientID) == "" {
		return ClientInformation{}, fmt.Errorf("mcp oauth: registered client missing client_id")
	}
	return info, nil
}

func StartAuthorization(discovery DiscoveryState, clientInformation ClientInformation, redirectURI string, scope string, state string) (AuthState, error) {
	if strings.TrimSpace(clientInformation.ClientID) == "" {
		return AuthState{}, fmt.Errorf("mcp oauth: client_id must not be empty")
	}
	if strings.TrimSpace(redirectURI) == "" {
		return AuthState{}, fmt.Errorf("mcp oauth: redirect uri must not be empty")
	}

	endpoint, err := authorizationEndpoint(discovery)
	if err != nil {
		return AuthState{}, err
	}
	if discovery.AuthorizationServerMetadata != nil {
		if !supportsValue(discovery.AuthorizationServerMetadata.ResponseTypesSupported, "code", true) {
			return AuthState{}, fmt.Errorf("mcp oauth: authorization server does not support code response type")
		}
		if len(discovery.AuthorizationServerMetadata.CodeChallengeMethodsSupported) > 0 &&
			!supportsValue(discovery.AuthorizationServerMetadata.CodeChallengeMethodsSupported, "S256", false) {
			return AuthState{}, fmt.Errorf("mcp oauth: authorization server does not support S256 PKCE")
		}
	}

	verifier, challenge, err := newPKCEPair()
	if err != nil {
		return AuthState{}, err
	}
	authURL, err := url.Parse(endpoint)
	if err != nil {
		return AuthState{}, err
	}
	query := authURL.Query()
	query.Set("response_type", "code")
	query.Set("client_id", clientInformation.ClientID)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	query.Set("redirect_uri", redirectURI)
	if strings.TrimSpace(state) != "" {
		query.Set("state", state)
	}
	if strings.TrimSpace(scope) != "" {
		query.Set("scope", strings.TrimSpace(scope))
		if hasScope(scope, "offline_access") {
			query.Add("prompt", "consent")
		}
	}
	if resource := resourceParameter(discovery); resource != "" {
		query.Set("resource", resource)
	}
	authURL.RawQuery = query.Encode()

	return AuthState{
		Discovery:        discovery,
		Client:           clientInformation,
		CodeVerifier:     verifier,
		State:            state,
		RedirectURI:      redirectURI,
		Scope:            strings.TrimSpace(scope),
		AuthorizationURL: authURL.String(),
	}, nil
}

func ExchangeAuthorization(ctx context.Context, httpClient HTTPDoer, discovery DiscoveryState, clientInformation ClientInformation, authorizationCode string, codeVerifier string, redirectURI string) (Tokens, error) {
	params := url.Values{}
	params.Set("grant_type", "authorization_code")
	params.Set("code", authorizationCode)
	params.Set("code_verifier", codeVerifier)
	params.Set("redirect_uri", redirectURI)
	return executeTokenRequest(ctx, httpClient, discovery, clientInformation, params, "")
}

func RefreshAuthorization(ctx context.Context, httpClient HTTPDoer, discovery DiscoveryState, clientInformation ClientInformation, refreshToken string) (Tokens, error) {
	params := url.Values{}
	params.Set("grant_type", "refresh_token")
	params.Set("refresh_token", refreshToken)
	tokens, err := executeTokenRequest(ctx, httpClient, discovery, clientInformation, params, refreshToken)
	if err != nil {
		return Tokens{}, err
	}
	return tokens, nil
}

func TokenExpiredSoon(tokens Tokens, window time.Duration) bool {
	if strings.TrimSpace(tokens.AccessToken) == "" {
		return true
	}
	expiresAt := tokens.ExpiresAt
	if expiresAt.IsZero() && tokens.ExpiresIn > 0 {
		obtainedAt := tokens.ObtainedAt
		if obtainedAt.IsZero() {
			obtainedAt = time.Now()
		}
		expiresAt = obtainedAt.Add(time.Duration(tokens.ExpiresIn) * time.Second)
	}
	if expiresAt.IsZero() {
		return false
	}
	return !time.Now().Add(window).Before(expiresAt)
}

func executeTokenRequest(ctx context.Context, httpClient HTTPDoer, discovery DiscoveryState, clientInformation ClientInformation, params url.Values, existingRefreshToken string) (Tokens, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	endpoint, err := tokenEndpoint(discovery)
	if err != nil {
		return Tokens{}, err
	}
	if resource := resourceParameter(discovery); resource != "" {
		params.Set("resource", resource)
	}

	headers := http.Header{}
	headers.Set("Content-Type", "application/x-www-form-urlencoded")
	headers.Set("Accept", "application/json")
	if err := applyClientAuthentication(discovery, clientInformation, headers, params); err != nil {
		return Tokens{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(params.Encode()))
	if err != nil {
		return Tokens{}, err
	}
	req.Header = headers

	resp, err := httpClient.Do(req)
	if err != nil {
		return Tokens{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if !respOK(resp.StatusCode) {
		return Tokens{}, tokenEndpointError(resp)
	}

	var tokens Tokens
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return Tokens{}, fmt.Errorf("mcp oauth: decode tokens: %w", err)
	}
	if strings.TrimSpace(tokens.AccessToken) == "" {
		return Tokens{}, fmt.Errorf("mcp oauth: token response missing access_token")
	}
	if tokens.ObtainedAt.IsZero() {
		tokens.ObtainedAt = time.Now().UTC()
	}
	if tokens.ExpiresAt.IsZero() && tokens.ExpiresIn > 0 {
		tokens.ExpiresAt = tokens.ObtainedAt.Add(time.Duration(tokens.ExpiresIn) * time.Second)
	}
	if strings.TrimSpace(tokens.RefreshToken) == "" {
		tokens.RefreshToken = existingRefreshToken
	}
	return tokens, nil
}

func registrationEndpoint(discovery DiscoveryState) (string, error) {
	if discovery.AuthorizationServerMetadata != nil {
		if strings.TrimSpace(discovery.AuthorizationServerMetadata.RegistrationEndpoint) == "" {
			return "", fmt.Errorf("mcp oauth: authorization server does not support dynamic client registration")
		}
		return discovery.AuthorizationServerMetadata.RegistrationEndpoint, nil
	}
	return resolveAuthServerPath(discovery.AuthorizationServerURL, "/register")
}

func authorizationEndpoint(discovery DiscoveryState) (string, error) {
	if discovery.AuthorizationServerMetadata != nil && strings.TrimSpace(discovery.AuthorizationServerMetadata.AuthorizationEndpoint) != "" {
		return discovery.AuthorizationServerMetadata.AuthorizationEndpoint, nil
	}
	return resolveAuthServerPath(discovery.AuthorizationServerURL, "/authorize")
}

func tokenEndpoint(discovery DiscoveryState) (string, error) {
	if discovery.AuthorizationServerMetadata != nil && strings.TrimSpace(discovery.AuthorizationServerMetadata.TokenEndpoint) != "" {
		return discovery.AuthorizationServerMetadata.TokenEndpoint, nil
	}
	return resolveAuthServerPath(discovery.AuthorizationServerURL, "/token")
}

func resolveAuthServerPath(authorizationServerURL string, path string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(authorizationServerURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("mcp oauth: invalid authorization server url")
	}
	parsed.Path = path
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func applyClientAuthentication(discovery DiscoveryState, clientInformation ClientInformation, headers http.Header, params url.Values) error {
	method := selectClientAuthMethod(discovery, clientInformation)
	switch method {
	case ClientAuthBasic:
		if strings.TrimSpace(clientInformation.ClientSecret) == "" {
			return fmt.Errorf("mcp oauth: client_secret_basic requires client_secret")
		}
		credentials := base64.StdEncoding.EncodeToString([]byte(clientInformation.ClientID + ":" + clientInformation.ClientSecret))
		headers.Set("Authorization", "Basic "+credentials)
	case ClientAuthPost:
		params.Set("client_id", clientInformation.ClientID)
		if clientInformation.ClientSecret != "" {
			params.Set("client_secret", clientInformation.ClientSecret)
		}
	case ClientAuthNone:
		params.Set("client_id", clientInformation.ClientID)
	default:
		return fmt.Errorf("mcp oauth: unsupported client auth method %q", method)
	}
	return nil
}

func selectClientAuthMethod(discovery DiscoveryState, clientInformation ClientInformation) string {
	supported := []string(nil)
	if discovery.AuthorizationServerMetadata != nil {
		supported = discovery.AuthorizationServerMetadata.TokenEndpointAuthMethodsSupported
	}
	if isClientAuthMethod(clientInformation.TokenEndpointAuthMethod) && (len(supported) == 0 || supportsValue(supported, clientInformation.TokenEndpointAuthMethod, false)) {
		return clientInformation.TokenEndpointAuthMethod
	}

	hasSecret := strings.TrimSpace(clientInformation.ClientSecret) != ""
	if len(supported) == 0 {
		if hasSecret {
			return ClientAuthBasic
		}
		return ClientAuthNone
	}
	if hasSecret && supportsValue(supported, ClientAuthBasic, false) {
		return ClientAuthBasic
	}
	if hasSecret && supportsValue(supported, ClientAuthPost, false) {
		return ClientAuthPost
	}
	if supportsValue(supported, ClientAuthNone, false) {
		return ClientAuthNone
	}
	if hasSecret {
		return ClientAuthPost
	}
	return ClientAuthNone
}

func isClientAuthMethod(method string) bool {
	switch method {
	case ClientAuthNone, ClientAuthBasic, ClientAuthPost:
		return true
	default:
		return false
	}
}

func resourceParameter(discovery DiscoveryState) string {
	if discovery.ResourceMetadata == nil {
		return ""
	}
	return strings.TrimSpace(discovery.ResourceMetadata.Resource)
}

func supportsValue(values []string, expected string, emptyMeansYes bool) bool {
	if len(values) == 0 {
		return emptyMeansYes
	}
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func hasScope(scope string, expected string) bool {
	for _, item := range strings.Fields(scope) {
		if item == expected {
			return true
		}
	}
	return false
}

func newPKCEPair() (string, string, error) {
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", "", fmt.Errorf("mcp oauth: generate pkce verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(randomBytes)
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])
	return verifier, challenge, nil
}

func tokenEndpointError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return fmt.Errorf("mcp oauth: token request failed with status %d", resp.StatusCode)
	}
	var payload struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error != "" {
		if payload.ErrorDescription != "" {
			return fmt.Errorf("mcp oauth: %s: %s", payload.Error, payload.ErrorDescription)
		}
		return fmt.Errorf("mcp oauth: %s", payload.Error)
	}
	return fmt.Errorf("mcp oauth: request failed with status %d: %s", resp.StatusCode, string(body))
}
