package http

import (
	"encoding/json"
	"io"
	"testing"

	nethttp "net/http"
	"net/http/httptest"

	"arkloop/services/api/internal/data"
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
		t.Fatalf("unexpected status: %d", recorder.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	version, ok := payload["schema_version"].(float64)
	if !ok || int64(version) != 9 {
		t.Fatalf("unexpected schema_version: %v", payload["schema_version"])
	}
}
