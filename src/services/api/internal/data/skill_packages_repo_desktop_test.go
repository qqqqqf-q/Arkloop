package data_test

import (
	"context"
	"path/filepath"
	"testing"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"
)

func TestSkillPackagesFindActiveByRegistryDesktop(t *testing.T) {
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

	repo, err := data.NewSkillPackagesRepository(pool)
	if err != nil {
		t.Fatalf("new skill packages repo: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO skill_packages (
			account_id, skill_key, version, display_name, instruction_path, manifest_key, bundle_key, files_prefix,
			registry_provider, registry_slug, registry_version
		) VALUES (
			$1, 'rainman-translate-book', '0.2.0', 'Deprecated Skill', 'SKILL.md', 'm1', 'b1', 'f1',
			'clawhub', 'rainman-translate-book', '0.2.0'
		)`,
		auth.DesktopAccountID,
	); err != nil {
		t.Fatalf("seed skill package: %v", err)
	}

	item, err := repo.FindActiveByRegistry(ctx, auth.DesktopAccountID, "clawhub", "rainman-translate-book", "0.2.0")
	if err != nil {
		t.Fatalf("find by registry: %v", err)
	}
	if item == nil {
		t.Fatal("expected skill package")
	}
	if item.SkillKey != "rainman-translate-book" || item.Version != "0.2.0" {
		t.Fatalf("unexpected skill package: %#v", item)
	}
}
