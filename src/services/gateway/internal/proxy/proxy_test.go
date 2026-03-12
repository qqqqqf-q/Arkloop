//go:build !desktop

package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProxyForwardsRequest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	p, err := New(Config{Upstream: upstream.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != `{"status":"ok"}` {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestProxySetsXForwardedFor(t *testing.T) {
	var capturedXFF string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedXFF = r.Header.Get("X-Forwarded-For")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p, err := New(Config{Upstream: upstream.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.5:8080"
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if capturedXFF != "203.0.113.5" {
		t.Fatalf("X-Forwarded-For: want %q, got %q", "203.0.113.5", capturedXFF)
	}
}

func TestProxySetsXForwardedHost(t *testing.T) {
	var capturedXFH string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedXFH = r.Header.Get("X-Forwarded-Host")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p, err := New(Config{Upstream: upstream.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "api.example.com"
	req.RemoteAddr = "127.0.0.1:9000"
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if capturedXFH != "api.example.com" {
		t.Fatalf("X-Forwarded-Host: want %q, got %q", "api.example.com", capturedXFH)
	}
}

func TestProxySSEFlush(t *testing.T) {
	flushed := make(chan struct{}, 1)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("upstream ResponseWriter does not implement Flusher")
			return
		}
		_, _ = w.Write([]byte("data: hello\n\n"))
		flusher.Flush()
		flushed <- struct{}{}
	}))
	defer upstream.Close()

	p, err := New(Config{Upstream: upstream.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	req.RemoteAddr = "127.0.0.1:5000"
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	select {
	case <-flushed:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("upstream flush was not triggered within timeout")
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
}

func TestNewInvalidUpstream(t *testing.T) {
	cases := []string{
		"",
		"not-a-url",
		"://missing-scheme",
	}
	for _, upstream := range cases {
		_, err := New(Config{Upstream: upstream})
		if err == nil {
			t.Errorf("expected error for upstream=%q, got nil", upstream)
		}
	}
}

func TestProxyDropsClientXForwardedFor(t *testing.T) {
	var capturedXFF string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedXFF = r.Header.Get("X-Forwarded-For")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p, err := New(Config{Upstream: upstream.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Client attempts to spoof XFF.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")

	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	// Spoofed value must not appear; only the real RemoteAddr IP should be set.
	if capturedXFF == "1.2.3.4" || capturedXFF == "1.2.3.4, 10.0.0.1" {
		t.Fatalf("client-supplied XFF was not dropped: %q", capturedXFF)
	}
	if capturedXFF != "10.0.0.1" {
		t.Fatalf("X-Forwarded-For: want %q, got %q", "10.0.0.1", capturedXFF)
	}
}
