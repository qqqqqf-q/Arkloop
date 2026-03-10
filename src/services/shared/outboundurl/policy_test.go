package outboundurl

import (
	"errors"
	"net/netip"
	"testing"
)

func TestNormalizeBaseURL(t *testing.T) {
	policy := Policy{}

	tests := []struct {
		name       string
		raw        string
		want       string
		wantReason string
	}{
		{name: "https public allowed", raw: "https://example.com/v1/", want: "https://example.com/v1"},
		{name: "http public denied", raw: "http://example.com/v1", wantReason: "insecure_scheme_denied"},
		{name: "userinfo denied", raw: "https://user:pass@example.com/v1", wantReason: "userinfo_denied"},
		{name: "query denied", raw: "https://example.com/v1?q=1", wantReason: "query_denied"},
		{name: "fragment denied", raw: "https://example.com/v1#frag", wantReason: "fragment_denied"},
		{name: "private ip denied", raw: "https://10.0.0.1/v1", wantReason: "private_ip_denied"},
		{name: "fake ip denied by default", raw: "https://198.18.0.1/v1", wantReason: "private_ip_denied"},
		{name: "localhost denied", raw: "https://localhost:8443/v1", wantReason: "localhost_denied"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := policy.NormalizeBaseURL(tt.raw)
			if tt.wantReason == "" {
				if err != nil {
					t.Fatalf("NormalizeBaseURL() error = %v", err)
				}
				if got != tt.want {
					t.Fatalf("NormalizeBaseURL() = %q, want %q", got, tt.want)
				}
				return
			}
			assertDeniedReason(t, err, tt.wantReason)
		})
	}
}

func TestNormalizeBaseURLAllowLoopbackHTTP(t *testing.T) {
	policy := Policy{AllowLoopbackHTTP: true}
	got, err := policy.NormalizeBaseURL("http://127.0.0.1:8080/v1/")
	if err != nil {
		t.Fatalf("NormalizeBaseURL() error = %v", err)
	}
	if got != "http://127.0.0.1:8080/v1" {
		t.Fatalf("NormalizeBaseURL() = %q", got)
	}

	_, err = policy.NormalizeBaseURL("http://example.com/v1")
	assertDeniedReason(t, err, "insecure_scheme_denied")
}

func TestNormalizeBaseURLTrustFakeIP(t *testing.T) {
	policy := Policy{TrustFakeIP: true}
	got, err := policy.NormalizeBaseURL("https://198.18.0.1/v1/")
	if err != nil {
		t.Fatalf("NormalizeBaseURL() error = %v", err)
	}
	if got != "https://198.18.0.1/v1" {
		t.Fatalf("NormalizeBaseURL() = %q", got)
	}
}

func TestValidateRequestURL(t *testing.T) {
	policy := Policy{}
	tests := []struct {
		name       string
		raw        string
		wantReason string
	}{
		{name: "https request allowed", raw: "https://example.com/search?q=test"},
		{name: "ftp denied", raw: "ftp://example.com/file", wantReason: "unsupported_scheme"},
		{name: "public http denied", raw: "http://example.com/search", wantReason: "insecure_scheme_denied"},
		{name: "metadata denied", raw: "https://169.254.169.254/latest/meta-data/", wantReason: "private_ip_denied"},
		{name: "localhost denied", raw: "https://localhost/api", wantReason: "localhost_denied"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := policy.ValidateRequestURL(tt.raw)
			if tt.wantReason == "" {
				if err != nil {
					t.Fatalf("ValidateRequestURL() error = %v", err)
				}
				return
			}
			assertDeniedReason(t, err, tt.wantReason)
		})
	}
}

