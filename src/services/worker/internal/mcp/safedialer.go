package mcp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	sharedoutbound "arkloop/services/shared/outboundurl"
)

type SSRFError struct {
	Message string
}

func (e SSRFError) Error() string {
	return e.Message
}

func newSafeHTTPClient() *http.Client {
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

		for _, ip := range ips {
			if isDeniedIP(ip.Unmap(), policy) {
				return nil, SSRFError{Message: fmt.Sprintf("mcp: ssrf: denied ip %s for host %s", ip, host)}
			}
		}

		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].Unmap().String(), port))
	}

	return &http.Client{
		Timeout:   0,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("mcp: ssrf: too many redirects")
			}
			if err := validateURL(req.URL, policy); err != nil {
				return err
			}
			return nil
		},
	}
}

func validateURL(u *url.URL, policy sharedoutbound.Policy) error {
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
		if isDeniedIP(ip, policy) {
			return SSRFError{Message: fmt.Sprintf("mcp: ssrf: denied ip %s", ip)}
		}
	}

	return nil
}

func isDeniedIP(ip netip.Addr, policy sharedoutbound.Policy) bool {
	if !policy.ProtectionEnabled {
		return false
	}
	if isCloudMetadata(ip) {
		return true
	}
	return policy.EnsureIPAllowed(ip.Unmap()) != nil
}

func isCloudMetadata(ip netip.Addr) bool {
	metadata := []netip.Addr{
		netip.MustParseAddr("169.254.169.254"),
		netip.MustParseAddr("fd00:ec2::254"),
	}
	for _, m := range metadata {
		if ip == m {
			return true
		}
	}
	return false
}
