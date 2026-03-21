//go:build desktop

package data_test

import (
	"context"
	"testing"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"
)

func TestPersonasRepositoryGetByIDForAccountWorksInDesktopMode(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, t.TempDir()+"/data.db")
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}

	projectRepo, err := data.NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}
	project, err := projectRepo.CreateDefaultForOwner(ctx, auth.DesktopAccountID, auth.DesktopUserID)
	if err != nil {
		t.Fatalf("create default project: %v", err)
	}

	personasRepo, err := data.NewPersonasRepository(pool)
	if err != nil {
		t.Fatalf("new personas repo: %v", err)
	}
	created, err := personasRepo.Create(
		ctx,
		project.ID,
		"desktop-persona",
		"1",
		"Desktop Persona",
		nil,
		"prompt",
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		"auto",
		true,
		"none",
		"agent.simple",
		nil,
	)
	if err != nil {
		t.Fatalf("create persona: %v", err)
	}

	got, err := personasRepo.GetByIDForAccount(ctx, auth.DesktopAccountID, created.ID)
	if err != nil {
		t.Fatalf("get persona by account: %v", err)
	}
	if got == nil {
		t.Fatal("expected persona, got nil")
	}
	if got.ID != created.ID {
		t.Fatalf("unexpected persona id: got %s want %s", got.ID, created.ID)
	}
}
