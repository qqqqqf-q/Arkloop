package mcpinstall

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"

	sharedmcpoauth "arkloop/services/shared/mcpoauth"
	sharedoutbound "arkloop/services/shared/outboundurl"

	"golang.org/x/oauth2"
)

// BuildCommandTransport creates an SDK CommandTransport from ServerConfig.
// The subprocess always inherits the parent environment with server.Env overrides.
func BuildCommandTransport(server ServerConfig) *sdkmcp.CommandTransport {
	cmd := exec.Command(server.Command, server.Args...)
	if server.Cwd != nil {
		cmd.Dir = *server.Cwd
	}
	cmd.Env = buildInheritedEnv(server.Env)
	return &sdkmcp.CommandTransport{Command: cmd}
}

// BuildSSETransport creates an SDK SSEClientTransport from ServerConfig (legacy 2024-11-05 protocol).
// httpClient may be nil (defaults to http.DefaultClient).
func BuildSSETransport(server ServerConfig, httpClient *http.Client) *sdkmcp.SSEClientTransport {
	client := httpClient
	if client == nil {
		client = http.DefaultClient
	}
	if len(server.Headers) > 0 {
		client = injectHeaders(client, server.Headers)
	}
	return &sdkmcp.SSEClientTransport{
		Endpoint:   server.URL,
		HTTPClient: client,
	}
}

// BuildStreamableTransport creates an SDK StreamableClientTransport from ServerConfig.
// httpClient may be nil (defaults to http.DefaultClient).
// onOAuthRefresh is called after token refresh; may be nil.
func BuildStreamableTransport(server ServerConfig, httpClient *http.Client, onOAuthRefresh func(updated *sharedmcpoauth.AuthState)) *sdkmcp.StreamableClientTransport {
	client := httpClient
	if client == nil {
		client = http.DefaultClient
	}

	// Wrap transport to inject static headers
	if len(server.Headers) > 0 {
		client = injectHeaders(client, server.Headers)
	}

	// Build OAuth handler if OAuth state exists
	var oauthHandler sdkauth.OAuthHandler
	if server.OAuth != nil {
		oauthHandler = &ArkloopOAuthHandler{
			OAuth:     server.OAuth,
			HTTPDoer:  client,
			OnRefresh: onOAuthRefresh,
		}
	}

	return &sdkmcp.StreamableClientTransport{
		Endpoint:    server.URL,
		HTTPClient:  client,
		OAuthHandler: oauthHandler,
	}
}

func buildInheritedEnv(overrides map[string]string) []string {
	env := os.Environ()
	for key, value := range overrides {
		prefix := key + "="
		filtered := make([]string, 0, len(env))
		for _, entry := range env {
			if !strings.HasPrefix(entry, prefix) {
				filtered = append(filtered, entry)
			}
		}
		filtered = append(filtered, fmt.Sprintf("%s=%s", key, value))
		env = filtered
	}
	return env
}

func injectHeaders(client *http.Client, headers map[string]string) *http.Client {
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &http.Client{
		Transport:    &HeaderInjectingRoundTripper{Base: transport, Headers: headers},
		CheckRedirect: client.CheckRedirect,
		Jar:          client.Jar,
		Timeout:      client.Timeout,
	}
}

// HeaderInjectingRoundTripper adds static headers to every outbound request.
type HeaderInjectingRoundTripper struct {
	Base    http.RoundTripper
	Headers map[string]string
}

func (t *HeaderInjectingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for key, value := range t.Headers {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		req.Header.Set(key, value)
	}
	return t.Base.RoundTrip(req)
}

// ArkloopOAuthHandler implements auth.OAuthHandler, bridging our mcpoauth.AuthState
// to the SDK's OAuth interface.
type ArkloopOAuthHandler struct {
	OAuth     *sharedmcpoauth.AuthState
	HTTPDoer  sharedmcpoauth.HTTPDoer
	OnRefresh func(updated *sharedmcpoauth.AuthState)
	mu        sync.Mutex
}

