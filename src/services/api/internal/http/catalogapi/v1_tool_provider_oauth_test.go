package catalogapi

import (
	nethttp "net/http"
	"net/http/httptest"
	"testing"
)

func TestToolProviderOAuthCallbackAllowsXAIPreflight(t *testing.T) {
	handler := toolProviderOAuthCallbackEntry(nil, nil, nil)
	req := httptest.NewRequest(nethttp.MethodOptions, "http://127.0.0.1:56121/callback", nil)
	req.Header.Set("Origin", "https://accounts.x.ai")
	req.Header.Set("Access-Control-Request-Private-Network", "true")
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != nethttp.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, nethttp.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://accounts.x.ai" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Private-Network"); got != "true" {
		t.Fatalf("allow private network = %q", got)
	}
}

func TestToolProviderOAuthCallbackRejectsUnknownPreflightOrigin(t *testing.T) {
	handler := toolProviderOAuthCallbackEntry(nil, nil, nil)
	req := httptest.NewRequest(nethttp.MethodOptions, "http://127.0.0.1:56121/callback", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != nethttp.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, nethttp.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Private-Network"); got != "" {
		t.Fatalf("allow private network = %q", got)
	}
}
