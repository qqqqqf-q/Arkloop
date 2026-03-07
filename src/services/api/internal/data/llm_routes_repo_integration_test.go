package data

import (
	"context"
	"errors"
	"testing"

	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/testutil"

	"github.com/google/uuid"
)

func setupLlmRoutesTestRepos(t *testing.T) (*LlmRoutesRepository, *LlmCredentialsRepository, *OrgRepository, context.Context) {
	t.Helper()

	db := testutil.SetupPostgresDatabase(t, "api_go_llm_routes")
	ctx := context.Background()

	if _, err := migrate.Up(ctx, db.DSN); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	pool, err := NewPool(ctx, db.DSN, PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	routesRepo, err := NewLlmRoutesRepository(pool)
	if err != nil {
		t.Fatalf("new routes repo: %v", err)
	}
	credentialsRepo, err := NewLlmCredentialsRepository(pool)
	if err != nil {
		t.Fatalf("new credentials repo: %v", err)
	}
	orgRepo, err := NewOrgRepository(pool)
	if err != nil {
		t.Fatalf("new org repo: %v", err)
	}
	return routesRepo, credentialsRepo, orgRepo, ctx
}

func createLlmRouteTestCredential(t *testing.T, ctx context.Context, orgRepo *OrgRepository, credentialsRepo *LlmCredentialsRepository, name string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	org, err := orgRepo.Create(ctx, name+"-org", name+" Org", "personal")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	credID := uuid.New()
	cred, err := credentialsRepo.Create(ctx, credID, org.ID, "openai", name, nil, nil, nil, nil, map[string]any{})
	if err != nil {
		t.Fatalf("create credential: %v", err)
	}
	return org.ID, cred.ID
}

func TestLlmRoutesCreateStoresTags(t *testing.T) {
	routesRepo, credentialsRepo, orgRepo, ctx := setupLlmRoutesTestRepos(t)
	orgID, credentialID := createLlmRouteTestCredential(t, ctx, orgRepo, credentialsRepo, "tags")

	route, err := routesRepo.Create(ctx, CreateLlmRouteParams{
		OrgID:        orgID,
		CredentialID: credentialID,
		Model:        "gpt-4o",
		IsDefault:    true,
		Tags:         []string{"chat", "chat", " openai "},
	})
	if err != nil {
		t.Fatalf("create route: %v", err)
	}
	if len(route.Tags) != 2 || route.Tags[0] != "chat" || route.Tags[1] != "openai" {
		t.Fatalf("unexpected tags: %#v", route.Tags)
	}

	stored, err := routesRepo.GetByID(ctx, orgID, route.ID)
	if err != nil {
		t.Fatalf("get route: %v", err)
	}
	if stored == nil {
		t.Fatal("expected stored route")
	}
	if len(stored.Tags) != 2 || stored.Tags[0] != "chat" || stored.Tags[1] != "openai" {
		t.Fatalf("unexpected stored tags: %#v", stored.Tags)
	}
}

func TestLlmRoutesSetDefaultByCredential(t *testing.T) {
	routesRepo, credentialsRepo, orgRepo, ctx := setupLlmRoutesTestRepos(t)
	orgID, credentialID := createLlmRouteTestCredential(t, ctx, orgRepo, credentialsRepo, "set-default")

	first, err := routesRepo.Create(ctx, CreateLlmRouteParams{OrgID: orgID, CredentialID: credentialID, Model: "gpt-4o", Priority: 1, IsDefault: true})
	if err != nil {
		t.Fatalf("create first route: %v", err)
	}
	second, err := routesRepo.Create(ctx, CreateLlmRouteParams{OrgID: orgID, CredentialID: credentialID, Model: "gpt-4.1", Priority: 2})
	if err != nil {
		t.Fatalf("create second route: %v", err)
	}

	updated, err := routesRepo.SetDefaultByCredential(ctx, orgID, credentialID, second.ID)
	if err != nil {
		t.Fatalf("set default: %v", err)
	}
	if updated == nil || !updated.IsDefault || updated.ID != second.ID {
		t.Fatalf("unexpected updated route: %#v", updated)
	}

	storedFirst, err := routesRepo.GetByID(ctx, orgID, first.ID)
	if err != nil {
		t.Fatalf("get first: %v", err)
	}
	storedSecond, err := routesRepo.GetByID(ctx, orgID, second.ID)
	if err != nil {
		t.Fatalf("get second: %v", err)
	}
	if storedFirst == nil || storedSecond == nil {
		t.Fatal("expected both routes")
	}
	if storedFirst.IsDefault {
		t.Fatal("expected first route default cleared")
	}
	if !storedSecond.IsDefault {
		t.Fatal("expected second route to be default")
	}
}

func TestLlmRoutesPromoteHighestPriorityToDefault(t *testing.T) {
	routesRepo, credentialsRepo, orgRepo, ctx := setupLlmRoutesTestRepos(t)
	orgID, credentialID := createLlmRouteTestCredential(t, ctx, orgRepo, credentialsRepo, "promote-default")

	first, err := routesRepo.Create(ctx, CreateLlmRouteParams{OrgID: orgID, CredentialID: credentialID, Model: "gpt-4o", Priority: 1, IsDefault: true})
	if err != nil {
		t.Fatalf("create first route: %v", err)
	}
	second, err := routesRepo.Create(ctx, CreateLlmRouteParams{OrgID: orgID, CredentialID: credentialID, Model: "gpt-4.1", Priority: 9})
	if err != nil {
		t.Fatalf("create second route: %v", err)
	}
	if err := routesRepo.DeleteByID(ctx, orgID, first.ID); err != nil {
		t.Fatalf("delete first route: %v", err)
	}

	promoted, err := routesRepo.PromoteHighestPriorityToDefault(ctx, orgID, credentialID)
	if err != nil {
		t.Fatalf("promote default: %v", err)
	}
	if promoted == nil || promoted.ID != second.ID || !promoted.IsDefault {
		t.Fatalf("unexpected promoted route: %#v", promoted)
	}
}

func TestLlmRoutesCreateDuplicateModelConflict(t *testing.T) {
	routesRepo, credentialsRepo, orgRepo, ctx := setupLlmRoutesTestRepos(t)
	orgID, credentialID := createLlmRouteTestCredential(t, ctx, orgRepo, credentialsRepo, "duplicate-model")

	if _, err := routesRepo.Create(ctx, CreateLlmRouteParams{OrgID: orgID, CredentialID: credentialID, Model: "gpt-4o", IsDefault: true}); err != nil {
		t.Fatalf("create first route: %v", err)
	}
	_, err := routesRepo.Create(ctx, CreateLlmRouteParams{OrgID: orgID, CredentialID: credentialID, Model: "GPT-4O"})
	if err == nil {
		t.Fatal("expected conflict error")
	}
	var conflictErr LlmRouteModelConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("expected LlmRouteModelConflictError, got %T %v", err, err)
	}
}
