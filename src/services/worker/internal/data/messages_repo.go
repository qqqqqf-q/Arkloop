package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MessagesRepository struct{}

type ThreadMessage struct {
	Role        string
	Content     string
	ContentJSON json.RawMessage
}

type ConversationSearchHit struct {
	ThreadID  uuid.UUID
	Role      string
	Content   string
	CreatedAt time.Time
}

func (MessagesRepository) InsertAssistantMessage(
	ctx context.Context,
	tx pgx.Tx,
	orgID uuid.UUID,
	threadID uuid.UUID,
	runID uuid.UUID,
	content string,
) (uuid.UUID, error) {
	if strings.TrimSpace(content) == "" {
		return uuid.Nil, nil
	}
	metadataJSON := map[string]any{}
	if runID != uuid.Nil {
		metadataJSON["run_id"] = runID.String()
	}
	metadataRaw, err := json.Marshal(metadataJSON)
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal metadata_json: %w", err)
	}
	var messageID uuid.UUID
	err = tx.QueryRow(
		ctx,
		`INSERT INTO messages (
			org_id, thread_id, created_by_user_id, role, content, metadata_json
		) VALUES (
			$1, $2, NULL, $3, $4, $5::jsonb
		)
		 RETURNING id`,
		orgID,
		threadID,
		"assistant",
		content,
		string(metadataRaw),
	).Scan(&messageID)
	if err != nil {
		return uuid.Nil, err
	}
	return messageID, nil
}

func (MessagesRepository) FindAssistantMessageByRunID(
	ctx context.Context,
	tx pgx.Tx,
	runID uuid.UUID,
) (*uuid.UUID, string, error) {
	if tx == nil {
		return nil, "", fmt.Errorf("tx must not be nil")
	}
	if runID == uuid.Nil {
		return nil, "", fmt.Errorf("run_id must not be empty")
	}

	var (
		messageID uuid.UUID
		content   string
	)
	err := tx.QueryRow(
		ctx,
		`SELECT id, content
		   FROM messages
		  WHERE role = 'assistant'
		    AND metadata_json->>'run_id' = $1
		    AND deleted_at IS NULL
		  ORDER BY created_at DESC, id DESC
		  LIMIT 1`,
		runID.String(),
	).Scan(&messageID, &content)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", nil
		}
		return nil, "", err
	}
	return &messageID, strings.TrimSpace(content), nil
}

func (MessagesRepository) ListByThread(
	ctx context.Context,
	tx pgx.Tx,
	orgID uuid.UUID,
	threadID uuid.UUID,
	limit int,
) ([]ThreadMessage, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := tx.Query(
		ctx,
		`SELECT role, content, content_json
		 FROM messages
		 WHERE org_id = $1
		   AND thread_id = $2
		   AND hidden = FALSE
		   AND deleted_at IS NULL
		 ORDER BY created_at ASC
		 LIMIT $3`,
		orgID,
		threadID,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ThreadMessage{}
	for rows.Next() {
		var item ThreadMessage
		if err := rows.Scan(&item.Role, &item.Content, &item.ContentJSON); err != nil {
			return nil, err
		}
		item.Role = strings.TrimSpace(item.Role)
		item.Content = strings.TrimSpace(item.Content)
		if item.Role == "" {
			continue
		}
		out = append(out, item)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return out, nil
}

func (MessagesRepository) SearchVisibleByOwner(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID uuid.UUID,
	ownerUserID uuid.UUID,
	query string,
	limit int,
) ([]ConversationSearchHit, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	trimmedQuery := strings.TrimSpace(query)
	if trimmedQuery == "" {
		return nil, fmt.Errorf("query must not be empty")
	}
	if limit <= 0 {
		limit = 10
	}

	like := "%" + escapeILikePattern(trimmedQuery) + "%"
	rows, err := pool.Query(
		ctx,
		`SELECT m.thread_id, m.role, m.content, m.created_at
		 FROM messages m
		 JOIN threads t ON t.id = m.thread_id
		 WHERE m.org_id = $1
		   AND t.org_id = $1
		   AND t.created_by_user_id = $2
		   AND t.deleted_at IS NULL
		   AND t.is_private = FALSE
		   AND m.deleted_at IS NULL
		   AND m.hidden = FALSE
		   AND m.content ILIKE $3 ESCAPE '!'
		 ORDER BY m.created_at DESC, m.id DESC
		 LIMIT $4`,
		orgID, ownerUserID, like, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hits := make([]ConversationSearchHit, 0, limit)
	for rows.Next() {
		var item ConversationSearchHit
		if err := rows.Scan(&item.ThreadID, &item.Role, &item.Content, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Role = strings.TrimSpace(item.Role)
		item.Content = strings.TrimSpace(item.Content)
		hits = append(hits, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return hits, nil
}

func escapeILikePattern(input string) string {
	replacer := strings.NewReplacer(
		"!", "!!",
		"%", "!%",
		"_", "!_",
	)
	return replacer.Replace(input)
}
