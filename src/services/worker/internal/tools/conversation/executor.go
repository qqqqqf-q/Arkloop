//go:build !desktop

package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	errorArgsInvalid     = "tool.args_invalid"
	errorIdentityMissing = "tool.conversation_identity_missing"
	errorSearchFailed    = "tool.conversation_search_failed"
	errorThreadNotFound  = "tool.thread_not_found"

	defaultLimit    = 10
	maxLimit        = 20
	contentMaxRunes = 280

	threadListMaxLimit     = 30
	threadMsgsDefaultLimit = 20
	threadMsgsMaxLimit     = 50
	threadMsgsMaxRunes     = 2000
)

type searchRepository interface {
	SearchVisibleByOwner(ctx context.Context, pool *pgxpool.Pool, accountID uuid.UUID, ownerUserID uuid.UUID, query string, limit int) ([]data.ConversationSearchHit, error)
}

type threadsRepository interface {
	ListByOwner(ctx context.Context, pool *pgxpool.Pool, accountID uuid.UUID, ownerUserID uuid.UUID, limit int, offset int, modeFilter string) ([]data.ThreadListItem, error)
	ListVisibleMessages(ctx context.Context, pool *pgxpool.Pool, accountID uuid.UUID, ownerUserID uuid.UUID, threadID uuid.UUID, limit int, offset int, roleFilter string, orderDesc bool) ([]data.VisibleMessage, error)
}

type ToolExecutor struct {
	pool        *pgxpool.Pool
	contextDB   contextDB
	repo        searchRepository
	threadsRepo threadsRepository
}

func NewToolExecutor(pool *pgxpool.Pool, repo searchRepository) *ToolExecutor {
	if repo == nil {
		repo = data.MessagesRepository{}
	}
	return &ToolExecutor{pool: pool, contextDB: pool, repo: repo, threadsRepo: data.ThreadsRepository{}}
}

func (e *ToolExecutor) Execute(ctx context.Context, toolName string, args map[string]any, execCtx tools.ExecutionContext, _ string) tools.ExecutionResult {
	started := time.Now()
	switch toolName {
	case ContextAgentSpec.Name:
		return executeContextTool(ctx, args, execCtx, started, e.contextDB)
	case ThreadListAgentSpec.Name:
		return e.executeThreadList(ctx, args, execCtx, started)
	case ThreadMessagesAgentSpec.Name:
		return e.executeThreadMessages(ctx, args, execCtx, started)
	default:
		return e.executeSearch(ctx, args, execCtx, started)
	}
}

