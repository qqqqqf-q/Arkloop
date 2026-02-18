package data

import (
	"context"
	"testing"

	"arkloop/services/api/internal/testutil"
)

func TestSchemaRepositoryCurrentAlembicVersion(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "api_go_readyz")

	ctx := context.Background()
	pool, err := NewPool(ctx, db.DSN)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	const revision = "test_revision_001"
	_, err = pool.Exec(ctx, `CREATE TABLE alembic_version (version_num VARCHAR(32) NOT NULL)`)
	if err != nil {
		t.Fatalf("create alembic_version: %v", err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO alembic_version (version_num) VALUES ($1)`, revision)
	if err != nil {
		t.Fatalf("insert revision: %v", err)
	}

	repo, err := NewSchemaRepository(pool)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}

	got, err := repo.CurrentAlembicVersion(ctx)
	if err != nil {
		t.Fatalf("current revision: %v", err)
	}
	if got != revision {
		t.Fatalf("unexpected revision: %q", got)
	}
}
