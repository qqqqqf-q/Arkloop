//go:build desktop

package data_test

import (
	"context"
	"testing"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"

	"github.com/google/uuid"
)

func setupDesktopLlmRoutesTestRepos(t *testing.T) (*data.LlmRoutesRepository, *data.LlmCredentialsRepository, context.Context) {
	t.Helper()
	ctx := context.Background()
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, t.TempDir()+"/data.db")
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlitePool.Close() })
	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}
	routesRepo, err := data.NewLlmRoutesRepository(pool)
	if err != nil {
		t.Fatalf("new routes repo: %v", err)
	}
	credentialsRepo, err := data.NewLlmCredentialsRepository(pool)
	if err != nil {
		t.Fatalf("new credentials repo: %v", err)
	}
	return routesRepo, credentialsRepo, ctx
}

func createDesktopLlmRouteTestCredential(t *testing.T, ctx context.Context, credentialsRepo *data.LlmCredentialsRepository, name string) uuid.UUID {
	t.Helper()
	ownerUserID := auth.DesktopUserID
	credential, err := credentialsRepo.Create(ctx, uuid.New(), "user", &ownerUserID, "openai", name, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("create credential: %v", err)
	}
	return credential.ID
}

func TestLlmRoutesDesktopCreateCaseVariantModel(t *testing.T) {
	routesRepo, credentialsRepo, ctx := setupDesktopLlmRoutesTestRepos(t)
	credentialID := createDesktopLlmRouteTestCredential(t, ctx, credentialsRepo, "case-variant-model")

	if _, err := routesRepo.Create(ctx, data.CreateLlmRouteParams{AccountID: auth.DesktopAccountID, Scope: data.LlmRouteScopeUser, CredentialID: credentialID, Model: "MiMo-V2.5-Pro"}); err != nil {
		t.Fatalf("create first route: %v", err)
	}
	if _, err := routesRepo.Create(ctx, data.CreateLlmRouteParams{AccountID: auth.DesktopAccountID, Scope: data.LlmRouteScopeUser, CredentialID: credentialID, Model: "mimo-v2.5-pro"}); err != nil {
		t.Fatalf("create case variant route: %v", err)
	}
}

func TestLlmRoutesDesktopDeleteOnlyDefaultModel(t *testing.T) {
	routesRepo, credentialsRepo, ctx := setupDesktopLlmRoutesTestRepos(t)
	credentialID := createDesktopLlmRouteTestCredential(t, ctx, credentialsRepo, "delete-default-model")

	route, err := routesRepo.Create(ctx, data.CreateLlmRouteParams{AccountID: auth.DesktopAccountID, Scope: data.LlmRouteScopeUser, CredentialID: credentialID, Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("create route: %v", err)
	}
	if err := routesRepo.DeleteByID(ctx, auth.DesktopAccountID, route.ID, data.LlmRouteScopeUser); err != nil {
		t.Fatalf("delete route: %v", err)
	}
	promoted, err := routesRepo.PromoteHighestPriorityToDefault(ctx, auth.DesktopAccountID, credentialID, data.LlmRouteScopeUser)
	if err != nil {
		t.Fatalf("promote after deleting only route: %v", err)
	}
	if promoted != nil {
		t.Fatalf("expected no promoted route, got %#v", promoted)
	}
}
