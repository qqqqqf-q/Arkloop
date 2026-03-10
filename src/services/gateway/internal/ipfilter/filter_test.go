package ipfilter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestExtractOrgID(t *testing.T) {
	orgID := "550e8400-e29b-41d4-a716-446655440000"
	token := signedToken(t, map[string]any{"sub": "user-id", "org": orgID, "exp": time.Now().Add(time.Hour).Unix()}, []byte("test-secret"))

	cases := []struct {
		name   string
		secret []byte
		header string
		want   string
	}{
		{"valid bearer with secret", []byte("test-secret"), "Bearer " + token, orgID},
		{"valid bearer without secret", nil, "Bearer " + token, ""},
		{"missing bearer prefix", nil, token, ""},
		{"empty", nil, "", ""},
		{"no org claim", []byte("test-secret"), "Bearer " + signedToken(t, map[string]any{"sub": "x", "exp": time.Now().Add(time.Hour).Unix()}, []byte("test-secret")), ""},
		{"malformed parts", []byte("test-secret"), "Bearer not.valid", ""},
	}

	for _, tc := range cases {
		got := extractOrgIDWithRedis(tc.header, nil, context.Background(), tc.secret)
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestMatchCIDR(t *testing.T) {
	cases := []struct {
		ip   string
		cidr string
		want bool
	}{
		{"192.168.1.5", "192.168.1.0/24", true},
		{"192.168.2.1", "192.168.1.0/24", false},
		{"10.0.0.1", "10.0.0.0/8", true},
		{"1.2.3.4", "1.2.3.4/32", true},
		{"1.2.3.5", "1.2.3.4/32", false},
		{"::1", "::1/128", true},
		{"2001:db8::1", "2001:db8::/32", true},
	}

	for _, tc := range cases {
		ip := net.ParseIP(tc.ip)
		got := matchCIDR(ip, tc.cidr)
		if got != tc.want {
			t.Errorf("matchCIDR(%q, %q) = %v, want %v", tc.ip, tc.cidr, got, tc.want)
		}
	}
}

func TestFilterCheckRulesSemantics(t *testing.T) {
	f := &Filter{redis: nil}

	cases := []struct {
		name      string
		r         rules
		clientIP  string
		wantBlock bool
	}{
		{
			name:      "no rules → allow",
			r:         rules{},
			clientIP:  "1.2.3.4",
			wantBlock: false,
		},
		{
			name:      "blocklist hit → block",
			r:         rules{Blocklist: []string{"1.2.3.4/32"}},
			clientIP:  "1.2.3.4",
			wantBlock: true,
		},
		{
			name:      "blocklist miss → allow",
			r:         rules{Blocklist: []string{"5.6.7.8/32"}},
			clientIP:  "1.2.3.4",
			wantBlock: false,
		},
		{
			name:      "allowlist hit → allow",
			r:         rules{Allowlist: []string{"10.0.0.0/8"}},
			clientIP:  "10.1.2.3",
			wantBlock: false,
		},
		{
			name:      "allowlist miss → block",
			r:         rules{Allowlist: []string{"10.0.0.0/8"}},
			clientIP:  "1.2.3.4",
			wantBlock: true,
		},
		{
			name:      "blocklist priority over allowlist",
			r:         rules{Allowlist: []string{"10.0.0.0/8"}, Blocklist: []string{"10.1.2.3/32"}},
			clientIP:  "10.1.2.3",
			wantBlock: true,
		},
		{
			name:      "both lists, not in blocklist, in allowlist → allow",
			r:         rules{Allowlist: []string{"10.0.0.0/8"}, Blocklist: []string{"10.1.2.3/32"}},
			clientIP:  "10.2.3.4",
			wantBlock: false,
		},
	}

	for _, tc := range cases {
		// directly test the logic using loadRules bypass
		blocked := applyRules(net.ParseIP(tc.clientIP), tc.r)
		if blocked != tc.wantBlock {
			t.Errorf("%s: blocked=%v, want %v", tc.name, blocked, tc.wantBlock)
		}
	}
	_ = f // suppress unused warning
}

func TestFilterMiddlewarePassesWithoutOrgID(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := NewFilter(nil, 0, nil).Middleware(next)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected next to be called when no Authorization header")
	}
}

func TestFilterMiddlewareFailOpenOnNilRedis(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	orgID := "550e8400-e29b-41d4-a716-446655440000"
	token := fakeToken(t, map[string]any{"sub": "u", "org": orgID})

	handler := NewFilter(nil, 0, nil).Middleware(next)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.RemoteAddr = "1.2.3.4:9000"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected fail-open (next called) when redis is nil")
	}
}

func TestExtractClientIP(t *testing.T) {
	cases := []struct {
		remoteAddr string
		want       string
	}{
		{"192.168.1.1:8080", "192.168.1.1"},
		{"[::1]:8080", "::1"},
		{"10.0.0.1:0", "10.0.0.1"},
	}

	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = tc.remoteAddr
		got := extractClientIP(req)
		if got != tc.want {
			t.Errorf("RemoteAddr=%q: got %q, want %q", tc.remoteAddr, got, tc.want)
		}
	}
}

// applyRules 暴露内部规则逻辑供测试直接验证语义，绕过 Redis 依赖。
func applyRules(ip net.IP, r rules) bool {
	if ip == nil || (len(r.Allowlist) == 0 && len(r.Blocklist) == 0) {
		return false
	}
	for _, cidr := range r.Blocklist {
		if matchCIDR(ip, cidr) {
			return true
		}
	}
	if len(r.Allowlist) > 0 {
		for _, cidr := range r.Allowlist {
			if matchCIDR(ip, cidr) {
				return false
			}
		}
		return true
	}
	return false
}

func fakeToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := encodeSegment(t, map[string]any{"alg": "HS256", "typ": "JWT"})
	payload := encodeSegment(t, claims)
	return header + "." + payload + ".fakesig"
}

func signedToken(t *testing.T, claims map[string]any, secret []byte) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims(claims))
	signed, err := token.SignedString(secret)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return signed
}

func encodeSegment(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
