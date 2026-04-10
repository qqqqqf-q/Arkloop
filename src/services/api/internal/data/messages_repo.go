package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"arkloop/services/shared/messagecontent"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Message struct {
	ID              uuid.UUID
	AccountID       uuid.UUID
	ThreadID        uuid.UUID
	ThreadSeq       int64
	CreatedByUserID *uuid.UUID
	Role            string
	Content         string
	// R14: 多模态预留字段，NULL 表示纯文本消息（读取 content）
	ContentJSON  json.RawMessage
	MetadataJSON json.RawMessage
	TokenCount   *int32
	DeletedAt    *time.Time
	CreatedAt    time.Time
	Hidden       bool
}

type NoAssistantMessageError struct{}

func (e NoAssistantMessageError) Error() string {
	return "no assistant message to hide"
}

type ThreadNotFoundError struct {
	ThreadID uuid.UUID
}

func (e ThreadNotFoundError) Error() string {
	return "thread not found"
}

type MessageRepository struct {
	db Querier
}

func (r *MessageRepository) WithTx(tx pgx.Tx) *MessageRepository {
	return &MessageRepository{db: tx}
}

func NewMessageRepository(db Querier) (*MessageRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &MessageRepository{db: db}, nil
}

func (r *MessageRepository) withWriteTx(ctx context.Context, fn func(q Querier) error) error {
	if tx, ok := r.db.(pgx.Tx); ok {
		return fn(tx)
	}
	starter, ok := r.db.(TxStarter)
	if !ok {
		return fmt.Errorf("db does not support transactions")
	}
	tx, err := starter.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func allocateThreadSeqRange(ctx context.Context, q Querier, accountID, threadID uuid.UUID, count int64) (int64, error) {
	if count <= 0 {
		return 0, fmt.Errorf("count must be positive")
	}
	var startSeq int64
	err := q.QueryRow(
		ctx,
		`UPDATE threads
		    SET next_message_seq = next_message_seq + $3
		  WHERE id = $2
		    AND account_id = $1
		RETURNING next_message_seq - $3`,
		accountID,
		threadID,
		count,
	).Scan(&startSeq)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ThreadNotFoundError{ThreadID: threadID}
		}
		return 0, err
	}
	return startSeq, nil
}

func (r *MessageRepository) Create(
	ctx context.Context,
	accountID uuid.UUID,
	threadID uuid.UUID,
	role string,
	content string,
	createdByUserID *uuid.UUID,
) (Message, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return Message{}, fmt.Errorf("account_id must not be empty")
	}
	if threadID == uuid.Nil {
		return Message{}, fmt.Errorf("thread_id must not be empty")
	}
	if role == "" {
		return Message{}, fmt.Errorf("role must not be empty")
	}
	if content == "" {
		return Message{}, fmt.Errorf("content must not be empty")
	}

	var message Message
	err := r.withWriteTx(ctx, func(q Querier) error {
		threadSeq, err := allocateThreadSeqRange(ctx, q, accountID, threadID, 1)
		if err != nil {
			return err
		}
		return q.QueryRow(
			ctx,
			`INSERT INTO messages (account_id, thread_id, thread_seq, created_by_user_id, role, content, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)
			 RETURNING id, account_id, thread_id, created_by_user_id, role, content,
			           content_json, metadata_json, token_count, deleted_at, created_at, hidden, thread_seq`,
			accountID,
			threadID,
			threadSeq,
			createdByUserID,
			role,
			content,
			currentTimestampText(),
		).Scan(
			&message.ID,
			&message.AccountID,
			&message.ThreadID,
			&message.CreatedByUserID,
			&message.Role,
			&message.Content,
			&message.ContentJSON,
			&message.MetadataJSON,
			&message.TokenCount,
			&message.DeletedAt,
			&message.CreatedAt,
			&message.Hidden,
			&message.ThreadSeq,
		)
	})
	if err != nil {
		return Message{}, err
	}
	return message, nil
}

func (r *MessageRepository) CreateStructured(
	ctx context.Context,
	accountID uuid.UUID,
	threadID uuid.UUID,
	role string,
	content string,
	contentJSON json.RawMessage,
	createdByUserID *uuid.UUID,
) (Message, error) {
	return r.CreateStructuredWithMetadata(ctx, accountID, threadID, role, content, contentJSON, nil, createdByUserID)
}

