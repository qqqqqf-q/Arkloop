package data

import (
	"context"
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
	CreatedAt       time.Time
	Hidden          bool
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
		 RETURNING id, org_id, thread_id, created_by_user_id, role, content, created_at, hidden`,
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
		`SELECT id, org_id, thread_id, created_by_user_id, role, content, created_at, hidden
		 FROM messages
		 WHERE org_id = $1
		   AND thread_id = $2
		   AND hidden = FALSE
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
