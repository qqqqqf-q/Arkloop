//go:build !desktop

package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"arkloop/services/gateway/internal/geoip"
)

func TestSecurityHeadersMiddlewareAllowsConfiguredOrigin(t *testing.T) {
	called := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusAccepted)
	})

	handler := traceMiddleware(
		securityHeadersMiddleware([]string{"http://localhost:19080"}, next),
		nil,
		geoip.Noop{},
		nil,
		0,
		nil,
		false,
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "http://localhost:19080")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if called != 1 {
		t.Fatalf("expected next to be called once, got %d", called)
	}
	if rec.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != frontendCSPHeaderValue {
		t.Fatalf("unexpected CSP header: %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:19080" {
		t.Fatalf("unexpected allow-origin: %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("unexpected allow-credentials: %q", got)
	}
	if got := rec.Header().Get("Access-Control-Expose-Headers"); got != traceIDHeader {
		t.Fatalf("unexpected expose-headers: %q", got)
	}
	if got := rec.Header().Get(traceIDHeader); got == "" {
		t.Fatal("missing trace header")
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("unexpected vary header: %q", got)
	}
}

func TestSecurityHeadersMiddlewareHandlesPreflight(t *testing.T) {
	called := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusNoContent)
	})

	handler := traceMiddleware(
		securityHeadersMiddleware(DefaultConfig().CORSAllowedOrigins, next),
		nil,
		geoip.Noop{},
		nil,
		0,
		nil,
		false,
	)

	req := httptest.NewRequest(http.MethodOptions, "/v1/auth/refresh", nil)
	req.Header.Set("Origin", "http://localhost:19081")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if called != 0 {
		t.Fatalf("preflight should not hit next, got %d", called)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:19081" {
		t.Fatalf("unexpected allow-origin: %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != corsAllowMethodsValue {
		t.Fatalf("unexpected allow-methods: %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != corsAllowHeadersValue {
		t.Fatalf("unexpected allow-headers: %q", got)
	}
	if got := rec.Header().Get(traceIDHeader); got == "" {
		t.Fatal("missing trace header")
	}
}

func TestSecurityHeadersMiddlewareRejectsUnknownOrigin(t *testing.T) {
	called := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	})

	handler := traceMiddleware(
		securityHeadersMiddleware(DefaultConfig().CORSAllowedOrigins, next),
		nil,
		geoip.Noop{},
		nil,
		0,
		nil,
		false,
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if called != 1 {
		t.Fatalf("expected next to be called once, got %d", called)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unexpected allow-origin: %q", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != frontendCSPHeaderValue {
		t.Fatalf("unexpected CSP header: %q", got)
	}
}

func TestSecurityHeadersMiddlewareKeepsStrictCSPForAPI(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := traceMiddleware(
		securityHeadersMiddleware(DefaultConfig().CORSAllowedOrigins, next),
		nil,
		geoip.Noop{},
		nil,
		0,
		nil,
		false,
	)

	req := httptest.NewRequest(http.MethodGet, "/v1/bootstrap/init", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Security-Policy"); got != cspHeaderValue {
		t.Fatalf("unexpected API CSP header: %q", got)
	}
}
