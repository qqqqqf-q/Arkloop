package mcp

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	sharedmcpinstall "arkloop/services/shared/mcpinstall"
	sharedmcpoauth "arkloop/services/shared/mcpoauth"

	"github.com/google/uuid"
)

const oauthRefreshLeeway = 5 * time.Minute

type AuthRequiredError struct {
	ServerID   string
	StatusCode int
	Reason     string
	Cause      error
}

func (e AuthRequiredError) Error() string {
	if e.Cause != nil {
		return "mcp auth_required: " + e.Reason + ": " + e.Cause.Error()
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("mcp auth_required: %s: status %d", e.Reason, e.StatusCode)
	}
	return "mcp auth_required: " + e.Reason
}

func (e AuthRequiredError) Unwrap() error {
	return e.Cause
}

func (c *HTTPClient) loadAuthState() {
	c.oauth = c.server.OAuth
	c.authSecret = strings.TrimSpace(c.server.AuthSecretID)
}

func (c *HTTPClient) prepareRequest(ctx context.Context, req *http.Request) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID := c.currentSessionID(); sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	for key, value := range c.server.Headers {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		req.Header.Set(key, value)
	}
	token, err := c.oauthAccessToken(ctx)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return nil
}

func (c *HTTPClient) doHTTP(req *http.Request) (*http.Response, error) {
	return c.httpClient.Do(req)
}

func (c *HTTPClient) oauthAccessToken(ctx context.Context) (string, error) {
	c.oauthMu.Lock()
	defer c.oauthMu.Unlock()

	if c.oauth == nil {
		return "", nil
	}
	tokens := c.oauth.Tokens
	if strings.TrimSpace(tokens.AccessToken) != "" && (tokens.ExpiresAt.IsZero() || time.Now().Add(oauthRefreshLeeway).Before(tokens.ExpiresAt)) {
		return strings.TrimSpace(tokens.AccessToken), nil
	}
	if strings.TrimSpace(tokens.RefreshToken) == "" {
		return "", AuthRequiredError{ServerID: c.server.ServerID, Reason: "missing_refresh_token"}
	}
	if err := c.refreshOAuthLocked(ctx); err != nil {
		return "", err
	}
	return strings.TrimSpace(c.oauth.Tokens.AccessToken), nil
}

func (c *HTTPClient) refreshOAuth(ctx context.Context) error {
	c.oauthMu.Lock()
	defer c.oauthMu.Unlock()
	if c.oauth == nil {
		return AuthRequiredError{ServerID: c.server.ServerID, Reason: "missing_oauth"}
	}
	if strings.TrimSpace(c.oauth.Tokens.RefreshToken) == "" {
		return AuthRequiredError{ServerID: c.server.ServerID, Reason: "missing_refresh_token"}
	}
	return c.refreshOAuthLocked(ctx)
}

func (c *HTTPClient) refreshOAuthLocked(ctx context.Context) error {
	refreshed, err := sharedmcpoauth.RefreshAuthorization(ctx, c.httpClient, c.oauth.Discovery, c.oauth.Client, c.oauth.Tokens.RefreshToken)
	if err != nil {
		return err
	}
	c.oauth.Tokens = refreshed
	c.server.OAuth = c.oauth
	return c.persistOAuthLocked(ctx)
}

func (c *HTTPClient) persistOAuthLocked(ctx context.Context) error {
	if c.authStore == nil || c.oauth == nil {
		return nil
	}
	secretID, err := uuid.Parse(c.authSecret)
	if err != nil || secretID == uuid.Nil {
		return AuthRequiredError{ServerID: c.server.ServerID, Reason: "missing_auth_secret"}
	}
	return c.authStore.Save(ctx, secretID, sharedmcpinstall.AuthPayload{
		Headers: cloneStringMap(c.server.Headers),
		Env:     cloneStringMap(c.server.Env),
		OAuth:   c.oauth,
	})
}

func cloneStringMap(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]string, len(value))
	for key, item := range value {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = item
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isAuthStatus(statusCode int) bool {
	return statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden
}
