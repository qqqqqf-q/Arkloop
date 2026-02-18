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
}

type ThreadNotFoundError struct {
	ThreadID uuid.UUID
}

func (e ThreadNotFoundError) Error() string {
	return "Thread 不存在"
}

type MessageRepository struct {
	db Querier
}

func NewMessageRepository(db Querier) (*MessageRepository, error) {
	if db == nil {
		return nil, errors.New("db 不能为空")
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
		return Message{}, fmt.Errorf("org_id 不能为空")
	}
	if threadID == uuid.Nil {
		return Message{}, fmt.Errorf("thread_id 不能为空")
	}
	if role == "" {
		return Message{}, fmt.Errorf("role 不能为空")
	}
	if content == "" {
		return Message{}, fmt.Errorf("content 不能为空")
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
		 RETURNING id, org_id, thread_id, created_by_user_id, role, content, created_at`,
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
		return nil, fmt.Errorf("org_id 不能为空")
	}
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("thread_id 不能为空")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("limit 必须为正数")
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT id, org_id, thread_id, created_by_user_id, role, content, created_at
		 FROM messages
		 WHERE org_id = $1
		   AND thread_id = $2
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
