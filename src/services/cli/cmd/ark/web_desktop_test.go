//go:build desktop

package main

import (
	"context"
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

func TestResolveWebRootUsesInjectedHint(t *testing.T) {
	oldHint := webRootHint
	t.Cleanup(func() {
		webRootHint = oldHint
	})

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	webRootHint = root

	got, err := resolveWebRoot("")
	if err != nil {
		t.Fatalf("resolveWebRoot: %v", err)
	}
	want, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("abs root: %v", err)
	}
	if got != want {
		t.Fatalf("resolveWebRoot = %q, want %q", got, want)
	}
}

func TestLocalTrustConfigForHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{host: "localhost", want: true},
		{host: "127.0.0.1", want: true},
		{host: "::1", want: true},
		{host: "0.0.0.0", want: true},
		{host: "::", want: true},
		{host: "", want: true},
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
		})
	}
}

func TestLocalTrustAllowedWithWildcardAllowsPlainLoopback(t *testing.T) {
	plain := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:19080/", nil)
	plain.Host = "127.0.0.1:19080"
	plain.RemoteAddr = "127.0.0.1:50000"
	if !localTrustAllowed(httptest.NewRecorder(), plain, localTrustConfig{Enabled: true}) {
		t.Fatal("expected plain loopback request to allow local trust")
	}

	remote := httptest.NewRequest(http.MethodGet, "http://192.168.1.10:19080/setup", nil)
	remote.Host = "192.168.1.10:19080"
	remote.RemoteAddr = "127.0.0.1:50000"
	if localTrustAllowed(httptest.NewRecorder(), remote, localTrustConfig{Enabled: true}) {
		t.Fatal("expected LAN host to reject local trust")
	}
}

