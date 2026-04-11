//go:build desktop

package data

import (
	"context"
	"path/filepath"
	"testing"

	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"

	"github.com/google/uuid"
)

func TestDesktopExternalThreadLinksRepositoryUpsertAndGet(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	seedDesktopAccount(t, pool, accountID)
	seedDesktopProject(t, pool, accountID, projectID)
	seedDesktopThread(t, pool, accountID, projectID, threadID)

	repo := ExternalThreadLinksRepository{}
	if err := repo.Upsert(ctx, pool, accountID, threadID, "nowledge", "desktop-thread-1"); err != nil {
		t.Fatalf("upsert first: %v", err)
	}

	got, found, err := repo.Get(ctx, pool, accountID, threadID, "nowledge")
	if err != nil {
		t.Fatalf("get first: %v", err)
	}
	if !found || got != "desktop-thread-1" {
		t.Fatalf("unexpected first link: found=%v value=%q", found, got)
	}

	if err := repo.Upsert(ctx, pool, accountID, threadID, "nowledge", "desktop-thread-2"); err != nil {
		t.Fatalf("upsert second: %v", err)
	}

	got, found, err = repo.Get(ctx, pool, accountID, threadID, "nowledge")
	if err != nil {
		t.Fatalf("get second: %v", err)
	}
	if !found || got != "desktop-thread-2" {
		t.Fatalf("unexpected updated link: found=%v value=%q", found, got)
	}
}