func (e *ToolExecutor) executeSearch(ctx context.Context, args map[string]any, execCtx tools.ExecutionContext, started time.Time) tools.ExecutionResult {
	if execCtx.AccountID == nil || execCtx.UserID == nil {
		return executionError(errorIdentityMissing, "account_id and user_id are required", started)
	}
	query, ok := args["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return executionError(errorArgsInvalid, "query must be a non-empty string", started)
	}
	if e.pool == nil {
		return executionError(errorSearchFailed, "conversation search pool not available", started)
	}
	if e.repo == nil {
		return executionError(errorSearchFailed, "conversation search repository not available", started)
	}

	limit := parseIntArg(args, "limit", defaultLimit, 1, maxLimit)
	hits, err := e.repo.SearchVisibleByOwner(ctx, e.pool, *execCtx.AccountID, *execCtx.UserID, query, limit)
	if err != nil {
		return executionError(errorSearchFailed, fmt.Sprintf("conversation search failed: %s", err.Error()), started)
	}

	messages := make([]map[string]any, 0, len(hits))
	for _, hit := range hits {
		messages = append(messages, map[string]any{
			"thread_id":  hit.ThreadID.String(),
			"role":       hit.Role,
			"content":    truncateRunes(strings.TrimSpace(hit.Content), contentMaxRunes),
			"created_at": hit.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"messages": messages},
		DurationMs: durationMs(started),
	}
}

func (e *ToolExecutor) executeThreadList(ctx context.Context, args map[string]any, execCtx tools.ExecutionContext, started time.Time) tools.ExecutionResult {
	if execCtx.AccountID == nil || execCtx.UserID == nil {
		return executionError(errorIdentityMissing, "account_id and user_id are required", started)
	}
	if e.pool == nil {
		return executionError(errorSearchFailed, "pool not available", started)
	}

	limit := parseIntArg(args, "limit", 10, 1, threadListMaxLimit)
	offset := parseIntArg(args, "offset", 0, 0, 10000)

	modeFilter := ""
	if m, ok := args["mode"].(string); ok && (m == "chat" || m == "work") {
		modeFilter = m
	}

	items, err := e.threadsRepo.ListByOwner(ctx, e.pool, *execCtx.AccountID, *execCtx.UserID, limit, offset, modeFilter)
	if err != nil {
		return executionError(errorSearchFailed, fmt.Sprintf("list threads failed: %s", err.Error()), started)
	}

	threads := make([]map[string]any, 0, len(items))
	for _, item := range items {
		title := ""
		if item.Title != nil {
			title = *item.Title
		}
		threads = append(threads, map[string]any{
			"id":            item.ID.String(),
			"title":         title,
			"mode":          item.Mode,
			"message_count": item.MessageCount,
			"updated_at":    item.UpdatedAt.UTC().Format(time.RFC3339),
			"created_at":    item.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"threads": threads},
		DurationMs: durationMs(started),
	}
}

func (e *ToolExecutor) executeThreadMessages(ctx context.Context, args map[string]any, execCtx tools.ExecutionContext, started time.Time) tools.ExecutionResult {
	if execCtx.AccountID == nil || execCtx.UserID == nil {
		return executionError(errorIdentityMissing, "account_id and user_id are required", started)
	}
	if e.pool == nil {
		return executionError(errorSearchFailed, "pool not available", started)
	}

	threadIDStr, ok := args["thread_id"].(string)
	if !ok || strings.TrimSpace(threadIDStr) == "" {
		return executionError(errorArgsInvalid, "thread_id is required", started)
	}
	threadID, err := uuid.Parse(threadIDStr)
	if err != nil {
		return executionError(errorArgsInvalid, "thread_id must be a valid UUID", started)
	}

	limit := parseIntArg(args, "limit", threadMsgsDefaultLimit, 1, threadMsgsMaxLimit)
	offset := parseIntArg(args, "offset", 0, 0, 10000)

	roleFilter := ""
	if r, ok := args["role"].(string); ok && (r == "user" || r == "assistant") {
		roleFilter = r
	}

	orderDesc := true
	if o, ok := args["order"].(string); ok && o == "asc" {
		orderDesc = false
	}

	msgs, err := e.threadsRepo.ListVisibleMessages(ctx, e.pool, *execCtx.AccountID, *execCtx.UserID, threadID, limit, offset, roleFilter, orderDesc)
	if err != nil {
		if errors.Is(err, data.ErrThreadNotFound) {
			return executionError(errorThreadNotFound, "thread not found or access denied", started)
		}
		return executionError(errorSearchFailed, fmt.Sprintf("list messages failed: %s", err.Error()), started)
	}

	messages := make([]map[string]any, 0, len(msgs))
	for _, msg := range msgs {
		messages = append(messages, map[string]any{
			"role":       strings.TrimSpace(msg.Role),
			"content":    truncateRunes(strings.TrimSpace(msg.Content), threadMsgsMaxRunes),
			"created_at": msg.CreatedAt.UTC().Format(time.RFC3339),
			"thread_seq": msg.ThreadSeq,
		})
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"messages": messages},
		DurationMs: durationMs(started),
	}
}

func parseLimit(args map[string]any, fallback int) int {
	return parseIntArg(args, "limit", fallback, 1, maxLimit)
}

func parseIntArg(args map[string]any, key string, fallback, min, max int) int {
	val, exists := args[key]
	if !exists {
		return fallback
	}
	var n int
	switch v := val.(type) {
	case float64:
		n = int(v)
	case int:
		n = v
	case int64:
		n = int(v)
	case json.Number:
		if i, err := v.Int64(); err == nil {
			n = int(i)
		} else {
			return fallback
		}
	default:
		return fallback
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

func truncateRunes(value string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxRunes]) + "..."
}

func executionError(class, message string, started time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error: &tools.ExecutionError{
			ErrorClass: class,
			Message:    message,
		},
		DurationMs: durationMs(started),
	}
}

func durationMs(started time.Time) int {
	ms := int(time.Since(started) / time.Millisecond)
	if ms < 0 {
		return 0
	}
	return ms
}

type contextDB interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}
