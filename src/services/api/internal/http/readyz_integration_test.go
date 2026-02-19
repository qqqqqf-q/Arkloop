package http

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	nethttp "net/http"
	"net/http/httptest"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/observability"
)

func TestReadyzOKWhenDatabaseReachable(t *testing.T) {
	db := setupTestDatabase(t, "api_go_readyz_http")

	pool, err := data.NewPool(t.Context(), db.DSN)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	schemaRepo, err := data.NewSchemaRepository(pool)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}

	logger := observability.NewJSONLogger("test", io.Discard)
	handler := NewHandler(HandlerConfig{Logger: logger, SchemaRepository: schemaRepo})

	req := httptest.NewRequest(nethttp.MethodGet, "/readyz", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != nethttp.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", recorder.Code, recorder.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	version, ok := payload["schema_version"].(float64)
	if !ok || int64(version) != migrate.ExpectedVersion {
		t.Fatalf("unexpected schema_version: %v (expected %d)", payload["schema_version"], migrate.ExpectedVersion)
	}
	if payload["match"] != true {
		t.Fatalf("expected match=true, got %v", payload["match"])
	}
}

func TestReadyz503WhenSchemaMismatch(t *testing.T) {
	db := setupTestDatabase(t, "api_go_readyz_mismatch")

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	// Roll back one migration to create a version mismatch.
	if _, err := migrate.DownOne(ctx, db.DSN); err != nil {
		t.Fatalf("down one: %v", err)
	}

	schemaRepo, err := data.NewSchemaRepository(pool)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}

	logger := observability.NewJSONLogger("test", io.Discard)
	handler := NewHandler(HandlerConfig{Logger: logger, SchemaRepository: schemaRepo})

	req := httptest.NewRequest(nethttp.MethodGet, "/readyz", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != nethttp.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	var payload ErrorEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Code != "not_ready" {
		t.Fatalf("expected code not_ready, got %q", payload.Code)
	}
}
