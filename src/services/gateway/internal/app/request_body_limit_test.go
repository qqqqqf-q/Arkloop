//go:build !desktop

package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"arkloop/services/gateway/internal/proxy"
)

func TestLimitRequestBodyMiddlewareRejectsKnownLength(t *testing.T) {
	called := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusNoContent)
	})

	handler := limitRequestBodyMiddleware(10, next)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("a", 11)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	if called != 0 {
		t.Fatalf("next handler should not be called, got %d", called)
	}
	if rec.Body.String() != `{"code":"http.request_too_large","message":"request body too large"}` {
		t.Fatalf("unexpected response body: %q", rec.Body.String())
	}
}

func TestLimitRequestBodyMiddlewareRejectsUnknownLength(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	p, err := proxy.New(proxy.Config{Upstream: upstream.URL})
	if err != nil {
		t.Fatalf("proxy.New: %v", err)
	}

	handler := limitRequestBodyMiddleware(10, p)
	body := io.LimitReader(strings.NewReader(strings.Repeat("a", 11)), 11)
	req := httptest.NewRequest(http.MethodPost, "/", body)
	if req.ContentLength != -1 {
		t.Fatalf("expected unknown content length, got %d", req.ContentLength)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != `{"code":"http.request_too_large","message":"request body too large"}` {
		t.Fatalf("unexpected response body: %q", rec.Body.String())
	}
}
