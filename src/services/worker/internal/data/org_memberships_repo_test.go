package data

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAccountMembershipsRepository_GetByAccountAndUser(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_account_memberships_lookup")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	accountID := uuid.New()
	userID := uuid.New()
	_, err = pool.Exec(
		context.Background(),
		`INSERT INTO account_memberships (account_id, user_id, role)
		 VALUES ($1, $2, $3)`,
		accountID,
		userID,
		"org_admin",
	)
	if err != nil {
		t.Fatalf("insert membership: %v", err)
	}

	repo := AccountMembershipsRepository{}
	record, err := repo.GetByAccountAndUser(context.Background(), pool, accountID, userID)
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

func TestAccountMembershipsRepository_GetByAccountAndUser_NotFound(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_account_memberships_not_found")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	repo := AccountMembershipsRepository{}
	record, err := repo.GetByAccountAndUser(context.Background(), pool, uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("get membership: %v", err)
	}
	if record != nil {
		t.Fatalf("expected nil record, got %#v", record)
	}
}
