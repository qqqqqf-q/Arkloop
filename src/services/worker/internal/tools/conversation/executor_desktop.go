//go:build desktop

package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

const (
	errorArgsInvalid     = "tool.args_invalid"
	errorIdentityMissing = "tool.conversation_identity_missing"
	errorSearchFailed    = "tool.conversation_search_failed"

	defaultLimit    = 10
	maxLimit        = 20
	contentMaxRunes = 280
)

type searchRepository interface {
	SearchVisibleByOwner(ctx context.Context, pool data.DesktopDB, accountID uuid.UUID, ownerUserID uuid.UUID, query string, limit int) ([]data.ConversationSearchHit, error)
}

type ToolExecutor struct {
	db   data.DesktopDB
	repo searchRepository
}

func NewToolExecutor(db data.DesktopDB, repo searchRepository) *ToolExecutor {
	if repo == nil {
		repo = data.MessagesRepository{}
	}
	return &ToolExecutor{db: db, repo: repo}
}

func (e *ToolExecutor) Execute(ctx context.Context, _ string, args map[string]any, execCtx tools.ExecutionContext, _ string) tools.ExecutionResult {
	started := time.Now()
	if execCtx.AccountID == nil || execCtx.UserID == nil {
		return executionError(errorIdentityMissing, "account_id and user_id are required", started)
	}
	query, ok := args["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return executionError(errorArgsInvalid, "query must be a non-empty string", started)
	}
	if e.db == nil {
		return executionError(errorSearchFailed, "conversation search pool not available", started)
	}
	if e.repo == nil {
		return executionError(errorSearchFailed, "conversation search repository not available", started)
	}

	limit := parseLimit(args, defaultLimit)
	hits, err := e.repo.SearchVisibleByOwner(ctx, e.db, *execCtx.AccountID, *execCtx.UserID, query, limit)
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

func parseLimit(args map[string]any, fallback int) int {
	switch v := args["limit"].(type) {
	case float64:
		if n := int(v); n >= 1 {
			if n > maxLimit {
				return maxLimit
			}
			return n
		}
	case int:
		if v >= 1 {
			if v > maxLimit {
				return maxLimit
			}
			return v
		}
	case int64:
		if v >= 1 {
			if v > maxLimit {
				return maxLimit
			}
			return int(v)
		}
	case json.Number:
		if n, err := v.Int64(); err == nil && n >= 1 {
			if n > maxLimit {
				return maxLimit
			}
			return int(n)
		}
	}
	return fallback
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
