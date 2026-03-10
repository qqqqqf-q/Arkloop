package http

import (
	"encoding/json"
	"io"
	"regexp"
	"testing"

	nethttp "net/http"
	"net/http/httptest"

	"arkloop/services/api/internal/observability"
)

// flusherRecorder is a test recorder that also implements http.Flusher,
// used to verify that the Flusher assertion is preserved after middleware wrapping.
type flusherRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (f *flusherRecorder) Flush() {
	f.flushed = true
	f.ResponseRecorder.Flush()
}

func TestHealthz(t *testing.T) {
	logger := observability.NewJSONLogger("test", io.Discard)
	handler := NewHandler(HandlerConfig{Logger: logger})

	req := httptest.NewRequest(nethttp.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != nethttp.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}

	traceID := recorder.Header().Get(observability.TraceIDHeader)
	if traceID == "" {
		t.Fatalf("missing %s header", observability.TraceIDHeader)
	}

	var payload map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestNotFoundReturnsEnvelope(t *testing.T) {
	logger := observability.NewJSONLogger("test", io.Discard)
	handler := NewHandler(HandlerConfig{Logger: logger})

	req := httptest.NewRequest(nethttp.MethodGet, "/nope", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != nethttp.StatusNotFound {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}

	traceID := recorder.Header().Get(observability.TraceIDHeader)
	if traceID == "" {
		t.Fatalf("missing %s header", observability.TraceIDHeader)
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(traceID) {
		t.Fatalf("invalid trace id: %q", traceID)
	}

	var payload ErrorEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.TraceID != traceID {
		t.Fatalf("trace_id mismatch: header=%q payload=%q", traceID, payload.TraceID)
	}
	if payload.Code != "http.method_not_allowed" {
		t.Fatalf("unexpected code: %q", payload.Code)
	}
	if payload.Message == "" {
		t.Fatalf("missing message")
	}
}

func TestReadyzRequiresDatabase(t *testing.T) {
	logger := observability.NewJSONLogger("test", io.Discard)
	handler := NewHandler(HandlerConfig{Logger: logger})

	req := httptest.NewRequest(nethttp.MethodGet, "/readyz", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != nethttp.StatusServiceUnavailable {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}

	traceID := recorder.Header().Get(observability.TraceIDHeader)
	if traceID == "" {
		t.Fatalf("missing %s header", observability.TraceIDHeader)
	}

	var payload ErrorEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Code != "health.not_ready" {
		t.Fatalf("unexpected code: %q", payload.Code)
	}
	if payload.TraceID != traceID {
		t.Fatalf("trace_id mismatch: header=%q payload=%q", traceID, payload.TraceID)
	}
}

// TestTraceMiddlewarePreservesHttpFlusher verifies that the http.Flusher capability
// is preserved after wrapping with TraceMiddleware.
// SSE/streaming depends on this; if the assertion fails, connections are established
// but data gets buffered and never sent.
func TestTraceMiddlewarePreservesHttpFlusher(t *testing.T) {
	logger := observability.NewJSONLogger("test", io.Discard)

	var capturedWriter nethttp.ResponseWriter
	inner := nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		capturedWriter = w
		w.WriteHeader(nethttp.StatusOK)
	})

	handler := TraceMiddleware(inner, logger, false, false)

	underlying := &flusherRecorder{ResponseRecorder: httptest.NewRecorder()}
	req := httptest.NewRequest(nethttp.MethodGet, "/healthz", nil)
	handler.ServeHTTP(underlying, req)

	if capturedWriter == nil {
		t.Fatal("capturedWriter is nil")
	}
	if _, ok := capturedWriter.(nethttp.Flusher); !ok {
		t.Fatal("TraceMiddleware wrapping lost the http.Flusher interface on ResponseWriter")
	}
}

func TestTraceMiddlewareStoresTrustedRequestMetadata(t *testing.T) {
	logger := observability.NewJSONLogger("test", io.Discard)

	inner := nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		ip := observability.ClientIPFromContext(r.Context())
		https, ok := observability.RequestHTTPSFromContext(r.Context())
		if ip != "203.0.113.8" {
			t.Fatalf("client ip = %q, want %q", ip, "203.0.113.8")
		}
		if !ok || !https {
			t.Fatalf("https metadata = (%v, %v), want (true, true)", https, ok)
		}
		w.WriteHeader(nethttp.StatusOK)
	})

	handler := TraceMiddleware(inner, logger, false, true)
	req := httptest.NewRequest(nethttp.MethodGet, "/healthz", nil)
	req.RemoteAddr = "10.0.0.5:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.8, 10.0.0.5")
	req.Header.Set("X-Forwarded-Proto", "https")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != nethttp.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
}

func TestTraceMiddlewareIgnoresUntrustedForwardedMetadata(t *testing.T) {
	logger := observability.NewJSONLogger("test", io.Discard)

	inner := nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		ip := observability.ClientIPFromContext(r.Context())
		https, ok := observability.RequestHTTPSFromContext(r.Context())
		if ip != "10.0.0.5" {
			t.Fatalf("client ip = %q, want %q", ip, "10.0.0.5")
		}
		if !ok || https {
			t.Fatalf("https metadata = (%v, %v), want (false, true)", https, ok)
		}
		w.WriteHeader(nethttp.StatusOK)
	})

	handler := TraceMiddleware(inner, logger, false, false)
	req := httptest.NewRequest(nethttp.MethodGet, "/healthz", nil)
	req.RemoteAddr = "10.0.0.5:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.8, 10.0.0.5")
	req.Header.Set("X-Forwarded-Proto", "https")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != nethttp.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
}

// TestThreadSubResourceRouting verifies that /v1/threads/{uuid}/messages and similar
// sub-resource paths don't return 422 due to uuid parse errors, proving the routing
// split logic correctly identifies segments.
func TestThreadSubResourceRouting(t *testing.T) {
	logger := observability.NewJSONLogger("test", io.Discard)
	handler := NewHandler(HandlerConfig{Logger: logger})

	paths := []string{
		"/v1/threads/00000000-0000-0000-0000-000000000001/messages",
		"/v1/threads/00000000-0000-0000-0000-000000000001/runs",
	}

	for _, path := range paths {
		req := httptest.NewRequest(nethttp.MethodGet, path, nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		if recorder.Code == nethttp.StatusUnprocessableEntity {
			t.Fatalf("path=%s: expected not 422, got %d body=%s", path, recorder.Code, recorder.Body.String())
		}
	}

	req := httptest.NewRequest(nethttp.MethodGet, "/v1/threads/00000000-0000-0000-0000-000000000001/unknown", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != nethttp.StatusNotFound {
		t.Fatalf("path=%s: expected 404, got %d body=%s", req.URL.Path, recorder.Code, recorder.Body.String())
	}
}
