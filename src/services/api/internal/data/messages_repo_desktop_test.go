//go:build desktop

package data_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestListForkedThreadMessagesInDesktopMode(t *testing.T) {
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

	projectRepo, err := data.NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}
	threadRepo, err := data.NewThreadRepository(pool)
	if err != nil {
		t.Fatalf("new thread repo: %v", err)
	}
	messageRepo, err := data.NewMessageRepository(pool)
	if err != nil {
		t.Fatalf("new message repo: %v", err)
	}

	project, err := projectRepo.GetOrCreateDefaultByOwner(ctx, auth.DesktopAccountID, auth.DesktopUserID)
	if err != nil {
		t.Fatalf("get or create default project: %v", err)
	}

	userID := auth.DesktopUserID
	thread, err := threadRepo.Create(ctx, auth.DesktopAccountID, &userID, project.ID, nil, false)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	if _, err := messageRepo.Create(ctx, auth.DesktopAccountID, thread.ID, "user", "before", &userID); err != nil {
		t.Fatalf("create first message: %v", err)
	}
	cutoff, err := messageRepo.Create(ctx, auth.DesktopAccountID, thread.ID, "assistant", "cutoff", &userID)
	if err != nil {
		t.Fatalf("create cutoff message: %v", err)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	txThreadRepo, err := data.NewThreadRepository(tx)
	if err != nil {
		t.Fatalf("new tx thread repo: %v", err)
	}
	txMessageRepo, err := data.NewMessageRepository(tx)
	if err != nil {
		t.Fatalf("new tx message repo: %v", err)
	}

	forked, err := txThreadRepo.Fork(ctx, auth.DesktopAccountID, &userID, thread.ID, cutoff.ID, false)
	if err != nil {
		t.Fatalf("fork thread: %v", err)
	}
	if _, err := txMessageRepo.CopyUpTo(ctx, auth.DesktopAccountID, thread.ID, forked.ID, cutoff.ID); err != nil {
		t.Fatalf("copy up to cutoff: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit fork tx: %v", err)
	}

	messages, err := messageRepo.ListByThread(ctx, auth.DesktopAccountID, forked.ID, 100)
	if err != nil {
		t.Fatalf("list forked messages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 forked messages, got %d", len(messages))
	}
}

func TestCreateMessageDesktopWritesHighPrecisionCreatedAt(t *testing.T) {
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

	projectRepo, err := data.NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}
	threadRepo, err := data.NewThreadRepository(pool)
	if err != nil {
		t.Fatalf("new thread repo: %v", err)
	}
	messageRepo, err := data.NewMessageRepository(pool)
	if err != nil {
		t.Fatalf("new message repo: %v", err)
	}

	project, err := projectRepo.GetOrCreateDefaultByOwner(ctx, auth.DesktopAccountID, auth.DesktopUserID)
	if err != nil {
		t.Fatalf("get or create default project: %v", err)
	}

	userID := auth.DesktopUserID
	thread, err := threadRepo.Create(ctx, auth.DesktopAccountID, &userID, project.ID, nil, false)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	msg, err := messageRepo.Create(ctx, auth.DesktopAccountID, thread.ID, "user", "hello", &userID)
	if err != nil {
		t.Fatalf("create message: %v", err)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin read tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var createdAt string
	if err := tx.QueryRow(ctx, `SELECT created_at FROM messages WHERE id = $1`, msg.ID).Scan(&createdAt); err != nil {
		t.Fatalf("query created_at: %v", err)
	}
	if !strings.Contains(createdAt, ".") || !strings.Contains(createdAt, "+0000") {
		t.Fatalf("expected fixed-width high precision created_at, got %q", createdAt)
	}
}

