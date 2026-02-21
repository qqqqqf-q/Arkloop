package data

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type MessagesRepository struct{}

type ThreadMessage struct {
	Role    string
	Content string
}

func (MessagesRepository) InsertAssistantMessage(
	ctx context.Context,
	tx pgx.Tx,
	orgID uuid.UUID,
	threadID uuid.UUID,
	content string,
) error {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	_, err := tx.Exec(
		ctx,
		`INSERT INTO messages (
			org_id, thread_id, created_by_user_id, role, content
		) VALUES (
			$1, $2, NULL, $3, $4
		)`,
		orgID,
		threadID,
		"assistant",
		content,
	)
	return err
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
		`SELECT role, content
		 FROM messages
		 WHERE org_id = $1
		   AND thread_id = $2
		   AND hidden = FALSE
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
		if err := rows.Scan(&item.Role, &item.Content); err != nil {
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