func TestEnsureIPAllowed(t *testing.T) {
	policy := Policy{}
	tests := []struct {
		ip      string
		wantErr bool
	}{
		{ip: "8.8.8.8"},
		{ip: "1.1.1.1"},
		{ip: "127.0.0.1", wantErr: true},
		{ip: "10.0.0.1", wantErr: true},
		{ip: "172.16.0.1", wantErr: true},
		{ip: "192.168.1.1", wantErr: true},
		{ip: "169.254.169.254", wantErr: true},
		{ip: "100.64.0.1", wantErr: true},
		{ip: "198.18.0.1", wantErr: true},
		{ip: "::1", wantErr: true},
		{ip: "fc00::1", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			err := policy.EnsureIPAllowed(netip.MustParseAddr(tt.ip))
			if tt.wantErr {
				assertDeniedReason(t, err, "private_ip_denied")
				return
			}
			if err != nil {
				t.Fatalf("EnsureIPAllowed() error = %v", err)
			}
		})
	}
}

func TestEnsureIPAllowedTrustFakeIP(t *testing.T) {
	policy := Policy{TrustFakeIP: true}
	if err := policy.EnsureIPAllowed(netip.MustParseAddr("198.18.0.1")); err != nil {
		t.Fatalf("EnsureIPAllowed() error = %v", err)
	}
	assertDeniedReason(t, policy.EnsureIPAllowed(netip.MustParseAddr("10.0.0.1")), "private_ip_denied")
}

func TestDefaultPolicyReadsTrustFakeIPEnv(t *testing.T) {
	t.Setenv(TrustFakeIPEnv, "true")
	policy := DefaultPolicy()
	if !policy.TrustFakeIP {
		t.Fatal("expected TrustFakeIP to be enabled from env")
	}
}

func TestValidateURLLoopbackHTTPRequiresExplicitLoopbackHost(t *testing.T) {
	policy := Policy{AllowLoopbackHTTP: true}
	if err := policy.ValidateRequestURL("http://localhost:3000/api"); err != nil {
		t.Fatalf("ValidateRequestURL() error = %v", err)
	}
	if err := policy.ValidateRequestURL("http://sub.localhost:3000/api"); err != nil {
		t.Fatalf("ValidateRequestURL() error = %v", err)
	}
	assertDeniedReason(t, policy.ValidateRequestURL("http://example.com/api"), "insecure_scheme_denied")
}

func TestParseIP(t *testing.T) {
	if got := ParseIP("fe80::1%eth0"); got != netip.MustParseAddr("fe80::1") {
		t.Fatalf("ParseIP() = %v", got)
	}
	if got := ParseIP("example.com"); got.IsValid() {
		t.Fatalf("ParseIP(example.com) = %v", got)
	}
}

func assertDeniedReason(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	var denied DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected DeniedError, got %T: %v", err, err)
	}
	if denied.Reason != want {
		t.Fatalf("Reason = %q, want %q", denied.Reason, want)
	}
}

func TestNormalizeInternalBaseURL(t *testing.T) {
	policy := Policy{}

	tests := []struct {
		name       string
		raw        string
		want       string
		wantReason string
	}{
		{name: "internal service http allowed", raw: "http://openviking:1933/api/", want: "http://openviking:1933/api"},
		{name: "private ip http allowed", raw: "http://10.0.0.8:8002/v1", want: "http://10.0.0.8:8002/v1"},
		{name: "userinfo denied", raw: "http://user:pass@openviking:1933/api", wantReason: "userinfo_denied"},
		{name: "query denied", raw: "http://openviking:1933/api?q=1", wantReason: "query_denied"},
		{name: "unsupported scheme denied", raw: "ftp://openviking:1933/api", wantReason: "unsupported_scheme"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := policy.NormalizeInternalBaseURL(tt.raw)
			if tt.wantReason == "" {
				if err != nil {
					t.Fatalf("NormalizeInternalBaseURL() error = %v", err)
				}
				if got != tt.want {
					t.Fatalf("NormalizeInternalBaseURL() = %q, want %q", got, tt.want)
				}
				return
			}
			assertDeniedReason(t, err, tt.wantReason)
		})
	}
}
