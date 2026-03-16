//go:build desktop

package personasync

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"arkloop/services/shared/database/sqliteadapter"
)

func TestSeedDesktopPersonas(t *testing.T) {
	personasRoot := findTestPersonasRoot(t)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	pool, err := sqliteadapter.AutoMigrate(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()

	// First seed should insert all personas.
	if err := SeedDesktopPersonas(ctx, pool, personasRoot); err != nil {
		t.Fatalf("SeedDesktopPersonas (first): %v", err)
	}

	row := pool.QueryRow(ctx, `SELECT COUNT(*) FROM personas WHERE is_active = 1`)
	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count == 0 {
		t.Fatal("expected at least one persona after seed")
	}
	t.Logf("seeded %d personas", count)

	// Second seed should be idempotent (update, not duplicate).
	if err := SeedDesktopPersonas(ctx, pool, personasRoot); err != nil {
		t.Fatalf("SeedDesktopPersonas (second): %v", err)
	}

	row = pool.QueryRow(ctx, `SELECT COUNT(*) FROM personas WHERE is_active = 1`)
	var count2 int
	if err := row.Scan(&count2); err != nil {
		t.Fatalf("count2: %v", err)
	}
	if count2 != count {
		t.Fatalf("idempotency broken: first=%d second=%d", count, count2)
	}

	// Verify key fields are populated.
	row = pool.QueryRow(ctx,
		`SELECT persona_key, display_name, prompt_md, executor_type, sync_mode
		 FROM personas WHERE is_active = 1 LIMIT 1`)
	var key, name, prompt, execType, syncMode string
	if err := row.Scan(&key, &name, &prompt, &execType, &syncMode); err != nil {
		t.Fatalf("scan persona: %v", err)
	}
	if key == "" || name == "" || prompt == "" {
		t.Fatalf("persona has empty required fields: key=%q name=%q prompt=%q", key, name, prompt)
	}
	if execType == "" {
		t.Fatal("executor_type should not be empty")
	}
	if syncMode != "platform_file_mirror" {
		t.Fatalf("sync_mode = %q, want platform_file_mirror", syncMode)
	}
}

func TestSeedDesktopPersonas_NilDB(t *testing.T) {
	if err := SeedDesktopPersonas(context.Background(), nil, "/tmp"); err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestSeedDesktopPersonas_EmptyRoot(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	pool, err := sqliteadapter.AutoMigrate(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	defer pool.Close()

	if err := SeedDesktopPersonas(context.Background(), pool, ""); err == nil {
		t.Fatal("expected error for empty root")
	}
}

// findTestPersonasRoot walks up from the test file to find src/personas.
func findTestPersonasRoot(t *testing.T) string {
	t.Helper()
	if envRoot := os.Getenv("ARKLOOP_PERSONAS_ROOT"); envRoot != "" {
		return envRoot
	}
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if filepath.Base(dir) == "src" {
			root := filepath.Join(dir, "personas")
			if _, err := os.Stat(root); err == nil {
				return root
			}
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
		dir = next
	}
	t.Skip("src/personas not found")
	return ""
}
