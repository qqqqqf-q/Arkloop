//go:build !desktop

package conversation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var testPool = new(pgxpool.Pool)

type repoMock struct {
	hits      []data.ConversationSearchHit
	err       error
	lastQuery string
	lastLimit int
}

func (m *repoMock) SearchVisibleByOwner(_ context.Context, _ *pgxpool.Pool, _ uuid.UUID, _ uuid.UUID, query string, limit int) ([]data.ConversationSearchHit, error) {
	m.lastQuery = query
	m.lastLimit = limit
	return m.hits, m.err
}

func newExecCtx() tools.ExecutionContext {
	accountID := uuid.New()
	userID := uuid.New()
	return tools.ExecutionContext{
		RunID:   uuid.New(),
		TraceID: "trace",
		AccountID:   &accountID,
		UserID:  &userID,
		Emitter: events.NewEmitter("trace"),
	}
}

func TestConversationExecutor_SearchSuccess(t *testing.T) {
	repo := &repoMock{hits: []data.ConversationSearchHit{{
		ThreadID:  uuid.New(),
		Role:      "assistant",
		Content:   "  this is a very useful memory  ",
		CreatedAt: time.Date(2026, 3, 8, 1, 2, 3, 0, time.UTC),
	}}}
	ex := NewToolExecutor(testPool, repo)
	result := ex.Execute(context.Background(), "conversation_search", map[string]any{"query": "memory"}, newExecCtx(), "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	messages, ok := result.ResultJSON["messages"].([]map[string]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("unexpected messages: %#v", result.ResultJSON["messages"])
	}
	if repo.lastLimit != defaultLimit {
		t.Fatalf("expected default limit %d, got %d", defaultLimit, repo.lastLimit)
	}
	if messages[0]["role"] != "assistant" {
		t.Fatalf("unexpected role: %v", messages[0]["role"])
	}
	if messages[0]["created_at"] != "2026-03-08T01:02:03Z" {
		t.Fatalf("unexpected created_at: %v", messages[0]["created_at"])
	}
	if messages[0]["content"] != "this is a very useful memory" {
		t.Fatalf("unexpected content: %v", messages[0]["content"])
	}
}

func TestConversationExecutor_TruncatesContent(t *testing.T) {
	repo := &repoMock{hits: []data.ConversationSearchHit{{
		ThreadID:  uuid.New(),
		Role:      "user",
		Content:   strings.Repeat("你", contentMaxRunes+5),
		CreatedAt: time.Now(),
	}}}
	ex := NewToolExecutor(testPool, repo)
	result := ex.Execute(context.Background(), "conversation_search", map[string]any{"query": "你好"}, newExecCtx(), "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	messages := result.ResultJSON["messages"].([]map[string]any)
	content := messages[0]["content"].(string)
	if !strings.HasSuffix(content, "...") {
		t.Fatalf("expected truncated content, got: %q", content)
	}
}

func TestConversationExecutor_ClampsLimit(t *testing.T) {
	repo := &repoMock{}
	ex := NewToolExecutor(testPool, repo)
	result := ex.Execute(context.Background(), "conversation_search", map[string]any{"query": "memory", "limit": 99}, newExecCtx(), "")
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error.Message)
	}
	if repo.lastLimit != maxLimit {
		t.Fatalf("expected clamped limit %d, got %d", maxLimit, repo.lastLimit)
	}
}

func TestConversationExecutor_EmptyQuery(t *testing.T) {
	ex := NewToolExecutor(testPool, &repoMock{})
	result := ex.Execute(context.Background(), "conversation_search", map[string]any{"query": ""}, newExecCtx(), "")
	if result.Error == nil || result.Error.ErrorClass != errorArgsInvalid {
		t.Fatalf("expected args_invalid, got: %+v", result.Error)
	}
}

func TestConversationExecutor_IdentityMissing(t *testing.T) {
	execCtx := newExecCtx()
	execCtx.UserID = nil
	ex := NewToolExecutor(testPool, &repoMock{})
	result := ex.Execute(context.Background(), "conversation_search", map[string]any{"query": "memory"}, execCtx, "")
	if result.Error == nil || result.Error.ErrorClass != errorIdentityMissing {
		t.Fatalf("expected identity_missing, got: %+v", result.Error)
	}
}

func TestConversationExecutor_SearchFailure(t *testing.T) {
	repo := &repoMock{err: errors.New("db down")}
	ex := NewToolExecutor(testPool, repo)
	result := ex.Execute(context.Background(), "conversation_search", map[string]any{"query": "memory", "limit": 3}, newExecCtx(), "")
	if result.Error == nil || result.Error.ErrorClass != errorSearchFailed {
		t.Fatalf("expected search_failed, got: %+v", result.Error)
	}
}
