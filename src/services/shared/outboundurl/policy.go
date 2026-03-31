package outboundurl

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	AllowLoopbackHTTPEnv = "ARKLOOP_OUTBOUND_ALLOW_LOOPBACK_HTTP"
	TrustFakeIPEnv       = "ARKLOOP_OUTBOUND_TRUST_FAKE_IP"
	ProxyURLEnv          = "ARKLOOP_OUTBOUND_PROXY_URL"
	TimeoutMSEnv         = "ARKLOOP_OUTBOUND_TIMEOUT_MS"
	RetryCountEnv        = "ARKLOOP_OUTBOUND_RETRY_COUNT"
	UserAgentEnv         = "ARKLOOP_OUTBOUND_USER_AGENT"
)

type DeniedError struct {
	Reason  string
	Details map[string]any
}

func (e DeniedError) Error() string {
	return fmt.Sprintf("outbound url denied: %s", e.Reason)
}

type Policy struct {
	AllowLoopbackHTTP bool
	TrustFakeIP       bool
	Resolver          *net.Resolver
	ProxyURL          string
	Timeout           time.Duration
	RetryCount        int
	UserAgent         string
}

func DefaultPolicy() Policy {
	return Policy{
		AllowLoopbackHTTP: allowLoopbackHTTPFromEnv(),
		TrustFakeIP:       trustFakeIPFromEnv(),
		Resolver:          net.DefaultResolver,
		ProxyURL:          strings.TrimSpace(os.Getenv(ProxyURLEnv)),
		Timeout:           timeoutFromEnv(),
		RetryCount:        retryCountFromEnv(),
		UserAgent:         strings.TrimSpace(os.Getenv(UserAgentEnv)),
	}
}

func allowLoopbackHTTPFromEnv() bool {
	raw := strings.TrimSpace(os.Getenv(AllowLoopbackHTTPEnv))
	if raw == "" {
		return defaultAllowLoopbackHTTP()
	}
	ok, err := strconv.ParseBool(raw)
	return err == nil && ok
}

func trustFakeIPFromEnv() bool {
	raw := strings.TrimSpace(os.Getenv(TrustFakeIPEnv))
	if raw == "" {
		return defaultTrustFakeIP()
	}
	ok, err := strconv.ParseBool(raw)
	return err == nil && ok
}

func timeoutFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv(TimeoutMSEnv))
	if raw == "" {
		return 0
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

func retryCountFromEnv() int {
	raw := strings.TrimSpace(os.Getenv(RetryCountEnv))
	if raw == "" {
		return 0
	}
	count, err := strconv.Atoi(raw)
	if err != nil || count < 0 {
		return 0
	}
	return count
}

func (p Policy) NormalizeBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", DeniedError{Reason: "invalid_url"}
	}
	if err := p.validateParsedURL(parsed, true); err != nil {
		return "", err
	}
	parsed.Path = strings.TrimRight(parsed.EscapedPath(), "/")
	if parsed.Path == "." {
		parsed.Path = ""
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func (p Policy) NormalizeInternalBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", DeniedError{Reason: "invalid_url"}
	}
	if err := validateInternalParsedURL(parsed, true); err != nil {
		return "", err
	}
	parsed.Path = strings.TrimRight(parsed.EscapedPath(), "/")
	if parsed.Path == "." {
		parsed.Path = ""
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func (p Policy) ValidateRequestURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return DeniedError{Reason: "invalid_url"}
	}
	return p.ValidateURL(parsed)
}

func (p Policy) ValidateURL(u *url.URL) error {
	return p.validateParsedURL(u, false)
}

func (p Policy) EnsureIPAllowed(ip netip.Addr) error {
	addr := ip.Unmap()
	if !addr.IsValid() {
		return DeniedError{Reason: "invalid_ip"}
	}
	if p.isDeniedIP(addr) {
		return DeniedError{Reason: "private_ip_denied", Details: map[string]any{"ip": addr.String()}}
	}
	return nil
}

func ParseIP(hostname string) netip.Addr {
	candidate := strings.TrimSpace(hostname)
	if idx := strings.Index(candidate, "%"); idx >= 0 {
		candidate = candidate[:idx]
	}
	addr, err := netip.ParseAddr(candidate)
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap()
}

func (p Policy) SafeDialContext(dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	if dialer == nil {
		dialer = &net.Dialer{Timeout: 10 * time.Second}
	}
	resolver := p.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("outbound dial split host/port: %w", err)
		}

		ips, err := resolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("outbound dns resolve failed: %w", err)
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("outbound dns resolve returned no addresses for %s", host)
		}

		for _, ip := range ips {
			if err := p.ensureDialIPAllowed(host, ip.Unmap()); err != nil {
				return nil, err
			}
		}

		target := net.JoinHostPort(ips[0].Unmap().String(), port)
		return dialer.DialContext(ctx, network, target)
	}
}

func (p Policy) CheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("outbound redirect limit exceeded")
	}
	return p.ValidateURL(req.URL)
}

func (p Policy) NewHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = p.SafeDialContext(&net.Dialer{Timeout: 10 * time.Second})
	if proxyURL := strings.TrimSpace(p.ProxyURL); proxyURL != "" {
		if parsed, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(parsed)
		}
	}
	effectiveTimeout := timeout
	if effectiveTimeout <= 0 && p.Timeout > 0 {
		effectiveTimeout = p.Timeout
	}
	return &http.Client{
		Timeout:       effectiveTimeout,
		Transport:     validatingTransport{base: transport, policy: p},
		CheckRedirect: p.CheckRedirect,
	}
}

