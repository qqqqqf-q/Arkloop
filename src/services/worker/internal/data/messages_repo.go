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

type GroupSearchHit struct {
	Role        string
	Content     string
	ContentJSON json.RawMessage
	CreatedAt   time.Time
}

func (MessagesRepository) InsertAssistantMessage(
	ctx context.Context,
	tx pgx.Tx,
	accountID uuid.UUID,
	threadID uuid.UUID,
	runID uuid.UUID,
	content string,
	contentJSON json.RawMessage,
	hidden bool,
) (uuid.UUID, error) {
	return (MessagesRepository{}).InsertAssistantMessageWithMetadata(ctx, tx, accountID, threadID, runID, content, contentJSON, hidden, nil)
}

func (MessagesRepository) InsertAssistantMessageWithMetadata(
	ctx context.Context,
	tx pgx.Tx,
	accountID uuid.UUID,
	threadID uuid.UUID,
	runID uuid.UUID,
	content string,
	contentJSON json.RawMessage,
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
	threadSeq, err := AllocateThreadSeqRange(ctx, tx, accountID, threadID, 1)
	if err != nil {
		return uuid.Nil, err
	}
	createdAt := currentTimestampText()
	var messageID uuid.UUID
	err = tx.QueryRow(
		ctx,
		`INSERT INTO messages (
			account_id, thread_id, thread_seq, created_by_user_id, role, content, content_json, metadata_json, hidden, created_at
		) VALUES (
			$1, $2, $3, NULL, $4, $5, $6, $7::jsonb, $8, $9
		)
		 RETURNING id`,
		accountID,
		threadID,
		threadSeq,
		"assistant",
		content,
		contentJSON,
		string(metadataRaw),
		hidden,
		createdAt,
	).Scan(&messageID)
	if err != nil {
		return uuid.Nil, err
	}
	return messageID, nil
}