func TestCopyUpToDesktopIncludesHiddenIntermediateHistory(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "copy-intermediate.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}

	projectRepo, err := data.NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}
	threadRepo, err := data.NewThreadRepository(pool)
	if err != nil {
		t.Fatalf("new thread repo: %v", err)
	}
	messageRepo, err := data.NewMessageRepository(pool)
	if err != nil {
		t.Fatalf("new message repo: %v", err)
	}

	project, err := projectRepo.GetOrCreateDefaultByOwner(ctx, auth.DesktopAccountID, auth.DesktopUserID)
	if err != nil {
		t.Fatalf("get or create default project: %v", err)
	}

	userID := auth.DesktopUserID
	thread, err := threadRepo.Create(ctx, auth.DesktopAccountID, &userID, project.ID, nil, false)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}

	first, err := messageRepo.Create(ctx, auth.DesktopAccountID, thread.ID, "user", "before", &userID)
	if err != nil {
		t.Fatalf("create first message: %v", err)
	}

	runID := uuid.NewString()
	assistantIntermediateID := uuid.New()
	toolIntermediateID := uuid.New()
	finalAssistantID := uuid.New()
	if err := insertDesktopMessageRow(ctx, pool, auth.DesktopAccountID, thread.ID, assistantIntermediateID, 2, "assistant", "tool call", `{"run_id":"`+runID+`","intermediate":"true"}`, true); err != nil {
		t.Fatalf("insert assistant intermediate: %v", err)
	}
	if err := insertDesktopMessageRow(ctx, pool, auth.DesktopAccountID, thread.ID, toolIntermediateID, 3, "tool", "tool result", `{"run_id":"`+runID+`","intermediate":"true"}`, true); err != nil {
		t.Fatalf("insert tool intermediate: %v", err)
	}
	if err := insertDesktopMessageRow(ctx, pool, auth.DesktopAccountID, thread.ID, finalAssistantID, 4, "assistant", "final", `{"run_id":"`+runID+`"}`, false); err != nil {
		t.Fatalf("insert final assistant: %v", err)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	txThreadRepo, err := data.NewThreadRepository(tx)
	if err != nil {
		t.Fatalf("new tx thread repo: %v", err)
	}
	txMessageRepo, err := data.NewMessageRepository(tx)
	if err != nil {
		t.Fatalf("new tx message repo: %v", err)
	}

	forked, err := txThreadRepo.Fork(ctx, auth.DesktopAccountID, &userID, thread.ID, finalAssistantID, false)
	if err != nil {
		t.Fatalf("fork thread: %v", err)
	}
	if _, err := txMessageRepo.CopyUpTo(ctx, auth.DesktopAccountID, thread.ID, forked.ID, finalAssistantID); err != nil {
		t.Fatalf("copy up to final assistant: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	rows, err := pool.Query(ctx, `SELECT role, hidden FROM messages WHERE thread_id = $1 ORDER BY thread_seq ASC`, forked.ID)
	if err != nil {
		t.Fatalf("query forked raw messages: %v", err)
	}
	defer rows.Close()

	type row struct {
		role   string
		hidden bool
	}
	var got []row
	for rows.Next() {
		var item row
		if err := rows.Scan(&item.role, &item.hidden); err != nil {
			t.Fatalf("scan forked raw message: %v", err)
		}
		got = append(got, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 forked messages including intermediate history, got %#v", got)
	}
	if got[1].role != "assistant" || !got[1].hidden || got[2].role != "tool" || !got[2].hidden || got[3].role != "assistant" || got[3].hidden {
		t.Fatalf("unexpected forked history: %#v", got)
	}
	if got[0].role != first.Role {
		t.Fatalf("unexpected first role: %#v", got)
	}
}

func TestHideMessagesAfterDesktopHidesIntermediateHistory(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "hide-after.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}

	projectRepo, err := data.NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}
	threadRepo, err := data.NewThreadRepository(pool)
	if err != nil {
		t.Fatalf("new thread repo: %v", err)
	}
	messageRepo, err := data.NewMessageRepository(pool)
	if err != nil {
		t.Fatalf("new message repo: %v", err)
	}

	project, err := projectRepo.GetOrCreateDefaultByOwner(ctx, auth.DesktopAccountID, auth.DesktopUserID)
	if err != nil {
		t.Fatalf("get or create default project: %v", err)
	}

	userID := auth.DesktopUserID
	thread, err := threadRepo.Create(ctx, auth.DesktopAccountID, &userID, project.ID, nil, false)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	cutoff, err := messageRepo.Create(ctx, auth.DesktopAccountID, thread.ID, "user", "cutoff", &userID)
	if err != nil {
		t.Fatalf("create cutoff: %v", err)
	}
	runID := uuid.NewString()
	assistantIntermediateID := uuid.New()
	finalAssistantID := uuid.New()
	if err := insertDesktopMessageRow(ctx, pool, auth.DesktopAccountID, thread.ID, assistantIntermediateID, 2, "assistant", "tool call", `{"run_id":"`+runID+`","intermediate":true}`, true); err != nil {
		t.Fatalf("insert assistant intermediate: %v", err)
	}
	if err := insertDesktopMessageRow(ctx, pool, auth.DesktopAccountID, thread.ID, finalAssistantID, 3, "assistant", "final", `{"run_id":"`+runID+`"}`, false); err != nil {
		t.Fatalf("insert final assistant: %v", err)
	}

	if err := messageRepo.HideMessagesAfter(ctx, auth.DesktopAccountID, thread.ID, cutoff.ID); err != nil {
		t.Fatalf("hide after: %v", err)
	}

	rows, err := pool.Query(ctx, `SELECT id, hidden, deleted_at FROM messages WHERE thread_id = $1 ORDER BY thread_seq ASC`, thread.ID)
	if err != nil {
		t.Fatalf("query hidden state: %v", err)
	}
	defer rows.Close()

	hiddenByID := map[uuid.UUID]bool{}
	deletedByID := map[uuid.UUID]string{}
	for rows.Next() {
		var id uuid.UUID
		var hidden bool
		var deletedAt *string
		if err := rows.Scan(&id, &hidden, &deletedAt); err != nil {
			t.Fatalf("scan hidden state: %v", err)
		}
		hiddenByID[id] = hidden
		if deletedAt != nil {
			deletedByID[id] = *deletedAt
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
	if hiddenByID[cutoff.ID] {
		t.Fatalf("cutoff message should stay visible")
	}
	if !hiddenByID[assistantIntermediateID] || !hiddenByID[finalAssistantID] {
		t.Fatalf("expected later intermediate and final messages hidden, got %#v", hiddenByID)
	}
	if deletedByID[assistantIntermediateID] == "" || deletedByID[finalAssistantID] == "" {
		t.Fatalf("expected later intermediate and final messages soft-deleted, got %#v", deletedByID)
	}
}

func TestHideLastAssistantMessageDesktopHidesSameRunIntermediateHistory(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "hide-last-assistant.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}

	projectRepo, err := data.NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}
	threadRepo, err := data.NewThreadRepository(pool)
	if err != nil {
		t.Fatalf("new thread repo: %v", err)
	}
	messageRepo, err := data.NewMessageRepository(pool)
	if err != nil {
		t.Fatalf("new message repo: %v", err)
	}

	project, err := projectRepo.GetOrCreateDefaultByOwner(ctx, auth.DesktopAccountID, auth.DesktopUserID)
	if err != nil {
		t.Fatalf("get or create default project: %v", err)
	}

	userID := auth.DesktopUserID
	thread, err := threadRepo.Create(ctx, auth.DesktopAccountID, &userID, project.ID, nil, false)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	if _, err := messageRepo.Create(ctx, auth.DesktopAccountID, thread.ID, "user", "before", &userID); err != nil {
		t.Fatalf("create user message: %v", err)
	}

	runID := uuid.NewString()
	assistantIntermediateID := uuid.New()
	toolIntermediateID := uuid.New()
	finalAssistantID := uuid.New()
	if err := insertDesktopMessageRow(ctx, pool, auth.DesktopAccountID, thread.ID, assistantIntermediateID, 2, "assistant", "tool call", `{"run_id":"`+runID+`","intermediate":true}`, true); err != nil {
		t.Fatalf("insert assistant intermediate: %v", err)
	}
	if err := insertDesktopMessageRow(ctx, pool, auth.DesktopAccountID, thread.ID, toolIntermediateID, 3, "tool", "tool result", `{"run_id":"`+runID+`","intermediate":true}`, true); err != nil {
		t.Fatalf("insert tool intermediate: %v", err)
	}
	if err := insertDesktopMessageRow(ctx, pool, auth.DesktopAccountID, thread.ID, finalAssistantID, 4, "assistant", "final", `{"run_id":"`+runID+`"}`, false); err != nil {
		t.Fatalf("insert final assistant: %v", err)
	}

	hiddenID, err := messageRepo.HideLastAssistantMessage(ctx, auth.DesktopAccountID, thread.ID)
	if err != nil {
		t.Fatalf("hide last assistant: %v", err)
	}
	if hiddenID != finalAssistantID {
		t.Fatalf("unexpected hidden id: got %s want %s", hiddenID, finalAssistantID)
	}

	rows, err := pool.Query(ctx, `SELECT id, hidden, deleted_at FROM messages WHERE thread_id = $1 ORDER BY thread_seq ASC`, thread.ID)
	if err != nil {
		t.Fatalf("query hidden state: %v", err)
	}
	defer rows.Close()

	hiddenByID := map[uuid.UUID]bool{}
	deletedByID := map[uuid.UUID]string{}
	for rows.Next() {
		var id uuid.UUID
		var hidden bool
		var deletedAt *string
		if err := rows.Scan(&id, &hidden, &deletedAt); err != nil {
			t.Fatalf("scan hidden state: %v", err)
		}
		hiddenByID[id] = hidden
		if deletedAt != nil {
			deletedByID[id] = *deletedAt
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
	if !hiddenByID[assistantIntermediateID] || !hiddenByID[toolIntermediateID] || !hiddenByID[finalAssistantID] {
		t.Fatalf("expected same-run history hidden, got %#v", hiddenByID)
	}
	if deletedByID[assistantIntermediateID] == "" || deletedByID[toolIntermediateID] == "" || deletedByID[finalAssistantID] == "" {
		t.Fatalf("expected same-run history soft-deleted, got %#v", deletedByID)
	}
}

func TestCopyUpToDesktopAllowsEmptyCanonicalHistory(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "copy-empty.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}

	projectRepo, err := data.NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}
	threadRepo, err := data.NewThreadRepository(pool)
	if err != nil {
		t.Fatalf("new thread repo: %v", err)
	}
	messageRepo, err := data.NewMessageRepository(pool)
	if err != nil {
		t.Fatalf("new message repo: %v", err)
	}

	project, err := projectRepo.GetOrCreateDefaultByOwner(ctx, auth.DesktopAccountID, auth.DesktopUserID)
	if err != nil {
		t.Fatalf("get or create default project: %v", err)
	}

	userID := auth.DesktopUserID
	sourceThread, err := threadRepo.Create(ctx, auth.DesktopAccountID, &userID, project.ID, nil, false)
	if err != nil {
		t.Fatalf("create source thread: %v", err)
	}
	targetThread, err := threadRepo.Create(ctx, auth.DesktopAccountID, &userID, project.ID, nil, false)
	if err != nil {
		t.Fatalf("create target thread: %v", err)
	}

	hiddenMessageID := uuid.New()
	if err := insertDesktopMessageRow(ctx, pool, auth.DesktopAccountID, sourceThread.ID, hiddenMessageID, 1, "assistant", "stale hidden", `{}`, true); err != nil {
		t.Fatalf("insert hidden message: %v", err)
	}

	pairs, err := messageRepo.CopyUpTo(ctx, auth.DesktopAccountID, sourceThread.ID, targetThread.ID, hiddenMessageID)
	if err != nil {
		t.Fatalf("copy up to hidden message: %v", err)
	}
	if len(pairs) != 0 {
		t.Fatalf("expected empty copy result, got %#v", pairs)
	}
}

func TestCopyUpToDesktopSkipsRolledBackIntermediateHistory(t *testing.T) {
	ctx := context.Background()

	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(t.TempDir(), "copy-skips-rolled-back.db"))
	if err != nil {
		t.Fatalf("auto migrate sqlite: %v", err)
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := auth.SeedDesktopUser(ctx, pool); err != nil {
		t.Fatalf("seed desktop user: %v", err)
	}

	projectRepo, err := data.NewProjectRepository(pool)
	if err != nil {
		t.Fatalf("new project repo: %v", err)
	}
	threadRepo, err := data.NewThreadRepository(pool)
	if err != nil {
		t.Fatalf("new thread repo: %v", err)
	}
	messageRepo, err := data.NewMessageRepository(pool)
	if err != nil {
		t.Fatalf("new message repo: %v", err)
	}

	project, err := projectRepo.GetOrCreateDefaultByOwner(ctx, auth.DesktopAccountID, auth.DesktopUserID)
	if err != nil {
		t.Fatalf("get or create default project: %v", err)
	}

	userID := auth.DesktopUserID
	sourceThread, err := threadRepo.Create(ctx, auth.DesktopAccountID, &userID, project.ID, nil, false)
	if err != nil {
		t.Fatalf("create source thread: %v", err)
	}
	if _, err := messageRepo.Create(ctx, auth.DesktopAccountID, sourceThread.ID, "user", "before", &userID); err != nil {
		t.Fatalf("create first message: %v", err)
	}

	runID := uuid.NewString()
	assistantIntermediateID := uuid.New()
	finalAssistantID := uuid.New()
	if err := insertDesktopMessageRow(ctx, pool, auth.DesktopAccountID, sourceThread.ID, assistantIntermediateID, 2, "assistant", "tool call", `{"run_id":"`+runID+`","intermediate":"true"}`, true); err != nil {
		t.Fatalf("insert assistant intermediate: %v", err)
	}
	if err := insertDesktopMessageRow(ctx, pool, auth.DesktopAccountID, sourceThread.ID, finalAssistantID, 3, "assistant", "final", `{"run_id":"`+runID+`"}`, false); err != nil {
		t.Fatalf("insert final assistant: %v", err)
	}

	if _, err := messageRepo.HideLastAssistantMessage(ctx, auth.DesktopAccountID, sourceThread.ID); err != nil {
		t.Fatalf("hide last assistant: %v", err)
	}

	targetThread, err := threadRepo.Create(ctx, auth.DesktopAccountID, &userID, project.ID, nil, false)
	if err != nil {
		t.Fatalf("create target thread: %v", err)
	}
	pairs, err := messageRepo.CopyUpTo(ctx, auth.DesktopAccountID, sourceThread.ID, targetThread.ID, finalAssistantID)
	if err != nil {
		t.Fatalf("copy rolled-back history: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("expected only stable pre-run history to copy, got %#v", pairs)
	}

	rows, err := pool.Query(ctx, `SELECT role, hidden FROM messages WHERE thread_id = $1 ORDER BY thread_seq ASC`, targetThread.ID)
	if err != nil {
		t.Fatalf("query copied target thread: %v", err)
	}
	defer rows.Close()
	var roles []string
	for rows.Next() {
		var role string
		var hidden bool
		if err := rows.Scan(&role, &hidden); err != nil {
			t.Fatalf("scan copied row: %v", err)
		}
		if hidden {
			t.Fatalf("did not expect copied rows to remain hidden")
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
	if len(roles) != 1 || roles[0] != "user" {
		t.Fatalf("unexpected copied roles after rollback: %#v", roles)
	}
}

func insertDesktopMessageRow(
	ctx context.Context,
	db *sqlitepgx.Pool,
	accountID uuid.UUID,
	threadID uuid.UUID,
	messageID uuid.UUID,
	threadSeq int64,
	role string,
	content string,
	metadataJSON string,
	hidden bool,
) error {
	if _, err := db.Exec(
		ctx,
		`INSERT INTO messages (
			id, account_id, thread_id, thread_seq, role, content, metadata_json, hidden
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8
		)`,
		messageID,
		accountID,
		threadID,
		threadSeq,
		role,
		content,
		metadataJSON,
		hidden,
	); err != nil {
		return err
	}
	_, err := db.Exec(
		ctx,
		`UPDATE threads
		    SET next_message_seq = CASE
		        WHEN next_message_seq <= $2 THEN $2 + 1
		        ELSE next_message_seq
		    END
		  WHERE id = $1`,
		threadID,
		threadSeq,
	)
	return err
}
