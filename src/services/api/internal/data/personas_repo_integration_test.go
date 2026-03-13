//go:build !desktop

package data

import (
	"context"
	"encoding/json"
	"testing"

	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/testutil"

	"github.com/google/uuid"
)

func TestPersonasRepositoryScopesRowsToProject(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "api_go_personas_repo")
	ctx := context.Background()

	if _, err := migrate.Up(ctx, db.DSN); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	pool, err := NewPool(ctx, db.DSN, PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	repo, err := NewPersonasRepository(pool)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	orgRepo, err := NewAccountRepository(pool)
	if err != nil {
		t.Fatalf("new org repo: %v", err)
	}
	projectRepo, err := NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}

	org, err := orgRepo.Create(ctx, "persona-scope-org", "Persona Scope Org", "personal")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	project, err := projectRepo.Create(ctx, org.ID, nil, "default", nil, "private")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	projectID := project.ID

	custom, err := repo.Create(ctx, projectID, "custom-only", "1", "Custom Only", nil, "prompt", nil, nil, nil, nil, nil, nil, "auto", "none", "agent.simple", nil)
	if err != nil {
		t.Fatalf("create custom persona: %v", err)
	}
	ghostID := insertGlobalPersonaRow(t, ctx, pool, "ghost", "Ghost Persona")

	list, err := repo.ListByProject(ctx, projectID)
	if err != nil {
		t.Fatalf("ListByProject failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 persona, got %d", len(list))
	}
	if list[0].ID != custom.ID {
		t.Fatalf("expected custom persona in list, got %s", list[0].ID)
	}

	gotCustom, err := repo.GetByID(ctx, projectID, custom.ID)
	if err != nil {
		t.Fatalf("GetByID custom failed: %v", err)
	}
	if gotCustom == nil || gotCustom.PersonaKey != "custom-only" {
		t.Fatalf("unexpected custom persona: %#v", gotCustom)
	}

	gotGhost, err := repo.GetByID(ctx, projectID, ghostID)
	if err != nil {
		t.Fatalf("GetByID ghost failed: %v", err)
	}
	if gotGhost != nil {
		t.Fatalf("expected ghost persona hidden, got %#v", gotGhost)
	}

	newName := "Renamed Ghost"
	patchedGhost, err := repo.Patch(ctx, projectID, ghostID, PersonaPatch{DisplayName: &newName})
	if err != nil {
		t.Fatalf("Patch ghost failed: %v", err)
	}
	if patchedGhost != nil {
		t.Fatalf("expected ghost patch ignored, got %#v", patchedGhost)
	}

	deletedGhost, err := repo.Delete(ctx, projectID, ghostID)
	if err != nil {
		t.Fatalf("Delete ghost failed: %v", err)
	}
	if deletedGhost {
		t.Fatal("expected ghost delete to affect no rows")
	}

	deletedCustom, err := repo.Delete(ctx, projectID, custom.ID)
	if err != nil {
		t.Fatalf("Delete custom failed: %v", err)
	}
	if !deletedCustom {
		t.Fatal("expected custom delete to succeed")
	}
}

func TestPersonasRepositoryRolesJSONRoundTrip(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "api_go_personas_repo_roles")
	ctx := context.Background()

	if _, err := migrate.Up(ctx, db.DSN); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	pool, err := NewPool(ctx, db.DSN, PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	repo, err := NewPersonasRepository(pool)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	orgRepo, err := NewOrgRepository(pool)
	if err != nil {
		t.Fatalf("new org repo: %v", err)
	}

	org, err := orgRepo.Create(ctx, "persona-role-org", "Persona Role Org", "personal")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	rolesJSON := json.RawMessage(`{"worker":{"prompt_md":"worker prompt","model":"worker^gpt-5-mini"}}`)
	created, err := repo.Create(ctx, org.ID, "roleful", "1", "Roleful", nil, "prompt", nil, nil, nil, rolesJSON, nil, nil, "auto", "none", "agent.simple", nil)
	if err != nil {
		t.Fatalf("create persona: %v", err)
	}
	assertJSONEqual(t, created.RolesJSON, rolesJSON)

	patched, err := repo.Patch(ctx, org.ID, created.ID, PersonaPatch{RolesJSON: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatalf("patch persona: %v", err)
	}
	assertJSONEqual(t, patched.RolesJSON, json.RawMessage(`{}`))
	got, err := repo.GetByID(ctx, org.ID, created.ID)
	if err != nil {
		t.Fatalf("get persona: %v", err)
	}
	if got == nil {
		t.Fatal("expected persona after patch")
	}
	assertJSONEqual(t, got.RolesJSON, json.RawMessage(`{}`))
}

func TestPersonasRepositoryUpsertPlatformMirrorStoresRolesJSON(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "api_go_personas_repo_roles_mirror")
	ctx := context.Background()

	if _, err := migrate.Up(ctx, db.DSN); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	pool, err := NewPool(ctx, db.DSN, PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	repo, err := NewPersonasRepository(pool)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}

	rolesJSON := json.RawMessage(`{"worker":{"prompt_md":"worker prompt"}}`)
	persona, err := repo.UpsertPlatformMirror(ctx, PlatformMirrorUpsertParams{
		PersonaKey:         "builtin-roleful",
		Version:            "1",
		DisplayName:        "Builtin Roleful",
		PromptMD:           "prompt",
		ToolAllowlist:      []string{},
		ToolDenylist:       []string{},
		BudgetsJSON:        json.RawMessage(`{}`),
		RolesJSON:          rolesJSON,
		ExecutorType:       "agent.simple",
		ExecutorConfigJSON: json.RawMessage(`{}`),
		IsActive:           true,
		MirroredFileDir:    "builtin-roleful",
	})
	if err != nil {
		t.Fatalf("upsert mirror: %v", err)
	}
	assertJSONEqual(t, persona.RolesJSON, rolesJSON)
}

func assertJSONEqual(t *testing.T, got json.RawMessage, want json.RawMessage) {
	t.Helper()
	var gotValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("unmarshal got json: %v", err)
	}
	var wantValue any
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("unmarshal want json: %v", err)
	}
	gotEncoded, _ := json.Marshal(gotValue)
	wantEncoded, _ := json.Marshal(wantValue)
	if string(gotEncoded) != string(wantEncoded) {
		t.Fatalf("json mismatch: got %s want %s", gotEncoded, wantEncoded)
	}
}

func insertGlobalPersonaRow(t *testing.T, ctx context.Context, pool Querier, personaKey string, displayName string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := pool.QueryRow(
		ctx,
		`INSERT INTO personas
			(account_id, persona_key, version, display_name, prompt_md, tool_allowlist, budgets_json, executor_type, executor_config_json)
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
