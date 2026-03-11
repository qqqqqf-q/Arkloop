package data

import (
	"context"
	"testing"

	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/testutil"

	"github.com/google/uuid"
)

func setupThreadsTestRepos(t *testing.T) (*ThreadRepository, *MessageRepository, *OrgRepository, *UserRepository, *ProjectRepository, context.Context) {
	t.Helper()

	db := testutil.SetupPostgresDatabase(t, "api_go_threads_repo")
	ctx := context.Background()

	if _, err := migrate.Up(ctx, db.DSN); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	pool, err := NewPool(ctx, db.DSN, PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	threadRepo, err := NewThreadRepository(pool)
	if err != nil {
		t.Fatalf("new thread repo: %v", err)
	}
	messageRepo, err := NewMessageRepository(pool)
	if err != nil {
		t.Fatalf("new message repo: %v", err)
	}
	orgRepo, err := NewOrgRepository(pool)
	if err != nil {
		t.Fatalf("new org repo: %v", err)
	}
	userRepo, err := NewUserRepository(pool)
	if err != nil {
		t.Fatalf("new user repo: %v", err)
	}
	projectRepo, err := NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}

	return threadRepo, messageRepo, orgRepo, userRepo, projectRepo, ctx
}

func TestThreadRepositoryModeLifecycle(t *testing.T) {
	threadRepo, messageRepo, orgRepo, userRepo, projectRepo, ctx := setupThreadsTestRepos(t)

	org, err := orgRepo.Create(ctx, "threads-mode", "Threads Mode Org", "personal")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	user, err := userRepo.Create(ctx, "threads-mode-user", "threads-mode@test.com", "en")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	project, err := projectRepo.CreateDefaultForOwner(ctx, org.ID, user.ID)
	if err != nil {
		t.Fatalf("create default project: %v", err)
	}

	chatTitle := "chat-title"
	chatThread, err := threadRepo.Create(ctx, org.ID, &user.ID, project.ID, ThreadModeChat, &chatTitle, false)
	if err != nil {
		t.Fatalf("create chat thread: %v", err)
	}
	if chatThread.Mode != ThreadModeChat {
		t.Fatalf("expected chat mode, got %#v", chatThread)
	}

	clawTitle := "claw-title"
	clawThread, err := threadRepo.Create(ctx, org.ID, &user.ID, project.ID, ThreadModeClaw, &clawTitle, false)
	if err != nil {
		t.Fatalf("create claw thread: %v", err)
	}
	if clawThread.Mode != ThreadModeClaw {
		t.Fatalf("expected claw mode, got %#v", clawThread)
	}

	listedAll, err := threadRepo.ListByOwner(ctx, org.ID, user.ID, nil, 10, nil, nil)
	if err != nil {
		t.Fatalf("list all by owner: %v", err)
	}
	if len(listedAll) != 2 {
		t.Fatalf("expected 2 threads without mode filter, got %d", len(listedAll))
	}

	chatMode := ThreadModeChat
	listedChat, err := threadRepo.ListByOwner(ctx, org.ID, user.ID, &chatMode, 10, nil, nil)
	if err != nil {
		t.Fatalf("list chat by owner: %v", err)
	}
	if len(listedChat) != 1 || listedChat[0].ID != chatThread.ID || listedChat[0].Mode != ThreadModeChat {
		t.Fatalf("unexpected chat list: %#v", listedChat)
	}

	clawMode := ThreadModeClaw
	listedClaw, err := threadRepo.ListByOwner(ctx, org.ID, user.ID, &clawMode, 10, nil, nil)
	if err != nil {
		t.Fatalf("list claw by owner: %v", err)
	}
	if len(listedClaw) != 1 || listedClaw[0].ID != clawThread.ID || listedClaw[0].Mode != ThreadModeClaw {
		t.Fatalf("unexpected claw list: %#v", listedClaw)
	}

	for _, item := range []struct {
		threadID uuid.UUID
		content  string
	}{
		{threadID: chatThread.ID, content: "shared-mode-search"},
		{threadID: clawThread.ID, content: "shared-mode-search"},
	} {
		if _, err := messageRepo.Create(ctx, org.ID, item.threadID, "user", item.content, &user.ID); err != nil {
			t.Fatalf("create message for %s: %v", item.threadID, err)
		}
	}

	searchAll, err := threadRepo.SearchByQuery(ctx, org.ID, user.ID, nil, "shared-mode-search", 10)
	if err != nil {
		t.Fatalf("search all: %v", err)
	}
	if len(searchAll) != 2 {
		t.Fatalf("expected 2 search results without mode filter, got %#v", searchAll)
	}

	searchChat, err := threadRepo.SearchByQuery(ctx, org.ID, user.ID, &chatMode, "shared-mode-search", 10)
	if err != nil {
		t.Fatalf("search chat: %v", err)
	}
	if len(searchChat) != 1 || searchChat[0].ID != chatThread.ID || searchChat[0].Mode != ThreadModeChat {
		t.Fatalf("unexpected chat search: %#v", searchChat)
	}

	searchClaw, err := threadRepo.SearchByQuery(ctx, org.ID, user.ID, &clawMode, "shared-mode-search", 10)
	if err != nil {
		t.Fatalf("search claw: %v", err)
	}
	if len(searchClaw) != 1 || searchClaw[0].ID != clawThread.ID || searchClaw[0].Mode != ThreadModeClaw {
		t.Fatalf("unexpected claw search: %#v", searchClaw)
	}

	forkSource, err := messageRepo.Create(ctx, org.ID, clawThread.ID, "user", "fork source", &user.ID)
	if err != nil {
		t.Fatalf("create fork source message: %v", err)
	}
	forked, err := threadRepo.Fork(ctx, org.ID, &user.ID, clawThread.ID, forkSource.ID, false)
	if err != nil {
		t.Fatalf("fork thread: %v", err)
	}
	if forked.Mode != ThreadModeClaw {
		t.Fatalf("expected forked mode claw, got %#v", forked)
	}
}
