package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Message struct {
	ID              uuid.UUID
	OrgID           uuid.UUID
	ThreadID        uuid.UUID
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

func NewMessageRepository(db Querier) (*MessageRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &MessageRepository{db: db}, nil
}

func (r *MessageRepository) Create(
	ctx context.Context,
	orgID uuid.UUID,
	threadID uuid.UUID,
	role string,
	content string,
	createdByUserID *uuid.UUID,
) (Message, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil {
		return Message{}, fmt.Errorf("org_id must not be empty")
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
	err := r.db.QueryRow(
		ctx,
		`WITH thread AS (
		   SELECT 1
		   FROM threads
		   WHERE id = $2
		     AND org_id = $1
		   LIMIT 1
		 )
		 INSERT INTO messages (org_id, thread_id, created_by_user_id, role, content)
		 SELECT $1, $2, $3, $4, $5
		 FROM thread
		 RETURNING id, org_id, thread_id, created_by_user_id, role, content,
		           content_json, metadata_json, token_count, deleted_at, created_at, hidden`,
		orgID,
		threadID,
		createdByUserID,
		role,
		content,
	).Scan(
		&message.ID,
		&message.OrgID,
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
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Message{}, ThreadNotFoundError{ThreadID: threadID}
		}
		return Message{}, err
	}

	return message, nil
}

func (r *MessageRepository) ListByThread(
	ctx context.Context,
	orgID uuid.UUID,
	threadID uuid.UUID,
	limit int,
) ([]Message, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil {
		return nil, fmt.Errorf("org_id must not be empty")
	}
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("thread_id must not be empty")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be positive")
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT id, org_id, thread_id, created_by_user_id, role, content,
		        content_json, metadata_json, token_count, deleted_at, created_at, hidden
		 FROM messages
		 WHERE org_id = $1
		   AND thread_id = $2
		   AND hidden = FALSE
		   AND deleted_at IS NULL
		 ORDER BY created_at ASC, id ASC
		 LIMIT $3`,
		orgID,
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
			&message.OrgID,
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

// UpdateContent 更新指定用户消息的内容。仅允许更新 role=user 的可见消息。
func (r *MessageRepository) UpdateContent(
	ctx context.Context,
	orgID uuid.UUID,
	threadID uuid.UUID,
	messageID uuid.UUID,
	newContent string,
) (Message, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil || threadID == uuid.Nil || messageID == uuid.Nil {
		return Message{}, fmt.Errorf("orgID, threadID and messageID must not be empty")
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
		   AND org_id = $1
		   AND role = 'user'
		   AND hidden = FALSE
		   AND deleted_at IS NULL
		 RETURNING id, org_id, thread_id, created_by_user_id, role, content,
		           content_json, metadata_json, token_count, deleted_at, created_at, hidden`,
		orgID, threadID, messageID, newContent,
	).Scan(
		&message.ID, &message.OrgID, &message.ThreadID, &message.CreatedByUserID,
		&message.Role, &message.Content, &message.ContentJSON, &message.MetadataJSON,
		&message.TokenCount, &message.DeletedAt, &message.CreatedAt, &message.Hidden,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Message{}, fmt.Errorf("message not found or not editable")
		}
		return Message{}, err
	}
	return message, nil
}

// HideMessagesAfter 隐藏该 thread 中在指定消息之后的所有可见消息。
// "之后"按 (created_at, id) 排序判断，确保与 ListByThread 顺序一致。
func (r *MessageRepository) HideMessagesAfter(
	ctx context.Context,
	orgID uuid.UUID,
	threadID uuid.UUID,
	afterMessageID uuid.UUID,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil || threadID == uuid.Nil || afterMessageID == uuid.Nil {
		return fmt.Errorf("orgID, threadID and afterMessageID must not be empty")
	}

	_, err := r.db.Exec(
		ctx,
		`UPDATE messages
		 SET hidden = TRUE
		 WHERE org_id = $1
		   AND thread_id = $2
		   AND hidden = FALSE
		   AND deleted_at IS NULL
		   AND (created_at, id) > (
		     SELECT created_at, id FROM messages WHERE id = $3 AND org_id = $1
		   )`,
		orgID, threadID, afterMessageID,
	)
	return err
}

// HideLastAssistantMessage 将该 thread 最后一条可见的 assistant 消息标记为 hidden。
// 若不存在这样的消息，返回 NoAssistantMessageError。
func (r *MessageRepository) HideLastAssistantMessage(
	ctx context.Context,
	orgID uuid.UUID,
	threadID uuid.UUID,
) (uuid.UUID, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("org_id must not be empty")
	}
	if threadID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("thread_id must not be empty")
	}

	var hiddenID uuid.UUID
	err := r.db.QueryRow(
		ctx,
		`UPDATE messages
		 SET hidden = TRUE
		 WHERE id = (
		   SELECT id FROM messages
		   WHERE org_id = $1
		     AND thread_id = $2
		     AND role = 'assistant'
		     AND hidden = FALSE
		     AND deleted_at IS NULL
		   ORDER BY created_at DESC, id DESC
		   LIMIT 1
		 )
		 AND org_id = $1
		 RETURNING id`,
		orgID,
		threadID,
	).Scan(&hiddenID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, NoAssistantMessageError{}
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

// CopyUpTo 将 sourceThreadID 中截止到 upToMessageID（含）的所有可见消息复制到 targetThreadID。
// 返回每条消息的 old→new ID 映射，调用方可据此迁移客户端侧缓存。
func (r *MessageRepository) CopyUpTo(
	ctx context.Context,
	orgID uuid.UUID,
	sourceThreadID uuid.UUID,
	targetThreadID uuid.UUID,
	upToMessageID uuid.UUID,
) ([]MessageIDPair, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil || sourceThreadID == uuid.Nil || targetThreadID == uuid.Nil || upToMessageID == uuid.Nil {
		return nil, fmt.Errorf("orgID, sourceThreadID, targetThreadID and upToMessageID must not be empty")
	}

	rows, err := r.db.Query(
		ctx,
		`WITH src AS (
		   SELECT id AS old_id, gen_random_uuid() AS new_id,
		          created_by_user_id, role, content, content_json, metadata_json, created_at
		   FROM messages
		   WHERE org_id = $1
		     AND thread_id = $2
		     AND hidden = FALSE
		     AND deleted_at IS NULL
		     AND (created_at, id) <= (
		       SELECT created_at, id FROM messages WHERE id = $4 AND org_id = $1
		     )
		   ORDER BY created_at ASC, id ASC
		 ),
		 inserted AS (
		   INSERT INTO messages (id, org_id, thread_id, created_by_user_id, role, content, content_json, metadata_json, created_at)
		   SELECT new_id, $1, $3, created_by_user_id, role, content, content_json, metadata_json, created_at
		   FROM src
		 )
		 SELECT old_id, new_id FROM src`,
		orgID, sourceThreadID, targetThreadID, upToMessageID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pairs []MessageIDPair
	for rows.Next() {
		var p MessageIDPair
		if err := rows.Scan(&p.OldID, &p.NewID); err != nil {
			return nil, err
		}
		pairs = append(pairs, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return pairs, nil
}
