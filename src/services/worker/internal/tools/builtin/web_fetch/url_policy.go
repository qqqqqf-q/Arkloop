package webfetch

import (
	"context"
	"fmt"
	"net"
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

// EnsureIPAllowed 校验解析后的 IP，防止 DNS rebinding 绕过字符串级 URL 检查。
func EnsureIPAllowed(ip netip.Addr) error {
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return UrlPolicyDeniedError{Reason: "private_ip_denied", Details: map[string]any{"ip": ip.String()}}
	}
	return nil
}

// SafeDialContext 在 DNS 解析后校验全部 IP，消除 TOCTOU 窗口。
func SafeDialContext(dialer *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("failed to split host/port: %w", err)
		}

		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("dns resolve failed: %w", err)
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("dns resolve returned no addresses for %s", host)
		}

		for _, ip := range ips {
			if err := EnsureIPAllowed(ip.Unmap()); err != nil {
				return nil, err
			}
		}

		// 用已校验的 IP 直连，防止二次 DNS 解析
		target := net.JoinHostPort(ips[0].Unmap().String(), port)
		return dialer.DialContext(ctx, network, target)
	}
}