func (h *ArkloopOAuthHandler) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.OAuth == nil {
		return nil, nil
	}

	tokens := h.OAuth.Tokens
	// If access token is valid, return it directly
	if strings.TrimSpace(tokens.AccessToken) != "" && !sharedmcpoauth.TokenExpiredSoon(tokens, 5*time.Minute) {
		return oauth2.StaticTokenSource(&oauth2.Token{
			AccessToken: strings.TrimSpace(tokens.AccessToken),
			TokenType:   "Bearer",
			Expiry:      tokens.ExpiresAt,
		}), nil
	}

	// Try to refresh
	if strings.TrimSpace(tokens.RefreshToken) == "" {
		return nil, nil
	}

	refreshed, err := sharedmcpoauth.RefreshAuthorization(ctx, h.HTTPDoer, h.OAuth.Discovery, h.OAuth.Client, tokens.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("mcp oauth: refresh failed: %w", ErrOAuthRefreshFailed)
	}

	h.OAuth.Tokens = refreshed
	if h.OnRefresh != nil {
		h.OnRefresh(h.OAuth)
	}

	return oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: strings.TrimSpace(refreshed.AccessToken),
		TokenType:   "Bearer",
		Expiry:      refreshed.ExpiresAt,
	}), nil
}

func (h *ArkloopOAuthHandler) Authorize(ctx context.Context, req *http.Request, resp *http.Response) error {
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	return fmt.Errorf("mcp oauth: authorization required: %w", ErrOAuthAuthRequired)
}

// NewSafeHTTPClient creates an HTTP client with SSRF protection via DNS-level IP filtering.
// Both Worker and API services should use this for MCP HTTP transports.
func NewSafeHTTPClient() *http.Client {
	policy := sharedoutbound.DefaultPolicy()
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if !policy.ProtectionEnabled {
			return dialer.DialContext(ctx, network, addr)
		}
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("mcp: ssrf: invalid addr %q: %w", addr, err)
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("mcp: ssrf: resolve %q: %w", host, err)
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("mcp: ssrf: no IPs for host %q", host)
		}
		for _, ip := range ips {
			if IsDeniedIP(ip.Unmap(), policy) {
				return nil, SSRFError{Message: fmt.Sprintf("mcp: ssrf: denied ip %s for host %s", ip, host)}
			}
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].Unmap().String(), port))
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("mcp: ssrf: too many redirects")
			}
			return ValidateURL(req.URL, policy)
		},
	}
}

type SSRFError struct {
	Message string
}

func (e SSRFError) Error() string { return e.Message }

func ValidateURL(u *url.URL, policy sharedoutbound.Policy) error {
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return SSRFError{Message: fmt.Sprintf("mcp: ssrf: unsupported scheme %q", scheme)}
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" {
		return SSRFError{Message: "mcp: ssrf: empty hostname"}
	}
	if !policy.ProtectionEnabled {
		return nil
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return SSRFError{Message: fmt.Sprintf("mcp: ssrf: denied hostname %q", host)}
	}
	if ip := sharedoutbound.ParseIP(host); ip.IsValid() {
		if IsDeniedIP(ip, policy) {
			return SSRFError{Message: fmt.Sprintf("mcp: ssrf: denied ip %s", ip)}
		}
	}
	return nil
}

func IsDeniedIP(ip netip.Addr, policy sharedoutbound.Policy) bool {
	if !policy.ProtectionEnabled {
		return false
	}
	for _, m := range []netip.Addr{
		netip.MustParseAddr("169.254.169.254"),
		netip.MustParseAddr("fd00:ec2::254"),
	} {
		if ip == m {
			return true
		}
	}
	return policy.EnsureIPAllowed(ip.Unmap()) != nil
}

var (
	// ErrOAuthAuthRequired is returned when the OAuth Authorize callback is invoked,
	// meaning the server requires user intervention to complete authorization.
	ErrOAuthAuthRequired = errors.New("oauth authorization required")

	// ErrOAuthRefreshFailed is returned when the OAuth token refresh fails.
	ErrOAuthRefreshFailed = errors.New("oauth refresh failed")
)
