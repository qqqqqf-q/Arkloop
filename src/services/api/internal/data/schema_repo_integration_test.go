package data

import (
	"context"
	"testing"

	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/testutil"
)

func TestSchemaRepositoryCurrentSchemaVersion(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "api_go_schema")

	ctx := context.Background()

	if _, err := migrate.Up(ctx, db.DSN); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	pool, err := NewPool(ctx, db.DSN)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	repo, err := NewSchemaRepository(pool)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}

	got, err := repo.CurrentSchemaVersion(ctx)
	if err != nil {
		t.Fatalf("current version: %v", err)
	}
	if got != migrate.ExpectedVersion {
		t.Fatalf("unexpected version: %d (expected %d)", got, migrate.ExpectedVersion)
	}
}
