package toolprovider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	sharedtoolruntime "arkloop/services/shared/toolruntime"
)

const (
	xaiOAuthClientID             = "b1a00492-073a-47ea-816f-4c329264a828"
	xaiOAuthDiscoveryURL         = "https://auth.x.ai/.well-known/openid-configuration"
	xaiOAuthRefreshSkew          = 120 * time.Second
	xaiOAuthRefreshRequestTimout = 20 * time.Second
)

type xaiOAuthTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	Scope        string `json:"scope,omitempty"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	ObtainedAt   string `json:"obtained_at,omitempty"`
}

type xaiOAuthDiscoveryResponse struct {
	TokenEndpoint string `json:"token_endpoint"`
}

type encryptProviderToken func(plaintext string) (encrypted string, keyVersion int, err error)

type persistProviderToken func(ctx context.Context, status sharedtoolruntime.ProviderRuntimeStatus, encrypted string, keyVersion int, expiresAt *time.Time) error

func refreshXAIProviderOAuthStatusesCore(
	ctx context.Context,
	statuses []sharedtoolruntime.ProviderRuntimeStatus,
	encrypt encryptProviderToken,
	persist persistProviderToken,
) {
	if ctx == nil {
		ctx = context.Background()
	}
	if encrypt == nil || persist == nil {
		return
	}
	for i := range statuses {
		status := &statuses[i]
		if status.ProviderName != "x_search.xai" || status.OAuthValue == nil || status.OAuthTokenSecretID == nil {
			continue
		}
		refreshed, expiresAt, err := refreshXAIProviderOAuthValue(ctx, *status)
		if err != nil {
			status.RuntimeState = sharedtoolruntime.ProviderRuntimeStateInvalidConfig
			status.RuntimeReason = "oauth_refresh_failed"
			continue
		}
		if refreshed == nil {
			continue
		}
		encoded, err := json.Marshal(refreshed)
		if err != nil {
			status.RuntimeState = sharedtoolruntime.ProviderRuntimeStateInvalidConfig
			status.RuntimeReason = "oauth_refresh_invalid"
			continue
		}
		encrypted, keyVersion, err := encrypt(string(encoded))
		if err != nil {
			status.RuntimeState = sharedtoolruntime.ProviderRuntimeStateDecryptFailed
			status.RuntimeReason = "oauth_secret_encrypt_failed"
			continue
		}
		if err := persist(ctx, *status, encrypted, keyVersion, expiresAt); err != nil {
			status.RuntimeState = sharedtoolruntime.ProviderRuntimeStateInvalidConfig
			status.RuntimeReason = "oauth_refresh_persist_failed"
			continue
		}
		plain := string(encoded)
		status.OAuthValue = &plain
		status.OAuthExpiresAt = expiresAt
	}
}

func refreshXAIProviderOAuthValue(ctx context.Context, status sharedtoolruntime.ProviderRuntimeStatus) (*xaiOAuthTokens, *time.Time, error) {
	if status.OAuthValue == nil {
		return nil, nil, nil
	}
	var tokens xaiOAuthTokens
	if err := json.Unmarshal([]byte(*status.OAuthValue), &tokens); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(tokens.AccessToken) == "" || strings.TrimSpace(tokens.RefreshToken) == "" {
		return nil, nil, nil
	}
	if !xaiTokenExpiresSoon(tokens.AccessToken, tokens.ExpiresAt, xaiOAuthRefreshSkew) {
		return nil, parseXAITokenExpiry(tokens.AccessToken, tokens.ExpiresAt), nil
	}
	endpoint, err := discoverXAITokenEndpoint(ctx)
	if err != nil {
		return nil, nil, err
	}
	refreshed, err := refreshXAIToken(ctx, endpoint, derefXAIClientID(status.OAuthClientID), tokens.RefreshToken)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(refreshed.RefreshToken) == "" {
		refreshed.RefreshToken = tokens.RefreshToken
	}
	if strings.TrimSpace(refreshed.IDToken) == "" {
		refreshed.IDToken = tokens.IDToken
	}
	if strings.TrimSpace(refreshed.TokenType) == "" {
		refreshed.TokenType = "Bearer"
	}
	if strings.TrimSpace(refreshed.Scope) == "" {
		refreshed.Scope = tokens.Scope
	}
	now := time.Now().UTC()
	refreshed.ObtainedAt = now.Format(time.RFC3339)
	if refreshed.ExpiresIn > 0 && strings.TrimSpace(refreshed.ExpiresAt) == "" {
		refreshed.ExpiresAt = now.Add(time.Duration(refreshed.ExpiresIn) * time.Second).Format(time.RFC3339)
	}
	return &refreshed, parseXAITokenExpiry(refreshed.AccessToken, refreshed.ExpiresAt), nil
}

func discoverXAITokenEndpoint(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, xaiOAuthDiscoveryURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: xaiOAuthRefreshRequestTimout}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("xai oauth discovery status %d", resp.StatusCode)
	}
	var discovery xaiOAuthDiscoveryResponse
	if err := json.Unmarshal(body, &discovery); err != nil {
		return "", err
	}
	endpoint := strings.TrimSpace(discovery.TokenEndpoint)
	if endpoint == "" {
		return "", fmt.Errorf("xai oauth discovery missing token_endpoint")
	}
	if err := validateXAIEndpoint(endpoint); err != nil {
		return "", err
	}
	return endpoint, nil
}

func refreshXAIToken(ctx context.Context, endpoint string, clientID string, refreshToken string) (xaiOAuthTokens, error) {
	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("client_id", clientID)
	values.Set("refresh_token", refreshToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return xaiOAuthTokens{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: xaiOAuthRefreshRequestTimout}
	resp, err := client.Do(req)
	if err != nil {
		return xaiOAuthTokens{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return xaiOAuthTokens{}, fmt.Errorf("xai oauth refresh status %d", resp.StatusCode)
	}
	var tokens xaiOAuthTokens
	if err := json.Unmarshal(body, &tokens); err != nil {
		return xaiOAuthTokens{}, err
	}
	if strings.TrimSpace(tokens.AccessToken) == "" {
		return xaiOAuthTokens{}, fmt.Errorf("xai oauth refresh missing access_token")
	}
	return tokens, nil
}

func xaiTokenExpiresSoon(accessToken string, expiresAt string, skew time.Duration) bool {
	exp := parseXAITokenExpiry(accessToken, expiresAt)
	if exp == nil {
		return false
	}
	return !time.Now().UTC().Add(skew).Before(*exp)
}

func parseXAITokenExpiry(accessToken string, expiresAt string) *time.Time {
	if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(expiresAt)); err == nil {
		utc := parsed.UTC()
		return &utc
	}
	parts := strings.Split(accessToken, ".")
	if len(parts) < 2 {
		return nil
	}
	payload := parts[1] + strings.Repeat("=", (4-len(parts[1])%4)%4)
	raw, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return nil
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil || claims.Exp <= 0 {
		return nil
	}
	exp := time.Unix(claims.Exp, 0).UTC()
	return &exp
}

func validateXAIEndpoint(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("xai oauth endpoint must use https")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "x.ai" && !strings.HasSuffix(host, ".x.ai") {
		return fmt.Errorf("xai oauth endpoint host is not x.ai")
	}
	return nil
}

func derefXAIClientID(value *string) string {
	if value != nil && strings.TrimSpace(*value) != "" {
		return strings.TrimSpace(*value)
	}
	return xaiOAuthClientID
}
