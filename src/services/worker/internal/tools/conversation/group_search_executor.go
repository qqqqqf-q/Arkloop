//go:build !desktop

package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type groupSearchRepository interface {
	SearchByThread(ctx context.Context, pool *pgxpool.Pool, threadID uuid.UUID, query string, limit int) ([]data.GroupSearchHit, error)
}

type GroupSearchExecutor struct {
	pool *pgxpool.Pool
	repo groupSearchRepository
}

func NewGroupSearchExecutor(pool *pgxpool.Pool, repo groupSearchRepository) *GroupSearchExecutor {
	if repo == nil {
		repo = groupSearchRepoAdapter{}
	}
	return &GroupSearchExecutor{pool: pool, repo: repo}
}

type groupSearchRepoAdapter struct{}

func (groupSearchRepoAdapter) SearchByThread(ctx context.Context, pool *pgxpool.Pool, threadID uuid.UUID, query string, limit int) ([]data.GroupSearchHit, error) {
	return data.MessagesRepository{}.SearchByThread(ctx, pool, threadID, query, limit)
}

func (e *GroupSearchExecutor) Execute(ctx context.Context, _ string, args map[string]any, execCtx tools.ExecutionContext, _ string) tools.ExecutionResult {
	started := time.Now()
	if execCtx.ThreadID == nil {
		return executionError("tool.group_search_identity_missing", "thread_id is required", started)
	}
	query, ok := args["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return executionError(errorArgsInvalid, "query must be a non-empty string", started)
	}
	if e.pool == nil {
		return executionError("tool.group_search_failed", "group search pool not available", started)
	}

	limit := parseLimit(args, defaultLimit)
	hits, err := e.repo.SearchByThread(ctx, e.pool, *execCtx.ThreadID, query, limit)
	if err != nil {
		return executionError("tool.group_search_failed", fmt.Sprintf("group search failed: %s", err.Error()), started)
	}

	messages := make([]map[string]any, 0, len(hits))
	for _, hit := range hits {
		entry := map[string]any{
			"role":       hit.Role,
			"content":    truncateRunes(strings.TrimSpace(hit.Content), contentMaxRunes),
			"created_at": hit.CreatedAt.UTC().Format(time.RFC3339),
		}
		if keys := extractAttachmentKeys(hit.ContentJSON); len(keys) > 0 {
			entry["attachment_keys"] = keys
		}
		messages = append(messages, entry)
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"messages": messages},
		DurationMs: durationMs(started),
	}
}

func extractAttachmentKeys(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var content messagecontent.Content
	if err := json.Unmarshal(raw, &content); err != nil {
		return nil
	}
	var keys []string
	for _, part := range content.Parts {
		if part.Type == messagecontent.PartTypeImage && part.Attachment != nil && part.Attachment.Key != "" {
			keys = append(keys, part.Attachment.Key)
		}
	}
	return keys
}
