//go:build !desktop

package data

import (
	"context"
	"testing"

	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/testutil"

	"github.com/google/uuid"
)

func TestPersonasRepositoryScopesRowsToOrg(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "api_go_personas_repo")
	ctx := context.Background()

	if _, err := migrate.Up(ctx, db.DSN); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	appDB, _, err := NewPool(ctx, db.DSN, PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(func() { appDB.Close() })

	repo, err := NewPersonasRepository(appDB)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	orgRepo, err := NewOrgRepository(appDB)
	if err != nil {
		t.Fatalf("new org repo: %v", err)
	}

	org, err := orgRepo.Create(ctx, "persona-scope-org", "Persona Scope Org", "personal")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	orgID := org.ID
	custom, err := repo.Create(ctx, orgID, "custom-only", "1", "Custom Only", nil, "prompt", nil, nil, nil, nil, nil, "auto", "none", "agent.simple", nil)
	if err != nil {
		t.Fatalf("create custom persona: %v", err)
	}
	ghostID := insertGlobalPersonaRow(t, ctx, appDB, "ghost", "Ghost Persona")

	list, err := repo.ListByOrg(ctx, orgID)
	if err != nil {
		t.Fatalf("ListByOrg failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 persona, got %d", len(list))
	}
	if list[0].ID != custom.ID {
		t.Fatalf("expected custom persona in list, got %s", list[0].ID)
	}

	gotCustom, err := repo.GetByID(ctx, orgID, custom.ID)
	if err != nil {
		t.Fatalf("GetByID custom failed: %v", err)
	}
	if gotCustom == nil || gotCustom.PersonaKey != "custom-only" {
		t.Fatalf("unexpected custom persona: %#v", gotCustom)
	}

	gotGhost, err := repo.GetByID(ctx, orgID, ghostID)
	if err != nil {
		t.Fatalf("GetByID ghost failed: %v", err)
	}
	if gotGhost != nil {
		t.Fatalf("expected ghost persona hidden, got %#v", gotGhost)
	}

	newName := "Renamed Ghost"
	patchedGhost, err := repo.Patch(ctx, orgID, ghostID, PersonaPatch{DisplayName: &newName})
	if err != nil {
		t.Fatalf("Patch ghost failed: %v", err)
	}
	if patchedGhost != nil {
		t.Fatalf("expected ghost patch ignored, got %#v", patchedGhost)
	}

	deletedGhost, err := repo.Delete(ctx, orgID, ghostID)
	if err != nil {
		t.Fatalf("Delete ghost failed: %v", err)
	}
	if deletedGhost {
		t.Fatal("expected ghost delete to affect no rows")
	}

	deletedCustom, err := repo.Delete(ctx, orgID, custom.ID)
	if err != nil {
		t.Fatalf("Delete custom failed: %v", err)
	}
	if !deletedCustom {
		t.Fatal("expected custom delete to succeed")
	}
}

func insertGlobalPersonaRow(t *testing.T, ctx context.Context, pool Querier, personaKey string, displayName string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := pool.QueryRow(
		ctx,
		`INSERT INTO personas
			(org_id, persona_key, version, display_name, prompt_md, tool_allowlist, budgets_json, executor_type, executor_config_json)
		 VALUES (NULL, $1, '1', $2, 'prompt', '{}', '{}'::jsonb, 'agent.simple', '{}'::jsonb)
		 RETURNING id`,
		personaKey,
		displayName,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert global persona failed: %v", err)
	}
	return id
}
