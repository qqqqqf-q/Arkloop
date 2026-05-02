//go:build !desktop

package data

import (
	"context"
	"testing"

	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/testutil"

	"github.com/google/uuid"
)

func setupThreadsTestRepos(t *testing.T) (*ThreadRepository, *MessageRepository, *AccountRepository, *UserRepository, *ProjectRepository, context.Context) {
	t.Helper()

	db := testutil.SetupPostgresDatabase(t, "api_go_threads_repo")
	ctx := context.Background()

	if _, err := migrate.Up(ctx, db.DSN); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	appDB, err := NewPool(ctx, db.DSN, PoolLimits{MaxConns: 32, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(func() { appDB.Close() })

	threadRepo, err := NewThreadRepository(appDB)
	if err != nil {
		t.Fatalf("new thread repo: %v", err)
	}
	messageRepo, err := NewMessageRepository(appDB)
	if err != nil {
		t.Fatalf("new message repo: %v", err)
	}
	accountRepo, err := NewAccountRepository(appDB)
	if err != nil {
		t.Fatalf("new account repo: %v", err)
	}
	userRepo, err := NewUserRepository(appDB)
	if err != nil {
		t.Fatalf("new user repo: %v", err)
	}
	projectRepo, err := NewProjectRepository(appDB)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}

	return threadRepo, messageRepo, accountRepo, userRepo, projectRepo, ctx
}

func TestThreadRepositoryListSearchFork(t *testing.T) {
	threadRepo, messageRepo, accountRepo, userRepo, projectRepo, ctx := setupThreadsTestRepos(t)

	account, err := accountRepo.Create(ctx, "threads-list", "Threads List Account", "personal")
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	user, err := userRepo.Create(ctx, "threads-list-user", "threads-list@test.com", "en")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	project, err := projectRepo.CreateDefaultForOwner(ctx, account.ID, user.ID)
	if err != nil {
		t.Fatalf("create default project: %v", err)
	}

	titleA := "thread-alpha"
	threadA, err := threadRepo.Create(ctx, account.ID, &user.ID, project.ID, &titleA, false)
	if err != nil {
		t.Fatalf("create thread a: %v", err)
	}

	titleB := "thread-beta"
	threadB, err := threadRepo.Create(ctx, account.ID, &user.ID, project.ID, &titleB, false)
	if err != nil {
		t.Fatalf("create thread b: %v", err)
	}

	titleWork := "thread-work"
	threadWork, err := threadRepo.CreateWithMode(ctx, account.ID, &user.ID, project.ID, &titleWork, false, ThreadModeWork)
	if err != nil {
		t.Fatalf("create work thread: %v", err)
	}
	if threadWork.Mode != ThreadModeWork {
		t.Fatalf("work thread mode = %q", threadWork.Mode)
	}

	listed, err := threadRepo.ListByOwner(ctx, account.ID, user.ID, 10, nil, nil)
	if err != nil {
		t.Fatalf("list by owner: %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("expected 3 threads, got %d", len(listed))
	}

	listedChat, err := threadRepo.ListByOwnerWithMode(ctx, account.ID, user.ID, 10, nil, nil, ThreadModeChat)
	if err != nil {
		t.Fatalf("list chat by owner: %v", err)
	}
	if len(listedChat) != 2 {
		t.Fatalf("expected 2 chat threads, got %d", len(listedChat))
	}
	listedWork, err := threadRepo.ListByOwnerWithMode(ctx, account.ID, user.ID, 10, nil, nil, ThreadModeWork)
	if err != nil {
		t.Fatalf("list work by owner: %v", err)
	}
	if len(listedWork) != 1 || listedWork[0].ID != threadWork.ID {
		t.Fatalf("unexpected work threads: %#v", listedWork)
	}

	needle := "shared-search-token-unique"
	for _, id := range []uuid.UUID{threadA.ID, threadB.ID} {
		if _, err := messageRepo.Create(ctx, account.ID, id, "user", needle, &user.ID); err != nil {
			t.Fatalf("create message: %v", err)
		}
	}

	searchHits, err := threadRepo.SearchByQuery(ctx, account.ID, user.ID, needle, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(searchHits) != 2 {
		t.Fatalf("expected 2 search hits, got %#v", searchHits)
	}
	workSearchHits, err := threadRepo.SearchByQueryWithMode(ctx, account.ID, user.ID, needle, 10, ThreadModeWork)
	if err != nil {
		t.Fatalf("search work: %v", err)
	}
	if len(workSearchHits) != 0 {
		t.Fatalf("expected no work search hits, got %#v", workSearchHits)
	}

	updatedThreadB, err := threadRepo.UpdateFields(ctx, threadB.ID, ThreadUpdateFields{
		SetMode: true,
		Mode:    ThreadModeWork,
	})
	if err != nil {
		t.Fatalf("update thread mode: %v", err)
	}
	if updatedThreadB == nil || updatedThreadB.Mode != ThreadModeWork {
		t.Fatalf("updated thread mode = %#v", updatedThreadB)
	}
	if !updatedThreadB.UpdatedAt.Equal(threadB.UpdatedAt) {
		t.Fatalf("mode update changed updated_at: before=%s after=%s", threadB.UpdatedAt, updatedThreadB.UpdatedAt)
	}

	workFolder := "/workspace/arkloop"
	workBucket := ThreadGtdBucketTodo
	pinnedAt := threadWork.CreatedAt
	threadWorkWithSidebar, err := threadRepo.UpdateFields(ctx, threadWork.ID, ThreadUpdateFields{
		SetSidebarWorkFolder: true,
		SidebarWorkFolder:    &workFolder,
		SetSidebarPinnedAt:   true,
		SidebarPinnedAt:      &pinnedAt,
		SetSidebarGtdBucket:  true,
		SidebarGtdBucket:     &workBucket,
	})
	if err != nil {
		t.Fatalf("update sidebar state: %v", err)
	}
	if threadWorkWithSidebar == nil || threadWorkWithSidebar.SidebarWorkFolder == nil || *threadWorkWithSidebar.SidebarWorkFolder != workFolder {
		t.Fatalf("sidebar work folder = %#v", threadWorkWithSidebar)
	}
	if threadWorkWithSidebar.SidebarPinnedAt == nil || !threadWorkWithSidebar.SidebarPinnedAt.Equal(pinnedAt) {
		t.Fatalf("sidebar pinned_at = %#v", threadWorkWithSidebar)
	}
	if threadWorkWithSidebar.SidebarGtdBucket == nil || *threadWorkWithSidebar.SidebarGtdBucket != workBucket {
		t.Fatalf("sidebar gtd bucket = %#v", threadWorkWithSidebar)
	}
	if !threadWorkWithSidebar.UpdatedAt.Equal(threadWork.UpdatedAt) {
		t.Fatalf("sidebar update changed updated_at: before=%s after=%s", threadWork.UpdatedAt, threadWorkWithSidebar.UpdatedAt)
	}
	threadWork = *threadWorkWithSidebar
	threadWorkWithLearning, err := threadRepo.UpdateFields(ctx, threadWork.ID, ThreadUpdateFields{
		SetLearningModeEnabled: true,
		LearningModeEnabled:    true,
	})
	if err != nil {
		t.Fatalf("update learning mode: %v", err)
	}
	if threadWorkWithLearning == nil || !threadWorkWithLearning.LearningModeEnabled {
		t.Fatalf("learning mode = %#v", threadWorkWithLearning)
	}
	threadWork = *threadWorkWithLearning

	forkSource, err := messageRepo.Create(ctx, account.ID, threadWork.ID, "user", "fork source body", &user.ID)
	if err != nil {
		t.Fatalf("create fork source message: %v", err)
	}
	forked, err := threadRepo.Fork(ctx, account.ID, &user.ID, threadWork.ID, forkSource.ID, false)
	if err != nil {
		t.Fatalf("fork thread: %v", err)
	}
	if forked.ParentThreadID == nil || *forked.ParentThreadID != threadWork.ID {
		t.Fatalf("expected parent_thread_id=%s, got %#v", threadWork.ID, forked.ParentThreadID)
	}
	if forked.BranchedFromMessageID == nil || *forked.BranchedFromMessageID != forkSource.ID {
		t.Fatalf("expected branched_from_message_id=%s", forkSource.ID)
	}
	if forked.Mode != ThreadModeWork {
		t.Fatalf("forked mode = %q", forked.Mode)
	}
	if !forked.LearningModeEnabled {
		t.Fatal("expected forked learning mode to be enabled")
	}
	if forked.SidebarWorkFolder == nil || *forked.SidebarWorkFolder != workFolder {
		t.Fatalf("forked sidebar work folder = %#v", forked.SidebarWorkFolder)
	}
	if forked.SidebarGtdBucket == nil || *forked.SidebarGtdBucket != workBucket {
		t.Fatalf("forked sidebar gtd bucket = %#v", forked.SidebarGtdBucket)
	}
	if forked.SidebarPinnedAt != nil {
		t.Fatalf("forked sidebar pinned_at = %#v", forked.SidebarPinnedAt)
	}
}
