//go:build !desktop

package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"arkloop/services/gateway/internal/geoip"
)

func TestTraceMiddlewareIgnoresIncomingTraceIDByDefault(t *testing.T) {
	incoming := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var seen string

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get(traceIDHeader)
		w.WriteHeader(http.StatusNoContent)
	})

	handler := traceMiddleware(next, nil, geoip.Noop{}, nil, 0, nil, false)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(traceIDHeader, incoming)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	got := rec.Header().Get(traceIDHeader)
	if got == incoming {
		t.Fatalf("trace id should not trust client input by default: %q", got)
	}
	if len(got) != 32 {
		t.Fatalf("unexpected trace id length: %d", len(got))
	}
	if seen != got {
		t.Fatalf("forwarded trace id = %q, want %q", seen, got)
	}
}

func TestTraceMiddlewareInjectsGeneratedTraceID(t *testing.T) {
	var seen string

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get(traceIDHeader)
		w.WriteHeader(http.StatusNoContent)
	})

	handler := traceMiddleware(next, nil, geoip.Noop{}, nil, 0, nil, false)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	got := rec.Header().Get(traceIDHeader)
	if got == "" {
		t.Fatal("response trace id should not be empty")
	}
	if seen != got {
		t.Fatalf("forwarded trace id = %q, want %q", seen, got)
	}
}

func TestTraceMiddlewareTrustsIncomingTraceIDWhenEnabled(t *testing.T) {
	incoming := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	var seen string

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get(traceIDHeader)
		w.WriteHeader(http.StatusNoContent)
	})

	handler := traceMiddleware(next, nil, geoip.Noop{}, nil, 0, nil, true)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(traceIDHeader, incoming)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get(traceIDHeader); got != incoming {
		t.Fatalf("response trace id = %q, want %q", got, incoming)
	}
	if seen != incoming {
		t.Fatalf("forwarded trace id = %q, want %q", seen, incoming)
	}
}

func TestTraceMiddlewareRejectsInvalidIncomingTraceID(t *testing.T) {
	invalid := "not-a-valid-trace-id"
	var seen string

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get(traceIDHeader)
		w.WriteHeader(http.StatusNoContent)
	})

	handler := traceMiddleware(next, nil, geoip.Noop{}, nil, 0, nil, true)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(traceIDHeader, invalid)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	got := rec.Header().Get(traceIDHeader)
	if got == invalid {
		t.Fatalf("invalid trace id should not be trusted: %q", got)
	}
	if len(got) != 32 {
		t.Fatalf("unexpected trace id length: %d", len(got))
	}
	if seen != got {
		t.Fatalf("forwarded trace id = %q, want %q", seen, got)
	}
}