func (r *MessageRepository) CreateStructuredWithMetadata(
	ctx context.Context,
	accountID uuid.UUID,
	threadID uuid.UUID,
	role string,
	content string,
	contentJSON json.RawMessage,
	metadataJSON json.RawMessage,
	createdByUserID *uuid.UUID,
) (Message, error) {
	slog.Debug("CreateStructuredWithMetadata", "accountID", accountID, "threadID", threadID, "role", role, "content_len", len(content))
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return Message{}, fmt.Errorf("account_id must not be empty")
	}
	if threadID == uuid.Nil {
		return Message{}, fmt.Errorf("thread_id must not be empty")
	}
	if role == "" {
		return Message{}, fmt.Errorf("role must not be empty")
	}
	if content == "" {
		return Message{}, fmt.Errorf("content must not be empty")
	}

	var normalizedContentJSON json.RawMessage
	if len(contentJSON) > 0 {
		normalizedContentJSON = contentJSON
	} else {
		// SQLite requires NOT NULL, use empty object
		normalizedContentJSON = json.RawMessage("{}")
	}
	var normalizedMetadataJSON json.RawMessage
	if len(metadataJSON) > 0 {
		normalizedMetadataJSON = metadataJSON
	} else {
		// SQLite requires NOT NULL, use empty object
		normalizedMetadataJSON = json.RawMessage("{}")
	}

	var message Message
	err := r.withWriteTx(ctx, func(q Querier) error {
		threadSeq, err := allocateThreadSeqRange(ctx, q, accountID, threadID, 1)
		if err != nil {
			return err
		}
		return q.QueryRow(
			ctx,
			`INSERT INTO messages (account_id, thread_id, thread_seq, created_by_user_id, role, content, content_json, metadata_json, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			 RETURNING id, account_id, thread_id, created_by_user_id, role, content,
			           content_json, metadata_json, token_count, deleted_at, created_at, hidden, thread_seq`,
			accountID,
			threadID,
			threadSeq,
			createdByUserID,
			role,
			content,
			normalizedContentJSON,
			normalizedMetadataJSON,
			currentTimestampText(),
		).Scan(
			&message.ID,
			&message.AccountID,
			&message.ThreadID,
			&message.CreatedByUserID,
			&message.Role,
			&message.Content,
			&message.ContentJSON,
			&message.MetadataJSON,
			&message.TokenCount,
			&message.DeletedAt,
			&message.CreatedAt,
			&message.Hidden,
			&message.ThreadSeq,
		)
	})
	if err != nil {
		slog.Debug("CreateStructuredWithMetadata query error", "error", err, "errorType", fmt.Sprintf("%T", err))
		return Message{}, err
	}
	slog.Debug("CreateStructuredWithMetadata success", "messageID", message.ID)

	return message, nil
}

func currentTimestampText() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05.000000000 -0700")
}