func TestConfigureHeadlessEnvLoadsNowledgeMemoryConfig(t *testing.T) {
	dataDir := t.TempDir()
	config := `{"memory":{"enabled":true,"provider":"nowledge","memoryCommitEachTurn":false,"nowledge":{"baseUrl":"http://127.0.0.1:14242","apiKey":"local-key","requestTimeoutMs":45000}}}`
	if err := os.WriteFile(filepath.Join(dataDir, "config.json"), []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("ARKLOOP_OPENVIKING_BASE_URL", "http://stale-openviking")

	if err := configureHeadlessEnv(19101, 19103, dataDir, ""); err != nil {
		t.Fatalf("configureHeadlessEnv: %v", err)
	}

	if got := os.Getenv("ARKLOOP_API_GO_ADDR"); got != "127.0.0.1:19101" {
		t.Fatalf("ARKLOOP_API_GO_ADDR = %q", got)
	}
	if got := os.Getenv("ARKLOOP_DATA_DIR"); got != dataDir {
		t.Fatalf("ARKLOOP_DATA_DIR = %q", got)
	}
	if got := os.Getenv("ARKLOOP_MEMORY_ENABLED"); got != "true" {
		t.Fatalf("ARKLOOP_MEMORY_ENABLED = %q", got)
	}
	if got := os.Getenv("ARKLOOP_MEMORY_COMMIT_EACH_TURN"); got != "false" {
		t.Fatalf("ARKLOOP_MEMORY_COMMIT_EACH_TURN = %q", got)
	}
	if got := os.Getenv("ARKLOOP_MEMORY_PROVIDER"); got != "nowledge" {
		t.Fatalf("ARKLOOP_MEMORY_PROVIDER = %q", got)
	}
	if got := os.Getenv("ARKLOOP_NOWLEDGE_BASE_URL"); got != "http://127.0.0.1:14242" {
		t.Fatalf("ARKLOOP_NOWLEDGE_BASE_URL = %q", got)
	}
	if got := os.Getenv("ARKLOOP_NOWLEDGE_API_KEY"); got != "local-key" {
		t.Fatalf("ARKLOOP_NOWLEDGE_API_KEY = %q", got)
	}
	if got := os.Getenv("ARKLOOP_NOWLEDGE_REQUEST_TIMEOUT_MS"); got != "45000" {
		t.Fatalf("ARKLOOP_NOWLEDGE_REQUEST_TIMEOUT_MS = %q", got)
	}
	if got := os.Getenv("ARKLOOP_OPENVIKING_BASE_URL"); got != "" {
		t.Fatalf("ARKLOOP_OPENVIKING_BASE_URL = %q", got)
	}
}

func TestConfigureHeadlessEnvDefaultsToNotebookWithoutConfig(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("ARKLOOP_NOWLEDGE_BASE_URL", "http://stale-nowledge")

	if err := configureHeadlessEnv(19101, 19103, dataDir, ""); err != nil {
		t.Fatalf("configureHeadlessEnv: %v", err)
	}

	if got := os.Getenv("ARKLOOP_MEMORY_ENABLED"); got != "true" {
		t.Fatalf("ARKLOOP_MEMORY_ENABLED = %q", got)
	}
	if got := os.Getenv("ARKLOOP_MEMORY_COMMIT_EACH_TURN"); got != "true" {
		t.Fatalf("ARKLOOP_MEMORY_COMMIT_EACH_TURN = %q", got)
	}
	if got := os.Getenv("ARKLOOP_MEMORY_PROVIDER"); got != "notebook" {
		t.Fatalf("ARKLOOP_MEMORY_PROVIDER = %q", got)
	}
	if got := os.Getenv("ARKLOOP_NOWLEDGE_BASE_URL"); got != "" {
		t.Fatalf("ARKLOOP_NOWLEDGE_BASE_URL = %q", got)
	}
}

func TestServeIndexInjectsDesktopInfoForPlainLoopback(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<html><head></head><body></body></html>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	t.Setenv("ARKLOOP_DESKTOP_TOKEN", "arkloop-desktop-token")

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:19080/", nil)
	req.Host = "127.0.0.1:19080"
	req.RemoteAddr = "127.0.0.1:50000"
	rec := httptest.NewRecorder()
	serveIndex(rec, req, root, localTrustConfig{Enabled: true})
	got := rec.Body.String()
	if !strings.Contains(got, "window.__ARKLOOP_DESKTOP__") {
		t.Fatalf("expected local desktop injection: %s", got)
	}
	if !strings.Contains(got, `getMode:function(){return "local"}`) {
		t.Fatalf("expected local mode getter: %s", got)
	}
}

func TestServeIndexDoesNotInjectDesktopInfoForLANHost(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<html><head></head><body></body></html>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	t.Setenv("ARKLOOP_DESKTOP_TOKEN", "arkloop-desktop-token")

	req := httptest.NewRequest(http.MethodGet, "http://192.168.1.10:19080/", nil)
	req.Host = "192.168.1.10:19080"
	req.RemoteAddr = "127.0.0.1:50000"
	rec := httptest.NewRecorder()
	serveIndex(rec, req, root, localTrustConfig{Enabled: true})
	if got := rec.Body.String(); strings.Contains(got, "window.__ARKLOOP_DESKTOP__") {
		t.Fatalf("unexpected desktop injection: %s", got)
	}
}

func TestDesktopOwnerPasswordCredentialExists(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()

	exists, err := desktopOwnerPasswordCredentialExists(ctx, dataDir)
	if err != nil {
		t.Fatalf("credential exists before setup: %v", err)
	}
	if exists {
		t.Fatal("expected no owner password before setup")
	}

	if err := setDesktopOwnerPassword(ctx, dataDir, "owner-web", "abc12345"); err != nil {
		t.Fatalf("set owner password: %v", err)
	}
	exists, err = desktopOwnerPasswordCredentialExists(ctx, dataDir)
	if err != nil {
		t.Fatalf("credential exists after setup: %v", err)
	}
	if !exists {
		t.Fatal("expected owner password after setup")
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
