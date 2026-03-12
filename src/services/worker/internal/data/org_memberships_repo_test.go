//go:build !desktop

package data

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/testutil"

	"arkloop/services/shared/database/pgadapter"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOrgMembershipsRepository_GetByOrgAndUser(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_org_memberships_lookup")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()
dbPool := pgadapter.New(pool)

	orgID := uuid.New()
	userID := uuid.New()
	_, err = pool.Exec(
		context.Background(),
		`INSERT INTO org_memberships (org_id, user_id, role)
		 VALUES ($1, $2, $3)`,
		orgID,
		userID,
		"org_admin",
	)
	if err != nil {
		t.Fatalf("insert membership: %v", err)
	}

	repo := OrgMembershipsRepository{}
	record, err := repo.GetByOrgAndUser(context.Background(), dbPool, orgID, userID)
	if err != nil {
		t.Fatalf("get membership: %v", err)
	}
	if record == nil {
		t.Fatal("expected membership record")
	}
	if record.Role != "org_admin" {
		t.Fatalf("unexpected role: %s", record.Role)
	}
}

func TestOrgMembershipsRepository_GetByOrgAndUser_NotFound(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_org_memberships_not_found")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()
dbPool := pgadapter.New(pool)

	repo := OrgMembershipsRepository{}
	record, err := repo.GetByOrgAndUser(context.Background(), dbPool, uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("get membership: %v", err)
	}
	if record != nil {
		t.Fatalf("expected nil record, got %#v", record)
	}
}
