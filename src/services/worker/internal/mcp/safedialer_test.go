package mcp

import (
	"context"
	"net/netip"
	"net/url"
	"testing"

	sharedmcpinstall "arkloop/services/shared/mcpinstall"
	sharedoutbound "arkloop/services/shared/outboundurl"
)

func TestIsDeniedIP(t *testing.T) {
	policy := sharedoutbound.Policy{ProtectionEnabled: true}
	tests := []struct {
		ip   string
		deny bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true},
		{"169.254.1.1", true},
		{"0.0.0.0", true},
		{"::", true},
		{"224.0.0.1", true},
		{"fd00:ec2::254", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"2606:4700::1111", false},
		{"93.184.216.34", false},
	}

	for _, tt := range tests {
		ip := netip.MustParseAddr(tt.ip)
		got := sharedmcpinstall.IsDeniedIP(ip, policy)
		if got != tt.deny {
			t.Errorf("IsDeniedIP(%s) = %v, want %v", tt.ip, got, tt.deny)
		}
	}
}

func TestValidateURL(t *testing.T) {
	policy := sharedoutbound.Policy{ProtectionEnabled: true}
	tests := []struct {
		rawURL  string
		wantErr bool
	}{
		{"https://example.com/api", false},
		{"http://api.example.com:8080/v1", false},
		{"ftp://example.com", true},
		{"http://localhost/api", true},
		{"http://127.0.0.1/api", true},
		{"http://10.0.0.1/api", true},
		{"http://169.254.169.254/latest/meta-data/", true},
		{"http://[::1]/api", true},
		{"http:///no-host", true},
		{"gopher://evil.com", true},
		{"http://sub.localhost/api", true},
	}

	for _, tt := range tests {
		u, err := url.Parse(tt.rawURL)
		if err != nil {
			if !tt.wantErr {
				t.Errorf("url.Parse(%q) unexpected error: %v", tt.rawURL, err)
			}
			continue
		}
		err = sharedmcpinstall.ValidateURL(u, policy)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateURL(%q) error=%v, wantErr=%v", tt.rawURL, err, tt.wantErr)
		}
	}
}

func TestValidateURLSkipsSafetyChecksByDefault(t *testing.T) {
	policy := sharedoutbound.Policy{}
	for _, rawURL := range []string{
		"http://localhost/api",
		"http://127.0.0.1/api",
		"http://10.0.0.1/api",
	} {
		t.Run(rawURL, func(t *testing.T) {
			u, err := url.Parse(rawURL)
			if err != nil {
				t.Fatalf("url.Parse(%q): %v", rawURL, err)
			}
			if err := sharedmcpinstall.ValidateURL(u, policy); err != nil {
				t.Fatalf("ValidateURL(%q) error=%v", rawURL, err)
			}
		})
	}
}

func TestNewSDKClientRejectsInternalURL(t *testing.T) {
	t.Setenv(sharedoutbound.ProtectionEnabledEnv, "true")

	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"public url", "https://api.example.com/mcp", false},
		{"localhost", "http://localhost:3000/mcp", true},
		{"loopback ip", "http://127.0.0.1:3000/mcp", true},
		{"private ip", "http://10.0.0.5:8080/mcp", true},
		{"metadata", "http://169.254.169.254/latest/meta-data/", true},
		{"ftp scheme", "ftp://evil.com/file", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newSDKClient(context.Background(), ServerConfig{
				URL:       tt.url,
				Transport: "streamable_http",
			}, nil)
			if tt.wantErr && err == nil {
				t.Errorf("newSDKClient(%q) expected error, got nil", tt.url)
			}
		})
	}
}
