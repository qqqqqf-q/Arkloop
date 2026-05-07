package websearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sharedoutbound "arkloop/services/shared/outboundurl"
)

func TestBasicProviderSearchCallsDesktopEndpoint(t *testing.T) {
	t.Setenv(sharedoutbound.ProtectionEnabledEnv, "true")
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")

	redirectValue := "a1aHR0cHM6Ly9leGFtcGxlLmNvbS9i"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Fatalf("expected /search path, got %q", r.URL.Path)
		}
		if r.URL.Query().Get("q") != "arkloop search" {
			t.Fatalf("query: %q", r.URL.Query().Get("q"))
		}
		if r.URL.Query().Get("max_results") != "3" {
			t.Fatalf("max_results: %q", r.URL.Query().Get("max_results"))
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]string{
				{"title": "Example A", "url": "https://example.com/a", "snippet": "Snippet A"},
				{"title": "Example B", "url": "https://www.bing.com/ck/a?u=" + redirectValue, "snippet": "Snippet B"},
				{"title": "Duplicate A", "url": "https://example.com/a#section", "snippet": "Duplicate snippet"},
				{"title": "Local", "url": "http://127.0.0.1/private", "snippet": "Unsafe"},
			},
		})
	}))
	t.Cleanup(srv.Close)

	p := NewBasicProviderWithEndpoint(srv.URL+"/search", "test-token")
	got, err := p.Search(context.Background(), "arkloop search", 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2: %#v", len(got), got)
	}
	if got[0].Title != "Example A" || got[0].URL != "https://example.com/a" || got[0].Snippet != "Snippet A" {
		t.Fatalf("first result: %#v", got[0])
	}
	if got[1].Title != "Example B" || got[1].URL != "https://example.com/b" || got[1].Snippet != "Snippet B" {
		t.Fatalf("second result: %#v", got[1])
	}
}

func TestBasicProviderSearchRequiresDesktopEndpoint(t *testing.T) {
	p := NewBasicProviderWithEndpoint("", "")
	_, err := p.Search(context.Background(), "q", 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "endpoint not configured") {
		t.Fatalf("error: %v", err)
	}
}

func TestBasicProviderSearchRejectsEmptyEndpointResults(t *testing.T) {
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	t.Cleanup(srv.Close)

	p := NewBasicProviderWithEndpoint(srv.URL+"/search", "")
	_, err := p.Search(context.Background(), "q", 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no usable results") {
		t.Fatalf("error: %v", err)
	}
}

func TestBasicProviderSearchHTTPError(t *testing.T) {
	t.Setenv(sharedoutbound.AllowLoopbackHTTPEnv, "true")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "blocked", http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	p := NewBasicProviderWithEndpoint(srv.URL+"/search", "")
	_, err := p.Search(context.Background(), "q", 1)
	if err == nil {
		t.Fatal("expected error")
	}
	httpErr, ok := err.(HttpError)
	if !ok {
		t.Fatalf("expected HttpError, got %T %v", err, err)
	}
	if httpErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status: %d", httpErr.StatusCode)
	}
}
