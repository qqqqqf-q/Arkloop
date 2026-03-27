//go:build !desktop

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
	ID           uuid.UUID
	Role         string
	Content      string
	ContentJSON  json.RawMessage
	MetadataJSON json.RawMessage
	CreatedAt    time.Time
	OutputTokens *int64 // assistant 消息的实际 output tokens，从 usage_records JOIN
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
	accountID uuid.UUID,
	threadID uuid.UUID,
	runID uuid.UUID,
	content string,
	hidden bool,
) (uuid.UUID, error) {
	return (MessagesRepository{}).InsertAssistantMessageWithMetadata(ctx, tx, accountID, threadID, runID, content, hidden, nil)
}

func (MessagesRepository) InsertAssistantMessageWithMetadata(
	ctx context.Context,
	tx pgx.Tx,
	accountID uuid.UUID,
	threadID uuid.UUID,
	runID uuid.UUID,
	content string,
	hidden bool,
	metadata map[string]any,
) (uuid.UUID, error) {
	if strings.TrimSpace(content) == "" {
		return uuid.Nil, nil
	}
	metadataJSON := map[string]any{}
	for key, value := range metadata {
		metadataJSON[key] = value
	}
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
			account_id, thread_id, created_by_user_id, role, content, metadata_json, hidden
		) VALUES (
			$1, $2, NULL, $3, $4, $5::jsonb, $6
		)
		 RETURNING id`,
		accountID,
		threadID,
		"assistant",
		content,
		string(metadataRaw),
		hidden,
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
	accountID uuid.UUID,
	threadID uuid.UUID,
	limit int,
) ([]ThreadMessage, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := tx.Query(
		ctx,
		`SELECT recent.id, recent.role, recent.content, recent.content_json, recent.metadata_json, recent.created_at,
		        COALESCE(u.output_tokens, 0) as output_tokens
		 FROM (
			SELECT id, role, content, content_json, created_at, metadata_json
			  FROM messages
			 WHERE account_id = $1
			   AND thread_id = $2
			   AND hidden = FALSE
			   AND deleted_at IS NULL
			   AND COALESCE(compacted, false) = false
			 ORDER BY created_at DESC, id DESC
			 LIMIT $3
		 ) recent
		 LEFT JOIN LATERAL (
			SELECT output_tokens
			  FROM usage_records
			 WHERE run_id = (recent.metadata_json->>'run_id')::uuid
			   AND usage_type = 'llm'
			 LIMIT 1
		 ) u ON true
		 ORDER BY recent.created_at ASC, recent.id ASC`,
		accountID,
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
		if err := rows.Scan(&item.ID, &item.Role, &item.Content, &item.ContentJSON, &item.MetadataJSON, &item.CreatedAt, &item.OutputTokens); err != nil {
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

func (MessagesRepository) ListByIDs(
	ctx context.Context,
	tx pgx.Tx,
	accountID uuid.UUID,
	threadID uuid.UUID,
	messageIDs []uuid.UUID,
) ([]ThreadMessage, error) {
	if tx == nil {
		return nil, fmt.Errorf("tx must not be nil")
	}
	if accountID == uuid.Nil || threadID == uuid.Nil {
		return nil, fmt.Errorf("account_id and thread_id must not be empty")
	}
	if len(messageIDs) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(
		ctx,
		`SELECT m.id, m.role, m.content, m.content_json, m.metadata_json, m.created_at,
		        COALESCE(u.output_tokens, 0) as output_tokens
		 FROM messages m
		 LEFT JOIN LATERAL (
			SELECT output_tokens
			  FROM usage_records
			 WHERE run_id = (m.metadata_json->>'run_id')::uuid
			   AND usage_type = 'llm'
			 LIMIT 1
		 ) u ON true
		 WHERE m.account_id = $1
		   AND m.thread_id = $2
		   AND m.id = ANY($3)
		   AND m.hidden = FALSE
		   AND m.deleted_at IS NULL
		 ORDER BY m.created_at ASC, m.id ASC`,
		accountID,
		threadID,
		messageIDs,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ThreadMessage, 0, len(messageIDs))
	for rows.Next() {
		var item ThreadMessage
		if err := rows.Scan(&item.ID, &item.Role, &item.Content, &item.ContentJSON, &item.MetadataJSON, &item.CreatedAt, &item.OutputTokens); err != nil {
			return nil, err
		}
		item.Role = strings.TrimSpace(item.Role)
		item.Content = strings.TrimSpace(item.Content)
		if item.Role == "" {
			continue
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (MessagesRepository) ListRecentByThread(
	ctx context.Context,
	tx pgx.Tx,
	accountID uuid.UUID,
	threadID uuid.UUID,
	limit int,
) ([]ThreadMessage, error) {
	if tx == nil {
		return nil, fmt.Errorf("tx must not be nil")
	}
	if accountID == uuid.Nil || threadID == uuid.Nil {
		return nil, fmt.Errorf("account_id and thread_id must not be empty")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be positive")
	}
	rows, err := tx.Query(
		ctx,
		`SELECT recent.id, recent.role, recent.content, recent.content_json, recent.created_at,
		        COALESCE(u.output_tokens, 0) as output_tokens
		 FROM (
		 	SELECT id, role, content, content_json, created_at, metadata_json
		 	  FROM messages
		 	 WHERE account_id = $1
		 	   AND thread_id = $2
		 	   AND hidden = FALSE
		 	   AND deleted_at IS NULL
		 	   AND COALESCE(compacted, false) = false
		 	 ORDER BY created_at DESC, id DESC
		 	 LIMIT $3
		 ) recent
		 LEFT JOIN LATERAL (
			SELECT output_tokens
			  FROM usage_records
			 WHERE run_id = (recent.metadata_json->>'run_id')::uuid
			   AND usage_type = 'llm'
			 LIMIT 1
		 ) u ON true
		 ORDER BY recent.created_at ASC, recent.id ASC`,
		accountID,
		threadID,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ThreadMessage, 0, limit)
	for rows.Next() {
		var item ThreadMessage
		if err := rows.Scan(&item.ID, &item.Role, &item.Content, &item.ContentJSON, &item.CreatedAt, &item.OutputTokens); err != nil {
			return nil, err
		}
		item.Role = strings.TrimSpace(item.Role)
		item.Content = strings.TrimSpace(item.Content)
		if item.Role == "" {
			continue
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (MessagesRepository) InsertThreadMessage(
	ctx context.Context,
	tx pgx.Tx,
	accountID uuid.UUID,
	threadID uuid.UUID,
	role string,
	content string,
	contentJSON json.RawMessage,
	createdByUserID *uuid.UUID,
) (uuid.UUID, error) {
	if tx == nil {
		return uuid.Nil, fmt.Errorf("tx must not be nil")
	}
	if accountID == uuid.Nil || threadID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("account_id and thread_id must not be empty")
	}
	trimmedRole := strings.TrimSpace(role)
	if trimmedRole == "" {
		return uuid.Nil, fmt.Errorf("role must not be empty")
	}
	trimmedContent := strings.TrimSpace(content)
	if trimmedContent == "" {
		return uuid.Nil, fmt.Errorf("content must not be empty")
	}
	var messageID uuid.UUID
	err := tx.QueryRow(
		ctx,
		`INSERT INTO messages (
			account_id, thread_id, created_by_user_id, role, content, content_json
		) VALUES (
			$1, $2, $3, $4, $5, $6
		)
		 RETURNING id`,
		accountID,
		threadID,
		createdByUserID,
		trimmedRole,
		trimmedContent,
		contentJSON,
	).Scan(&messageID)
	if err != nil {
		return uuid.Nil, err
	}
	return messageID, nil
}

// InsertCompactSummaryMessage 写入线程级摘要（system role，metadata 标记 kind）。
func (MessagesRepository) InsertCompactSummaryMessage(
	ctx context.Context,
	tx pgx.Tx,
	accountID uuid.UUID,
	threadID uuid.UUID,
	content string,
	metadata json.RawMessage,
) (uuid.UUID, error) {
	if tx == nil {
		return uuid.Nil, fmt.Errorf("tx must not be nil")
	}
	if accountID == uuid.Nil || threadID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("account_id and thread_id must not be empty")
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return uuid.Nil, fmt.Errorf("content must not be empty")
	}
	if len(metadata) == 0 {
		metadata = []byte(`{}`)
	}
	emptyParts := json.RawMessage(`{"parts":[]}`)
	var messageID uuid.UUID
	err := tx.QueryRow(
		ctx,
		`INSERT INTO messages (
			account_id, thread_id, created_by_user_id, role, content, content_json, metadata_json, hidden, compacted
		) VALUES (
			$1, $2, NULL, 'system', $3, $4, $5::jsonb, false, false
		)
		 RETURNING id`,
		accountID,
		threadID,
		content,
		emptyParts,
		string(metadata),
	).Scan(&messageID)
	if err != nil {
		return uuid.Nil, err
	}
	return messageID, nil
}

// MarkThreadMessagesCompacted 将消息标记为已压缩并从常规列表中隐藏。
func (MessagesRepository) MarkThreadMessagesCompacted(
	ctx context.Context,
	tx pgx.Tx,
	accountID uuid.UUID,
	threadID uuid.UUID,
	messageIDs []uuid.UUID,
) error {
	if tx == nil {
		return fmt.Errorf("tx must not be nil")
	}
	if accountID == uuid.Nil || threadID == uuid.Nil {
		return fmt.Errorf("account_id and thread_id must not be empty")
	}
	if len(messageIDs) == 0 {
		return nil
	}
	_, err := tx.Exec(
		ctx,
		`UPDATE messages
		    SET compacted = true,
		        hidden = true
		  WHERE account_id = $1
		    AND thread_id = $2
		    AND id = ANY($3::uuid[])
		    AND deleted_at IS NULL`,
		accountID,
		threadID,
		messageIDs,
	)
	return err
}

func (MessagesRepository) SearchVisibleByOwner(
	ctx context.Context,
	pool *pgxpool.Pool,
	accountID uuid.UUID,
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
		 WHERE m.account_id = $1
		   AND t.account_id = $1
		   AND t.created_by_user_id = $2
		   AND t.deleted_at IS NULL
		   AND t.is_private = FALSE
		   AND m.deleted_at IS NULL
		   AND m.hidden = FALSE
		   AND m.content ILIKE $3 ESCAPE '!'
		 ORDER BY m.created_at DESC, m.id DESC
		 LIMIT $4`,
		accountID, ownerUserID, like, limit,
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
