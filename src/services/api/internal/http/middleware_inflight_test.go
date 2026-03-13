//go:build !desktop

package http

import (
	"encoding/json"
	"io"
	"sync/atomic"
	"testing"

	nethttp "net/http"
	"net/http/httptest"

	"arkloop/services/api/internal/observability"
)

func TestInFlightMiddlewareRejectsWhenBusy(t *testing.T) {
	logger := observability.NewJSONLogger("test", io.Discard)

	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	doneFirst := make(chan struct{})

	next := nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		select {
		case <-firstEntered:
		default:
			close(firstEntered)
			<-releaseFirst
		}
		w.WriteHeader(nethttp.StatusOK)
	})

	handler := TraceMiddleware(InFlightMiddleware(next, 1), logger, false, false)

	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
	go func() {
		handler.ServeHTTP(rec1, req1)
		close(doneFirst)
	}()

	<-firstEntered

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != nethttp.StatusServiceUnavailable {
		t.Fatalf("unexpected status: %d body=%s", rec2.Code, rec2.Body.String())
	}

	traceID := rec2.Header().Get(observability.TraceIDHeader)
	if traceID == "" {
		t.Fatalf("missing %s header", observability.TraceIDHeader)
	}

	var payload ErrorEnvelope
	if err := json.Unmarshal(rec2.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Code != "overload.busy" {
		t.Fatalf("unexpected code: %q", payload.Code)
	}
	if payload.TraceID != traceID {
		t.Fatalf("trace_id mismatch: header=%q payload=%q", traceID, payload.TraceID)
	}

	details, ok := payload.Details.(map[string]any)
	if !ok {
		t.Fatalf("unexpected details type: %T", payload.Details)
	}
	if got, ok := details["limit"]; !ok || got == nil {
		t.Fatalf("missing details.limit: %#v", details)
	}

	close(releaseFirst)
	<-doneFirst

	if rec1.Code != nethttp.StatusOK {
		t.Fatalf("unexpected first status: %d body=%s", rec1.Code, rec1.Body.String())
	}
}

func TestInFlightMiddlewareBypassPaths(t *testing.T) {
	logger := observability.NewJSONLogger("test", io.Discard)

	var calls int64
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	doneFirst := make(chan struct{})

	next := nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		n := atomic.AddInt64(&calls, 1)
		if n == 1 {
			close(firstEntered)
			<-releaseFirst
		}
		w.WriteHeader(nethttp.StatusOK)
	})

	handler := TraceMiddleware(InFlightMiddleware(next, 1), logger, false, false)

	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
	go func() {
		handler.ServeHTTP(rec1, req1)
		close(doneFirst)
	}()

	<-firstEntered

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(nethttp.MethodGet, "/healthz", nil)
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != nethttp.StatusOK {
		t.Fatalf("unexpected status for /healthz: %d body=%s", rec2.Code, rec2.Body.String())
	}

	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(nethttp.MethodGet, "/v1/runs/00000000-0000-0000-0000-000000000001/events", nil)
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != nethttp.StatusOK {
		t.Fatalf("unexpected status for /v1/runs/*/events: %d body=%s", rec3.Code, rec3.Body.String())
	}

	close(releaseFirst)
	<-doneFirst
}
