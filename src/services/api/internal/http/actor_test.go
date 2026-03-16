package http

import (
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"arkloop/services/api/internal/http/httpkit"
)

func TestResolveActorAuthServiceNilReturns503(t *testing.T) {
	req := httptest.NewRequest(nethttp.MethodGet, "/v1/config/schema", nil)
	req.Header.Set("Authorization", "Bearer some-token")

	recorder := httptest.NewRecorder()
	_, ok := httpkit.ResolveActor(recorder, req, "trace123", nil, nil, nil, nil)
	if ok {
		t.Fatalf("expected ok=false")
	}
	if recorder.Code != nethttp.StatusServiceUnavailable {
		t.Fatalf("unexpected status: %d body=%s", recorder.Code, recorder.Body.String())
	}

	var payload ErrorEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v body=%s", err, recorder.Body.String())
	}
	if payload.Code != "auth.not_configured" {
		t.Fatalf("unexpected code: %q", payload.Code)
	}
	if payload.TraceID != "trace123" {
		t.Fatalf("unexpected trace_id: %q", payload.TraceID)
	}
}
