package http

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	nethttp "net/http"
	"net/http/httptest"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	"arkloop/services/api/internal/testutil"
)

func TestReadyzOKWhenDatabaseReachable(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "api_go_readyz_http")

	ctx := context.Background()
	pool, err := data.NewPool(ctx, db.DSN)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	const revision = "test_revision_002"
	if _, err := pool.Exec(ctx, `CREATE TABLE alembic_version (version_num VARCHAR(32) NOT NULL)`); err != nil {
		t.Fatalf("create alembic_version: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO alembic_version (version_num) VALUES ($1)`, revision); err != nil {
		t.Fatalf("insert revision: %v", err)
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

	if recorder.Code != nethttp.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}

	var payload map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	if payload["alembic_revision"] != revision {
		t.Fatalf("unexpected alembic revision: %q", payload["alembic_revision"])
	}
}