func (MessagesRepository) InsertIntermediateMessage(
	ctx context.Context,
	tx pgx.Tx,
	accountID, threadID uuid.UUID,
	threadSeq int64,
	role, content string,
	contentJSON json.RawMessage,
	metadataJSON json.RawMessage,
	createdAt time.Time,
) (uuid.UUID, error) {
	id := uuid.New()
	_, err := tx.Exec(
		ctx,
		`INSERT INTO messages (id, account_id, thread_id, thread_seq, role, content, content_json, metadata_json, hidden, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, TRUE, $9)`,
		id, accountID, threadID, threadSeq, role, content, contentJSON, metadataJSON, createdAt,
	)
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
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
		  ORDER BY thread_seq DESC
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
				SELECT id, role, content, content_json, created_at, metadata_json, thread_seq
				  FROM messages m
				 WHERE m.account_id = $1
				   AND m.thread_id = $2
				   AND m.deleted_at IS NULL
				   AND COALESCE(m.compacted, false) = false
				   AND (
				     m.hidden = FALSE
				     OR (
				       m.metadata_json->>'intermediate' = 'true'
				       AND EXISTS (
				         SELECT 1
				           FROM messages final
				          WHERE final.account_id = m.account_id
				            AND final.thread_id = m.thread_id
				            AND final.deleted_at IS NULL
				            AND final.hidden = FALSE
				            AND final.role = 'assistant'
				            AND NULLIF(final.metadata_json->>'run_id', '') = NULLIF(m.metadata_json->>'run_id', '')
				       )
				     )
				   )
				 ORDER BY thread_seq DESC
				 LIMIT $3
			 ) recent
		 LEFT JOIN LATERAL (
			SELECT output_tokens
			  FROM usage_records
			 WHERE run_id = (recent.metadata_json->>'run_id')::uuid
			   AND usage_type = 'llm'
			 LIMIT 1
		 ) u ON true
			 ORDER BY recent.thread_seq ASC`,
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

func (MessagesRepository) ListByThreadUpToID(
	ctx context.Context,
	tx pgx.Tx,
	accountID uuid.UUID,
	threadID uuid.UUID,
	upToMessageID uuid.UUID,
	limit int,
) ([]ThreadMessage, error) {
	if limit <= 0 {
		limit = 200
	}
	if upToMessageID == uuid.Nil {
		return nil, fmt.Errorf("up_to_message_id must not be empty")
	}
	rows, err := tx.Query(
		ctx,
		`SELECT recent.id, recent.role, recent.content, recent.content_json, recent.metadata_json, recent.created_at,
			        COALESCE(u.output_tokens, 0) as output_tokens
			 FROM (
				SELECT id, role, content, content_json, created_at, metadata_json, thread_seq
				  FROM messages m
				 WHERE m.account_id = $1
				   AND m.thread_id = $2
				   AND m.deleted_at IS NULL
				   AND COALESCE(m.compacted, false) = false
				   AND (
				     m.hidden = FALSE
				     OR (
				       m.metadata_json->>'intermediate' = 'true'
				       AND EXISTS (
				         SELECT 1
				           FROM messages final
				          WHERE final.account_id = m.account_id
				            AND final.thread_id = m.thread_id
				            AND final.deleted_at IS NULL
				            AND final.hidden = FALSE
				            AND final.role = 'assistant'
				            AND NULLIF(final.metadata_json->>'run_id', '') = NULLIF(m.metadata_json->>'run_id', '')
				       )
				     )
				   )
				   AND thread_seq <= (
				     SELECT thread_seq
				       FROM messages
				      WHERE account_id = $1
				        AND thread_id = $2
				        AND id = $3
				        AND deleted_at IS NULL
				   )
				 ORDER BY thread_seq DESC
				 LIMIT $4
			 ) recent
		 LEFT JOIN LATERAL (
			SELECT output_tokens
			  FROM usage_records
			 WHERE run_id = (recent.metadata_json->>'run_id')::uuid
			   AND usage_type = 'llm'
			 LIMIT 1
		 ) u ON true
			 ORDER BY recent.thread_seq ASC`,
		accountID,
		threadID,
		upToMessageID,
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
	if len(out) == 0 {
		return nil, fmt.Errorf("thread history upper bound message not found")
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
		   AND (m.hidden = FALSE OR m.metadata_json->>'intermediate' = 'true')
		   AND m.deleted_at IS NULL
			 ORDER BY m.thread_seq ASC`,
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
			 	SELECT id, role, content, content_json, created_at, metadata_json, thread_seq
			 	  FROM messages
			 	 WHERE account_id = $1
			 	   AND thread_id = $2
			 	   AND (hidden = FALSE OR metadata_json->>'intermediate' = 'true')
			 	   AND deleted_at IS NULL
			 	   AND COALESCE(compacted, false) = false
			 	 ORDER BY thread_seq DESC
			 	 LIMIT $3
			 ) recent
		 LEFT JOIN LATERAL (
			SELECT output_tokens
			  FROM usage_records
			 WHERE run_id = (recent.metadata_json->>'run_id')::uuid
			   AND usage_type = 'llm'
			 LIMIT 1
		 ) u ON true
			 ORDER BY recent.thread_seq ASC`,
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
	threadSeq, err := AllocateThreadSeqRange(ctx, tx, accountID, threadID, 1)
	if err != nil {
		return uuid.Nil, err
	}
	createdAt := currentTimestampText()
	var messageID uuid.UUID
	err = tx.QueryRow(
		ctx,
		`INSERT INTO messages (
			account_id, thread_id, thread_seq, created_by_user_id, role, content, content_json, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8
		)
		 RETURNING id`,
		accountID,
		threadID,
		threadSeq,
		createdByUserID,
		trimmedRole,
		trimmedContent,
		contentJSON,
		createdAt,
	).Scan(&messageID)
	if err != nil {
		return uuid.Nil, err
	}
	return messageID, nil
}

func currentTimestampText() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05.000000000 -0700")
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

func (MessagesRepository) SearchByThread(
	ctx context.Context,
	pool *pgxpool.Pool,
	threadID uuid.UUID,
	query string,
	limit int,
) ([]GroupSearchHit, error) {
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
		`SELECT m.role, m.content, m.content_json, m.created_at
		 FROM messages m
		 WHERE m.thread_id = $1
		   AND m.deleted_at IS NULL
		   AND m.hidden = FALSE
		   AND m.content ILIKE $2 ESCAPE '!'
		 ORDER BY m.created_at DESC, m.id DESC
		 LIMIT $3`,
		threadID, like, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hits := make([]GroupSearchHit, 0, limit)
	for rows.Next() {
		var item GroupSearchHit
		if err := rows.Scan(&item.Role, &item.Content, &item.ContentJSON, &item.CreatedAt); err != nil {
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
