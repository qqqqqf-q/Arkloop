package webfetch

import (
	"fmt"
	"net/netip"
	"net/url"
	"strings"
)

type UrlPolicyDeniedError struct {
	Reason  string
	Details map[string]any
}

func (e UrlPolicyDeniedError) Error() string {
	return fmt.Sprintf("url denied: %s", e.Reason)
}

func EnsureURLAllowed(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return UrlPolicyDeniedError{Reason: "invalid_url"}
	}

	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme != "http" && scheme != "https" {
		return UrlPolicyDeniedError{Reason: "unsupported_scheme", Details: map[string]any{"scheme": scheme}}
	}

	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return UrlPolicyDeniedError{Reason: "missing_hostname"}
	}

	lowered := strings.ToLower(strings.Trim(host, "."))
	if lowered == "localhost" || strings.HasSuffix(lowered, ".localhost") {
		return UrlPolicyDeniedError{Reason: "localhost_denied", Details: map[string]any{"hostname": host}}
	}

	ip := tryParseIP(host)
	if !ip.IsValid() {
		return nil
	}

	if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return UrlPolicyDeniedError{Reason: "private_ip_denied", Details: map[string]any{"ip": ip.String()}}
	}
	return nil
}

func tryParseIP(hostname string) netip.Addr {
	candidate := strings.TrimSpace(hostname)
	if idx := strings.Index(candidate, "%"); idx >= 0 {
		candidate = candidate[:idx]
	}
	addr, err := netip.ParseAddr(candidate)
	if err != nil {
		return netip.Addr{}
	}
	return addr
}