func (p Policy) NewInternalHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &http.Client{
		Timeout:       timeout,
		Transport:     validatingInternalTransport{base: transport},
		CheckRedirect: checkInternalRedirect,
	}
}

type validatingTransport struct {
	base   http.RoundTripper
	policy Policy
}

func (t validatingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.policy.ValidateURL(req.URL); err != nil {
		return nil, err
	}
	if userAgent := strings.TrimSpace(t.policy.UserAgent); userAgent != "" && req.Header.Get("User-Agent") == "" {
		req = req.Clone(req.Context())
		req.Header.Set("User-Agent", userAgent)
	}
	var resp *http.Response
	var err error
	attempts := t.policy.RetryCount + 1
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 0; attempt < attempts; attempt++ {
		resp, err = t.base.RoundTrip(req)
		if err == nil {
			return resp, nil
		}
	}
	return nil, err
}

type validatingInternalTransport struct {
	base http.RoundTripper
}

func (t validatingInternalTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := validateInternalParsedURL(req.URL, false); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(req)
}

func checkInternalRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("outbound redirect limit exceeded")
	}
	return validateInternalParsedURL(req.URL, false)
}

func (p Policy) validateParsedURL(u *url.URL, baseURLMode bool) error {
	if u == nil {
		return DeniedError{Reason: "invalid_url"}
	}
	if !u.IsAbs() {
		return DeniedError{Reason: "invalid_url"}
	}

	scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
	hostname := strings.TrimSpace(u.Hostname())
	if hostname == "" {
		return DeniedError{Reason: "missing_hostname"}
	}

	if baseURLMode {
		if u.User != nil {
			return DeniedError{Reason: "userinfo_denied"}
		}
		if strings.TrimSpace(u.RawQuery) != "" {
			return DeniedError{Reason: "query_denied"}
		}
		if strings.TrimSpace(u.Fragment) != "" {
			return DeniedError{Reason: "fragment_denied"}
		}
	}

	if !p.isAllowedSchemeForHost(scheme, hostname) {
		if scheme != "http" && scheme != "https" {
			return DeniedError{Reason: "unsupported_scheme", Details: map[string]any{"scheme": scheme}}
		}
		return DeniedError{Reason: "insecure_scheme_denied", Details: map[string]any{"scheme": scheme}}
	}

	lowered := strings.ToLower(strings.Trim(hostname, "."))
	if lowered == "localhost" || strings.HasSuffix(lowered, ".localhost") {
		if p.AllowLoopbackHTTP && scheme == "http" {
			return nil
		}
		return DeniedError{Reason: "localhost_denied", Details: map[string]any{"hostname": hostname}}
	}

	if ip := ParseIP(hostname); ip.IsValid() {
		if p.AllowLoopbackHTTP && scheme == "http" && ip.IsLoopback() {
			return nil
		}
		return p.EnsureIPAllowed(ip)
	}

	return nil
}

func validateInternalParsedURL(u *url.URL, baseURLMode bool) error {
	if u == nil {
		return DeniedError{Reason: "invalid_url"}
	}
	if !u.IsAbs() {
		return DeniedError{Reason: "invalid_url"}
	}

	scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
	if scheme != "http" && scheme != "https" {
		return DeniedError{Reason: "unsupported_scheme", Details: map[string]any{"scheme": scheme}}
	}
	if strings.TrimSpace(u.Hostname()) == "" {
		return DeniedError{Reason: "missing_hostname"}
	}

	if baseURLMode {
		if u.User != nil {
			return DeniedError{Reason: "userinfo_denied"}
		}
		if strings.TrimSpace(u.RawQuery) != "" {
			return DeniedError{Reason: "query_denied"}
		}
		if strings.TrimSpace(u.Fragment) != "" {
			return DeniedError{Reason: "fragment_denied"}
		}
	}

	return nil
}

func (p Policy) ensureDialIPAllowed(host string, ip netip.Addr) error {
	addr := ip.Unmap()
	if !addr.IsValid() {
		return DeniedError{Reason: "invalid_ip"}
	}
	if p.AllowLoopbackHTTP && addr.IsLoopback() && isExplicitLoopbackHost(host) {
		return nil
	}
	return p.EnsureIPAllowed(addr)
}

func (p Policy) isAllowedSchemeForHost(scheme, hostname string) bool {
	if scheme == "https" {
		return true
	}
	if scheme != "http" || !p.AllowLoopbackHTTP {
		return false
	}
	return isExplicitLoopbackHost(hostname)
}

func isExplicitLoopbackHost(hostname string) bool {
	lowered := strings.ToLower(strings.Trim(strings.TrimSpace(hostname), "."))
	if lowered == "localhost" || strings.HasSuffix(lowered, ".localhost") {
		return true
	}
	ip := ParseIP(lowered)
	return ip.IsValid() && ip.IsLoopback()
}

func (p Policy) isDeniedIP(ip netip.Addr) bool {
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if p.TrustFakeIP && fakeIPPrefix.Contains(ip) {
		return false
	}
	for _, prefix := range extraDeniedPrefixes {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

var fakeIPPrefix = netip.MustParsePrefix("198.18.0.0/15")

var extraDeniedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	fakeIPPrefix,
	netip.MustParsePrefix("fc00::/7"),
}