func (r *MessageRepository) ListByThread(
	ctx context.Context,
	accountID uuid.UUID,
	threadID uuid.UUID,
	limit int,
) ([]Message, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return nil, fmt.Errorf("account_id must not be empty")
	}
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("thread_id must not be empty")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be positive")
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT id, account_id, thread_id, created_by_user_id, role, content,
		        content_json, metadata_json, token_count, deleted_at, created_at, hidden, thread_seq
		 FROM (
			SELECT id, account_id, thread_id, created_by_user_id, role, content,
			       content_json, metadata_json, token_count, deleted_at, created_at, hidden, thread_seq
			  FROM messages
			 WHERE account_id = $1
			   AND thread_id = $2
			   AND hidden = FALSE
			   AND deleted_at IS NULL
			   AND COALESCE(compacted, false) = false
			 ORDER BY thread_seq DESC
			 LIMIT $3
		 ) recent
		 ORDER BY thread_seq ASC`,
		accountID,
		threadID,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var message Message
		if err := rows.Scan(
			&message.ID,
			&message.AccountID,
			&message.ThreadID,
			&message.CreatedByUserID,
			&message.Role,
			&message.Content,
			&message.ContentJSON,
			&message.MetadataJSON,
			&message.TokenCount,
			&message.DeletedAt,
			&message.CreatedAt,
			&message.Hidden,
			&message.ThreadSeq,
		); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return messages, nil
}

func (r *MessageRepository) GetByID(
	ctx context.Context,
	accountID uuid.UUID,
	threadID uuid.UUID,
	messageID uuid.UUID,
) (*Message, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil || threadID == uuid.Nil || messageID == uuid.Nil {
		return nil, fmt.Errorf("accountID, threadID and messageID must not be empty")
	}

	var message Message
	err := r.db.QueryRow(
		ctx,
		`SELECT id, account_id, thread_id, created_by_user_id, role, content,
		        content_json, metadata_json, token_count, deleted_at, created_at, hidden, thread_seq
		 FROM messages
		 WHERE account_id = $1
		   AND thread_id = $2
		   AND id = $3
		   AND deleted_at IS NULL`,
		accountID,
		threadID,
		messageID,
	).Scan(
		&message.ID,
		&message.AccountID,
		&message.ThreadID,
		&message.CreatedByUserID,
		&message.Role,
		&message.Content,
		&message.ContentJSON,
		&message.MetadataJSON,
		&message.TokenCount,
		&message.DeletedAt,
		&message.CreatedAt,
		&message.Hidden,
		&message.ThreadSeq,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &message, nil
}

func (r *MessageRepository) GetLatestVisibleMessage(
	ctx context.Context,
	accountID uuid.UUID,
	threadID uuid.UUID,
) (*Message, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil || threadID == uuid.Nil {
		return nil, fmt.Errorf("accountID and threadID must not be empty")
	}

	var message Message
	err := r.db.QueryRow(
		ctx,
		`SELECT id, account_id, thread_id, created_by_user_id, role, content,
		        content_json, metadata_json, token_count, deleted_at, created_at, hidden, thread_seq
		   FROM messages
		  WHERE account_id = $1
		    AND thread_id = $2
		    AND hidden = FALSE
		    AND deleted_at IS NULL
		    AND COALESCE(compacted, false) = false
		  ORDER BY thread_seq DESC
		  LIMIT 1`,
		accountID,
		threadID,
	).Scan(
		&message.ID,
		&message.AccountID,
		&message.ThreadID,
		&message.CreatedByUserID,
		&message.Role,
		&message.Content,
		&message.ContentJSON,
		&message.MetadataJSON,
		&message.TokenCount,
		&message.DeletedAt,
		&message.CreatedAt,
		&message.Hidden,
		&message.ThreadSeq,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &message, nil
}

// UpdateContent 更新指定用户消息的内容。仅允许更新 role=user 的可见消息。
func (r *MessageRepository) UpdateContent(
	ctx context.Context,
	accountID uuid.UUID,
	threadID uuid.UUID,
	messageID uuid.UUID,
	newContent string,
) (Message, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil || threadID == uuid.Nil || messageID == uuid.Nil {
		return Message{}, fmt.Errorf("accountID, threadID and messageID must not be empty")
	}
	if newContent == "" {
		return Message{}, fmt.Errorf("content must not be empty")
	}

	var message Message
	err := r.db.QueryRow(
		ctx,
		`UPDATE messages
		 SET content = $4
		 WHERE id = $3
		   AND thread_id = $2
		   AND account_id = $1
		   AND role = 'user'
		   AND hidden = FALSE
		   AND deleted_at IS NULL
		 RETURNING id, account_id, thread_id, created_by_user_id, role, content,
		           content_json, metadata_json, token_count, deleted_at, created_at, hidden, thread_seq`,
		accountID, threadID, messageID, newContent,
	).Scan(
		&message.ID, &message.AccountID, &message.ThreadID, &message.CreatedByUserID,
		&message.Role, &message.Content, &message.ContentJSON, &message.MetadataJSON,
		&message.TokenCount, &message.DeletedAt, &message.CreatedAt, &message.Hidden, &message.ThreadSeq,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Message{}, fmt.Errorf("message not found or not editable")
		}
		return Message{}, err
	}
	return message, nil
}

func (r *MessageRepository) UpdateStructuredContent(
	ctx context.Context,
	accountID uuid.UUID,
	threadID uuid.UUID,
	messageID uuid.UUID,
	newContent string,
	contentJSON json.RawMessage,
) (Message, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil || threadID == uuid.Nil || messageID == uuid.Nil {
		return Message{}, fmt.Errorf("accountID, threadID and messageID must not be empty")
	}
	if newContent == "" {
		return Message{}, fmt.Errorf("content must not be empty")
	}

	var normalizedContentJSON json.RawMessage
	if len(contentJSON) > 0 {
		normalizedContentJSON = contentJSON
	}

	var message Message
	err := r.db.QueryRow(
		ctx,
		`UPDATE messages
		 SET content = $4,
		     content_json = $5
		 WHERE id = $3
		   AND thread_id = $2
		   AND account_id = $1
		   AND role = 'user'
		   AND hidden = FALSE
		   AND deleted_at IS NULL
		 RETURNING id, account_id, thread_id, created_by_user_id, role, content,
		           content_json, metadata_json, token_count, deleted_at, created_at, hidden, thread_seq`,
		accountID, threadID, messageID, newContent, normalizedContentJSON,
	).Scan(
		&message.ID, &message.AccountID, &message.ThreadID, &message.CreatedByUserID,
		&message.Role, &message.Content, &message.ContentJSON, &message.MetadataJSON,
		&message.TokenCount, &message.DeletedAt, &message.CreatedAt, &message.Hidden, &message.ThreadSeq,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Message{}, fmt.Errorf("message not found or not editable")
		}
		return Message{}, err
	}
	return message, nil
}

// HideMessagesAfter 隐藏该 thread 中在指定消息之后的所有消息。
// “之后”按 thread_seq 排序判断，确保与 ListByThread 顺序一致。
func (r *MessageRepository) HideMessagesAfter(
	ctx context.Context,
	accountID uuid.UUID,
	threadID uuid.UUID,
	afterMessageID uuid.UUID,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil || threadID == uuid.Nil || afterMessageID == uuid.Nil {
		return fmt.Errorf("accountID, threadID and afterMessageID must not be empty")
	}

	_, err := r.db.Exec(
		ctx,
		`UPDATE messages
		 SET hidden = TRUE,
		     deleted_at = $4
		 WHERE account_id = $1
		   AND thread_id = $2
		   AND deleted_at IS NULL
		   AND thread_seq > (
		     SELECT thread_seq FROM messages WHERE id = $3 AND account_id = $1
		   )`,
		accountID, threadID, afterMessageID, currentTimestampText(),
	)
	return err
}

// HideLastAssistantMessage 将该 thread 最后一条可见的 assistant 消息以及同 run 的
// intermediate 历史标记为 hidden。
// 若不存在这样的消息，返回 NoAssistantMessageError。
func (r *MessageRepository) HideLastAssistantMessage(
	ctx context.Context,
	accountID uuid.UUID,
	threadID uuid.UUID,
) (uuid.UUID, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("account_id must not be empty")
	}
	if threadID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("thread_id must not be empty")
	}

	var hiddenID uuid.UUID
	err := r.withWriteTx(ctx, func(q Querier) error {
		var (
			targetID    uuid.UUID
			targetMeta  json.RawMessage
			targetRunID string
		)
		if err := q.QueryRow(
			ctx,
			`SELECT id, metadata_json
			   FROM messages
			  WHERE account_id = $1
			    AND thread_id = $2
			    AND role = 'assistant'
			    AND hidden = FALSE
			    AND deleted_at IS NULL
			  ORDER BY thread_seq DESC
			  LIMIT 1`,
			accountID,
			threadID,
		).Scan(&targetID, &targetMeta); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return NoAssistantMessageError{}
			}
			return err
		}

		var metadata map[string]any
		if len(targetMeta) > 0 && json.Unmarshal(targetMeta, &metadata) == nil {
			if value, _ := metadata["run_id"].(string); strings.TrimSpace(value) != "" {
				targetRunID = strings.TrimSpace(value)
			}
		}

		idsToHide := []uuid.UUID{targetID}
		if targetRunID != "" {
			rows, err := q.Query(
				ctx,
				`SELECT id, metadata_json
				   FROM messages
				  WHERE account_id = $1
				    AND thread_id = $2
				    AND deleted_at IS NULL`,
				accountID,
				threadID,
			)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var (
					messageID   uuid.UUID
					metadataRaw json.RawMessage
				)
				if err := rows.Scan(&messageID, &metadataRaw); err != nil {
					return err
				}
				if messageID == targetID {
					continue
				}
				var candidate map[string]any
				if len(metadataRaw) == 0 || json.Unmarshal(metadataRaw, &candidate) != nil {
					continue
				}
				if value, _ := candidate["run_id"].(string); strings.TrimSpace(value) == targetRunID {
					idsToHide = append(idsToHide, messageID)
				}
			}
			if err := rows.Err(); err != nil {
				return err
			}
		}

		if _, err := q.Exec(
			ctx,
			`UPDATE messages
			    SET hidden = TRUE,
			        deleted_at = $4
			  WHERE account_id = $1
			    AND thread_id = $2
			    AND deleted_at IS NULL
			    AND id = ANY($3::uuid[])`,
			accountID,
			threadID,
			idsToHide,
			currentTimestampText(),
		); err != nil {
			return err
		}

		hiddenID = targetID
		return nil
	})
	if err != nil {
		var noAssistant NoAssistantMessageError
		if errors.As(err, &noAssistant) {
			return uuid.Nil, noAssistant
		}
		return uuid.Nil, err
	}

	return hiddenID, nil
}

// MessageIDPair 记录一条消息在 fork 复制中对应的旧/新 ID。
type MessageIDPair struct {
	OldID uuid.UUID
	NewID uuid.UUID
}

// CopyUpTo 将 sourceThreadID 中截止到 upToMessageID（含）的 canonical 历史复制到 targetThreadID。
// 返回每条消息的 old→new ID 映射，调用方可据此迁移客户端侧缓存。
func (r *MessageRepository) CopyUpTo(
	ctx context.Context,
	accountID uuid.UUID,
	sourceThreadID uuid.UUID,
	targetThreadID uuid.UUID,
	upToMessageID uuid.UUID,
) ([]MessageIDPair, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil || sourceThreadID == uuid.Nil || targetThreadID == uuid.Nil || upToMessageID == uuid.Nil {
		return nil, fmt.Errorf("accountID, sourceThreadID, targetThreadID and upToMessageID must not be empty")
	}

	type sourceMessage struct {
		OldID           uuid.UUID
		CreatedByUserID *uuid.UUID
		Role            string
		Content         string
		ContentJSON     json.RawMessage
		MetadataJSON    json.RawMessage
		Hidden          bool
		CreatedAt       time.Time
		ThreadSeq       int64
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT id, created_by_user_id, role, content, content_json, metadata_json, hidden, created_at, thread_seq
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
		     WHERE id = $3
		       AND account_id = $1
		       AND thread_id = $2
		   )
		 ORDER BY thread_seq ASC`,
		accountID, sourceThreadID, upToMessageID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sourceMessages []sourceMessage
	for rows.Next() {
		var message sourceMessage
		if err := rows.Scan(
			&message.OldID,
			&message.CreatedByUserID,
			&message.Role,
			&message.Content,
			&message.ContentJSON,
			&message.MetadataJSON,
			&message.Hidden,
			&message.CreatedAt,
			&message.ThreadSeq,
		); err != nil {
			return nil, err
		}
		sourceMessages = append(sourceMessages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	pairs := make([]MessageIDPair, 0, len(sourceMessages))
	if len(sourceMessages) == 0 {
		return pairs, nil
	}
	if err := r.withWriteTx(ctx, func(q Querier) error {
		startSeq, err := allocateThreadSeqRange(ctx, q, accountID, targetThreadID, int64(len(sourceMessages)))
		if err != nil {
			return err
		}
		for index, message := range sourceMessages {
			newID := uuid.New()
			threadSeq := startSeq + int64(index)
			if _, err := q.Exec(
				ctx,
				`INSERT INTO messages (id, account_id, thread_id, thread_seq, created_by_user_id, role, content, content_json, metadata_json, hidden, created_at)
					 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
				newID,
				accountID,
				targetThreadID,
				threadSeq,
				message.CreatedByUserID,
				message.Role,
				message.Content,
				message.ContentJSON,
				message.MetadataJSON,
				message.Hidden,
				message.CreatedAt,
			); err != nil {
				return err
			}
			pairs = append(pairs, MessageIDPair{OldID: message.OldID, NewID: newID})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return pairs, nil
}

func (r *MessageRepository) ListAllAttachmentKeysByThread(
	ctx context.Context,
	accountID uuid.UUID,
	threadID uuid.UUID,
) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil || threadID == uuid.Nil {
		return nil, fmt.Errorf("account_id and thread_id must not be empty")
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT content_json
		   FROM messages
		  WHERE account_id = $1
		    AND thread_id = $2`,
		accountID,
		threadID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := make(map[string]struct{})
	keys := make([]string, 0)
	for rows.Next() {
		var contentJSON json.RawMessage
		if err := rows.Scan(&contentJSON); err != nil {
			return nil, err
		}
		content, err := messagecontent.Parse(contentJSON)
		if err != nil {
			continue
		}
		for _, part := range content.Parts {
			if part.Attachment == nil {
				continue
			}
			key := strings.TrimSpace(part.Attachment.Key)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return keys, nil
}
