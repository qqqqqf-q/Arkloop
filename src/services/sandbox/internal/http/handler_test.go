package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"arkloop/services/sandbox/internal/logging"
)

func newTestLogger() *logging.JSONLogger {
	return logging.NewJSONLogger("test", nil)
}

func TestAuthMiddleware_EnforcedWithValidToken(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := authMiddleware(inner, "secret-token", newTestLogger())

	req := httptest.NewRequest(http.MethodGet, "/v1/stats", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAuthMiddleware_EnforcedWithInvalidToken(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})
	handler := authMiddleware(inner, "secret-token", newTestLogger())

	req := httptest.NewRequest(http.MethodGet, "/v1/stats", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_EnforcedWithNoToken(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})
	handler := authMiddleware(inner, "secret-token", newTestLogger())

	req := httptest.NewRequest(http.MethodGet, "/v1/stats", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_HealthzBypass(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := authMiddleware(inner, "secret-token", newTestLogger())

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for healthz, got %d", rec.Code)
	}
}

func TestAuthMiddleware_NotEnforced(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := authMiddleware(inner, "", newTestLogger())

	req := httptest.NewRequest(http.MethodGet, "/v1/stats", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 when auth not enforced, got %d", rec.Code)
	}
}
