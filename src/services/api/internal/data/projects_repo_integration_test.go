package data

import (
	"context"
	"sync"
	"testing"

	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/testutil"
)

func TestProjectRepositoryGetOrCreateDefaultByOwner(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "api_go_projects_repo")
	ctx := context.Background()

	if _, err := migrate.Up(ctx, db.DSN); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	pool, err := NewPool(ctx, db.DSN, PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	orgRepo, err := NewAccountRepository(pool)
	if err != nil {
		t.Fatalf("new org repo: %v", err)
	}
	userRepo, err := NewUserRepository(pool)
	if err != nil {
		t.Fatalf("new user repo: %v", err)
	}
	repo, err := NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}

	org, err := orgRepo.Create(ctx, "project-owner-org", "Project Owner Org", "personal")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	user, err := userRepo.Create(ctx, "project-owner", "project-owner@test.com", "en")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	first, err := repo.GetOrCreateDefaultByOwner(ctx, org.ID, user.ID)
	if err != nil {
		t.Fatalf("first get or create default project: %v", err)
	}
	if !first.IsDefault {
		t.Fatalf("expected default project, got %#v", first)
	}
	if first.OwnerUserID == nil || *first.OwnerUserID != user.ID {
		t.Fatalf("unexpected owner_user_id: %#v", first.OwnerUserID)
	}
	if first.Visibility != "private" {
		t.Fatalf("unexpected visibility: %q", first.Visibility)
	}

	second, err := repo.GetOrCreateDefaultByOwner(ctx, org.ID, user.ID)
	if err != nil {
		t.Fatalf("second get or create default project: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected same default project, got %s vs %s", first.ID, second.ID)
	}

	ids := make(chan string, 2)
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			project, err := repo.GetOrCreateDefaultByOwner(ctx, org.ID, user.ID)
			if err != nil {
				errCh <- err
				return
			}
			ids <- project.ID.String()
		}()
	}
	wg.Wait()
	close(ids)
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent get or create default project: %v", err)
		}
	}
	for id := range ids {
		if id != first.ID.String() {
			t.Fatalf("expected concurrent default project %s, got %s", first.ID, id)
		}
	}
}
