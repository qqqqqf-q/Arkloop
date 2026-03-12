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

func TestSkillsRepositoryResolveEnabledSkills(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_enabled_skills")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()
	dbPool := pgadapter.New(pool)
	repo := SkillsRepository{}
	orgID := uuid.New()
	userID := uuid.New()
	profileRef := "pref_test"
	workspaceRef := "wsref_test"
	if _, err := pool.Exec(context.Background(), `INSERT INTO workspace_registries (workspace_ref, org_id, owner_user_id, metadata_json) VALUES ($1, $2, $3, '{}'::jsonb)`, workspaceRef, orgID, userID); err != nil {
		t.Fatalf("seed workspace registry: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO skill_packages (org_id, skill_key, version, display_name, instruction_path, manifest_key, bundle_key, files_prefix) VALUES ($1, 'grep-helper', '1', 'Grep Helper', 'SKILL.md', 'm1', 'b1', 'f1'), ($1, 'sed-helper', '1', 'Sed Helper', 'SKILL.md', 'm2', 'b2', 'f2')`, orgID); err != nil {
		t.Fatalf("seed skill packages: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO profile_skill_installs (profile_ref, org_id, owner_user_id, skill_key, version) VALUES ($1, $2, $3, 'grep-helper', '1')`, profileRef, orgID, userID); err != nil {
		t.Fatalf("seed profile install: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO workspace_skill_enablements (workspace_ref, org_id, enabled_by_user_id, skill_key, version) VALUES ($1, $2, $3, 'grep-helper', '1'), ($1, $2, $3, 'sed-helper', '1')`, workspaceRef, orgID, userID); err != nil {
		t.Fatalf("seed workspace enablement: %v", err)
	}
	items, err := repo.ResolveEnabledSkills(context.Background(), dbPool, orgID, profileRef, workspaceRef)
	if err != nil {
		t.Fatalf("resolve enabled skills: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 resolved skill, got %d", len(items))
	}
	if items[0].SkillKey != "grep-helper" || items[0].Version != "1" {
		t.Fatalf("unexpected resolved skill: %#v", items[0])
	}
}
