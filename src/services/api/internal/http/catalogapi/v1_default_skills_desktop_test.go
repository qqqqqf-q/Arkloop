//go:build desktop

package catalogapi

import (
	"context"
	"path/filepath"
	"testing"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"
	sharedenvironmentref "arkloop/services/shared/environmentref"
	"github.com/jackc/pgx/v5"
)

func TestReplaceDefaultSkillsAcrossBoundWorkspacesDesktop(t *testing.T) {
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

	enableRepo, err := data.NewWorkspaceSkillEnablementsRepository(pool)
	if err != nil {
		t.Fatalf("new workspace skill enable repo: %v", err)
	}

	profileRef := sharedenvironmentref.BuildProfileRef(auth.DesktopAccountID, &auth.DesktopUserID)
	defaultWorkspaceRef := "wsref_default_skills"
	projectWorkspaceRef := "wsref_project_binding"

	if _, err := pool.Exec(ctx, `
		INSERT INTO profile_registries (profile_ref, account_id, owner_user_id, default_workspace_ref, metadata_json)
		VALUES ($1, $2, $3, $4, '{}')`,
		profileRef, auth.DesktopAccountID, auth.DesktopUserID, defaultWorkspaceRef,
	); err != nil {
		t.Fatalf("seed profile registry: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspace_registries (workspace_ref, account_id, owner_user_id, metadata_json)
		VALUES ($1, $2, $3, '{}'), ($4, $2, $3, '{}')`,
		defaultWorkspaceRef, auth.DesktopAccountID, auth.DesktopUserID, projectWorkspaceRef,
	); err != nil {
		t.Fatalf("seed workspace registries: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO default_workspace_bindings (profile_ref, owner_user_id, account_id, binding_scope, binding_target_id, workspace_ref)
		VALUES ($1, $2, $3, 'project', '11111111-1111-4111-8111-111111111111', $4)`,
		profileRef, auth.DesktopUserID, auth.DesktopAccountID, projectWorkspaceRef,
	); err != nil {
		t.Fatalf("seed default workspace binding: %v", err)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	targets, err := replaceDefaultSkillsAcrossBoundWorkspaces(
		ctx,
		tx,
		enableRepo,
		auth.DesktopAccountID,
		auth.DesktopUserID,
		profileRef,
		defaultWorkspaceRef,
		[]data.WorkspaceSkillEnablement{{SkillKey: "research", Version: "1.1.0"}},
	)
	if err != nil {
		t.Fatalf("replace default skills across bound workspaces: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	if len(targets) != 2 {
		t.Fatalf("expected 2 target workspaces, got %#v", targets)
	}

	for _, workspaceRef := range []string{defaultWorkspaceRef, projectWorkspaceRef} {
		var count int
		if err := pool.QueryRow(
			ctx,
			`SELECT count(*) FROM workspace_skill_enablements WHERE account_id = $1 AND workspace_ref = $2 AND skill_key = 'research' AND version = '1.1.0'`,
			auth.DesktopAccountID,
			workspaceRef,
		).Scan(&count); err != nil {
			t.Fatalf("count workspace skills for %s: %v", workspaceRef, err)
		}
		if count != 1 {
			t.Fatalf("expected research enabled in %s, got %d rows", workspaceRef, count)
		}
	}
}
