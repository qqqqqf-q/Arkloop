package data_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"
	sharedenvironmentref "arkloop/services/shared/environmentref"
)

func TestProfileRegistriesUpdateInstalledSkillRefsWorksInDesktopMode(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}

	repo, err := data.NewProfileRegistriesRepository(pool)
	if err != nil {
		t.Fatalf("new profile registries repo: %v", err)
	}

	profileRef := sharedenvironmentref.BuildProfileRef(auth.DesktopAccountID, &auth.DesktopUserID)
	if err := repo.Ensure(ctx, profileRef, auth.DesktopAccountID, auth.DesktopUserID); err != nil {
		t.Fatalf("ensure profile registry: %v", err)
	}
	if err := repo.UpdateInstalledSkillRefs(ctx, profileRef, []string{"translate-book@1.0.0"}); err != nil {
		t.Fatalf("update installed skill refs: %v", err)
	}

	var metadata string
	if err := pool.QueryRow(ctx, `SELECT metadata_json FROM profile_registries WHERE profile_ref = $1`, profileRef).Scan(&metadata); err != nil {
		t.Fatalf("load profile metadata: %v", err)
	}
	if metadata == "" {
		t.Fatal("expected metadata_json to be updated")
	}
	if want := "translate-book@1.0.0"; !strings.Contains(metadata, want) {
		t.Fatalf("metadata_json = %q, want substring %q", metadata, want)
	}
}
