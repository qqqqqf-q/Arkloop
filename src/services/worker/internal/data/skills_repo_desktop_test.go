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

func TestSkillsRepositoryResolveEnabledSkillsDesktop(t *testing.T) {
	db := openDesktopSkillsDB(t)
	repo := NewSkillsRepository(db)
	accountID := uuid.New()
	userID := uuid.New()
	profileRef := "pref_test"
	workspaceRef := "wsref_test"

	seedDesktopSkillsAccount(t, db, accountID, userID)
	if _, err := db.Exec(context.Background(), `INSERT INTO workspace_registries (workspace_ref, account_id, owner_user_id, metadata_json) VALUES ($1, $2, $3, '{}')`, workspaceRef, accountID, userID); err != nil {
		t.Fatalf("seed workspace registry: %v", err)
	}
	if _, err := db.Exec(context.Background(), `INSERT INTO skill_packages (account_id, skill_key, version, display_name, instruction_path, manifest_key, bundle_key, files_prefix) VALUES ($1, 'grep-helper', '1', 'Grep Helper', 'SKILL.md', 'm1', 'b1', 'f1'), ($1, 'sed-helper', '1', 'Sed Helper', 'SKILL.md', 'm2', 'b2', 'f2')`, accountID); err != nil {
		t.Fatalf("seed skill packages: %v", err)
	}
	if _, err := db.Exec(context.Background(), `INSERT INTO skill_packages (account_id, skill_key, version, display_name, instruction_path, manifest_key, bundle_key, files_prefix, sync_mode) VALUES (NULL, 'builtin-auto', '1', 'Builtin Auto', 'SKILL.md', 'pm1', 'pb1', 'pf1', 'platform_skill'), (NULL, 'builtin-manual', '1', 'Builtin Manual', 'SKILL.md', 'pm2', 'pb2', 'pf2', 'platform_skill'), (NULL, 'builtin-removed', '1', 'Builtin Removed', 'SKILL.md', 'pm3', 'pb3', 'pf3', 'platform_skill')`); err != nil {
		t.Fatalf("seed platform skill packages: %v", err)
	}
	if _, err := db.Exec(context.Background(), `INSERT INTO profile_skill_installs (profile_ref, account_id, owner_user_id, skill_key, version) VALUES ($1, $2, $3, 'grep-helper', '1')`, profileRef, accountID, userID); err != nil {
		t.Fatalf("seed profile install: %v", err)
	}
	if _, err := db.Exec(context.Background(), `INSERT INTO workspace_skill_enablements (workspace_ref, account_id, enabled_by_user_id, skill_key, version) VALUES ($1, $2, $3, 'grep-helper', '1'), ($1, $2, $3, 'sed-helper', '1')`, workspaceRef, accountID, userID); err != nil {
		t.Fatalf("seed workspace enablement: %v", err)
	}
	if _, err := db.Exec(context.Background(), `INSERT INTO profile_platform_skill_overrides (profile_ref, skill_key, version, status) VALUES ($1, 'builtin-manual', '1', 'manual'), ($1, 'builtin-removed', '1', 'removed')`, profileRef); err != nil {
		t.Fatalf("seed platform overrides: %v", err)
	}

	items, err := repo.ResolveEnabledSkills(context.Background(), accountID, profileRef, workspaceRef)
	if err != nil {
		t.Fatalf("resolve enabled skills: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 resolved skills, got %d", len(items))
	}
	if items[0].SkillKey != "builtin-auto" || !items[0].AutoInject {
		t.Fatalf("expected builtin auto skill, got %#v", items[0])
	}
	if items[1].SkillKey != "builtin-manual" || items[1].AutoInject {
		t.Fatalf("expected builtin manual skill, got %#v", items[1])
	}
	if items[2].SkillKey != "grep-helper" || items[2].Version != "1" || !items[2].AutoInject {
		t.Fatalf("unexpected workspace-resolved skill: %#v", items[2])
	}
}

func TestSkillsRepositoryResolveEnabledSkillsFallsBackToPlatformSkillsDesktop(t *testing.T) {
	db := openDesktopSkillsDB(t)
	repo := NewSkillsRepository(db)
	accountID := uuid.New()

	if _, err := db.Exec(context.Background(), `INSERT INTO skill_packages (account_id, skill_key, version, display_name, instruction_path, manifest_key, bundle_key, files_prefix, sync_mode) VALUES (NULL, 'builtin-auto', '1', 'Builtin Auto', 'SKILL.md', 'pm1', 'pb1', 'pf1', 'platform_skill')`); err != nil {
		t.Fatalf("seed platform skill package: %v", err)
	}

	items, err := repo.ResolveEnabledSkills(context.Background(), accountID, "", "")
	if err != nil {
		t.Fatalf("resolve platform skills: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 platform skill, got %d", len(items))
	}
	if items[0].SkillKey != "builtin-auto" || !items[0].AutoInject {
		t.Fatalf("unexpected platform skill: %#v", items[0])
	}
}

func openDesktopSkillsDB(t *testing.T) *sqlitepgx.Pool {
	t.Helper()
	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlitePool.Close(); err != nil {
			t.Fatalf("close sqlite pool: %v", err)
		}
	})
	return sqlitepgx.New(sqlitePool.Unwrap())
}

func seedDesktopSkillsAccount(t *testing.T, db DB, accountID, userID uuid.UUID) {
	t.Helper()
	if _, err := db.Exec(context.Background(), `INSERT INTO users (id, username, email, status) VALUES ($1, $2, $3, 'active')`, userID, "skills-user-"+userID.String(), "skills-"+userID.String()+"@test.local"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := db.Exec(context.Background(), `INSERT INTO accounts (id, slug, name, type, status, owner_user_id) VALUES ($1, $2, $3, 'personal', 'active', $4)`, accountID, "skills-account-"+accountID.String(), "Desktop Skills Account", userID); err != nil {
		t.Fatalf("seed account: %v", err)
	}
}
