//go:build desktop

package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsLocalWebRequestRequiresLoopbackHostAndRemote(t *testing.T) {
	tests := []struct {
		name           string
		host           string
		remote         string
		xForwardedHost string
		xForwardedFor  string
		want           bool
	}{
		{name: "localhost loopback", host: "localhost:19080", remote: "127.0.0.1:50000", want: true},
		{name: "ipv4 loopback", host: "127.0.0.1:19080", remote: "127.0.0.1:50000", want: true},
		{name: "ipv6 loopback", host: "[::1]:19080", remote: "[::1]:50000", want: true},
		{name: "lan host", host: "192.168.1.10:19080", remote: "127.0.0.1:50000", want: false},
		{name: "tunnel host", host: "example.trycloudflare.com", remote: "127.0.0.1:50000", want: false},
		{name: "remote addr", host: "localhost:19080", remote: "192.168.1.5:50000", want: false},
		{name: "forwarded public host", host: "127.0.0.1:19080", remote: "127.0.0.1:50000", xForwardedHost: "example.com", want: false},
		{name: "forwarded public client", host: "127.0.0.1:19080", remote: "127.0.0.1:50000", xForwardedFor: "203.0.113.10", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &http.Request{Host: tt.host, RemoteAddr: tt.remote}
			if tt.xForwardedHost != "" || tt.xForwardedFor != "" {
				req.Header = http.Header{}
			}
			if tt.xForwardedHost != "" {
				req.Header.Set("X-Forwarded-Host", tt.xForwardedHost)
			}
			if tt.xForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwardedFor)
			}
			if got := isLocalWebRequest(req); got != tt.want {
				t.Fatalf("isLocalWebRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInjectDesktopInfoAddsLocalModeScript(t *testing.T) {
	got := string(injectDesktopInfo([]byte("<html><head></head><body></body></html>"), "arkloop-desktop-token"))
	if !strings.Contains(got, "window.__ARKLOOP_DESKTOP__") {
		t.Fatalf("missing desktop injection: %s", got)
	}
	if !strings.Contains(got, "getApiBaseUrl:function(){return window.location.origin}") {
		t.Fatalf("missing same-origin api getter: %s", got)
	}
	if !strings.Contains(got, "</head>") {
		t.Fatalf("index head was not preserved: %s", got)
	}
}

func TestResolveAssetPathStaysInsideWebRoot(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name string
		path string
		want string
		ok   bool
	}{
		{name: "root falls back to index", path: "/", ok: false},
		{name: "asset path", path: "/assets/app.js", want: filepath.Join(root, "assets", "app.js"), ok: true},
		{name: "slash traversal collapses inside root", path: "/../data.db", want: filepath.Join(root, "data.db"), ok: true},
		{name: "backslash traversal collapses inside root", path: `/..\data.db`, want: filepath.Join(root, "data.db"), ok: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := resolveAssetPath(root, tt.path)
			if ok != tt.ok {
				t.Fatalf("resolveAssetPath ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("resolveAssetPath path = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAssetPathWithinRootRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	link := filepath.Join(root, "secret.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if assetPathWithinRoot(root, link) {
		t.Fatal("expected symlink escape to be rejected")
	}
}

func TestLocalTrustConfigForHost(t *testing.T) {
	tests := []struct {
		host      string
		want      bool
		wantToken bool
	}{
		{host: "localhost", want: true, wantToken: true},
		{host: "127.0.0.1", want: true, wantToken: true},
		{host: "::1", want: true, wantToken: true},
		{host: "0.0.0.0", want: true, wantToken: true},
		{host: "::", want: true, wantToken: true},
		{host: "", want: true, wantToken: true},
		{host: "192.168.1.10", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			got, err := localTrustConfigForHost(tt.host)
			if err != nil {
				t.Fatalf("localTrustConfigForHost(%q): %v", tt.host, err)
			}
			if got.Enabled != tt.want {
				t.Fatalf("localTrustConfigForHost(%q).Enabled = %v, want %v", tt.host, got.Enabled, tt.want)
			}
			if (got.Token != "") != tt.wantToken {
				t.Fatalf("localTrustConfigForHost(%q).Token presence = %v, want %v", tt.host, got.Token != "", tt.wantToken)
			}
		})
	}
}

func TestLocalTrustAllowedWithWildcardRequiresURLTokenOrCookie(t *testing.T) {
	cfg := localTrustConfig{Enabled: true, Token: "setup-token"}

	plain := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:19080/", nil)
	plain.Host = "127.0.0.1:19080"
	plain.RemoteAddr = "127.0.0.1:50000"
	if localTrustAllowed(httptest.NewRecorder(), plain, cfg) {
		t.Fatal("expected wildcard local trust to require setup token")
	}

	withToken := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:19080/setup?"+localTrustTokenQuery+"=setup-token", nil)
	withToken.Host = "127.0.0.1:19080"
	withToken.RemoteAddr = "127.0.0.1:50000"
	rec := httptest.NewRecorder()
	if !localTrustAllowed(rec, withToken, cfg) {
		t.Fatal("expected matching setup token to allow local trust")
	}
	if cookie := rec.Result().Cookies(); len(cookie) != 1 || cookie[0].Name != localTrustCookieName {
		t.Fatalf("expected local trust cookie, got %#v", cookie)
	}

	withCookie := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:19080/", nil)
	withCookie.Host = "127.0.0.1:19080"
	withCookie.RemoteAddr = "127.0.0.1:50000"
	withCookie.AddCookie(&http.Cookie{Name: localTrustCookieName, Value: "setup-token"})
	if !localTrustAllowed(httptest.NewRecorder(), withCookie, cfg) {
		t.Fatal("expected matching cookie to allow local trust")
	}

	remote := httptest.NewRequest(http.MethodGet, "http://192.168.1.10:19080/setup?"+localTrustTokenQuery+"=setup-token", nil)
	remote.Host = "192.168.1.10:19080"
	remote.RemoteAddr = "127.0.0.1:50000"
	if localTrustAllowed(httptest.NewRecorder(), remote, cfg) {
		t.Fatal("expected LAN host to reject local trust even with token")
	}
}

func TestValidateOwnerPassword(t *testing.T) {
	if err := validateOwnerPassword("abc12345"); err != nil {
		t.Fatalf("expected valid password: %v", err)
	}
	if err := validateOwnerPassword("abcdefgh"); err == nil {
		t.Fatal("expected password without digit to fail")
	}
}
