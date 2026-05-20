//go:build desktop

package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrThreadNotFound = errors.New("thread not found or access denied")

type ThreadListItem struct {
	ID           uuid.UUID
	Title        *string
	Mode         string
	MessageCount int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type VisibleMessage struct {
	Role      string
	Content   string
	CreatedAt time.Time
	ThreadSeq int64
}

type ThreadsRepository struct{}

func (ThreadsRepository) ListByOwner(
	ctx context.Context,
	pool DesktopDB,
	accountID uuid.UUID,
	ownerUserID uuid.UUID,
	limit int,
	offset int,
	modeFilter string,
) ([]ThreadListItem, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool must not be nil")
	}
	if limit <= 0 {
		limit = 10
	}
	if offset < 0 {
		offset = 0
	}

	args := []any{accountID, ownerUserID}
	modeClause := ""
	if modeFilter != "" {
		args = append(args, modeFilter)
		modeClause = fmt.Sprintf(" AND t.mode = $%d", len(args))
	}
	args = append(args, limit, offset)
	limitIdx := len(args) - 1
	offsetIdx := len(args)

	q := fmt.Sprintf(
		`SELECT t.id, t.title, t.mode,
		        (SELECT COUNT(*) FROM messages m
		          WHERE m.thread_id = t.id AND m.account_id = $1
		            AND m.deleted_at IS NULL AND m.hidden = FALSE) AS message_count,
		        t.created_at, t.updated_at
		   FROM threads t
		  WHERE t.account_id = $1
		    AND t.created_by_user_id = $2
		    AND t.deleted_at IS NULL
		    AND t.is_private = FALSE%s
		  ORDER BY t.updated_at DESC, t.id DESC
		  LIMIT $%d OFFSET $%d`,
		modeClause, limitIdx, offsetIdx,
	)

	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list threads: %w", err)
	}
	defer rows.Close()

	items := make([]ThreadListItem, 0, limit)
	for rows.Next() {
		var item ThreadListItem
		if err := rows.Scan(&item.ID, &item.Title, &item.Mode, &item.MessageCount, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan thread: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (ThreadsRepository) ListVisibleMessages(
	ctx context.Context,
	pool DesktopDB,
	accountID uuid.UUID,
	ownerUserID uuid.UUID,
	threadID uuid.UUID,
	limit int,
	offset int,
	roleFilter string,
	orderDesc bool,
) ([]VisibleMessage, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool must not be nil")
	}
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT TRUE FROM threads
		  WHERE id = $1 AND account_id = $2 AND created_by_user_id = $3
		    AND deleted_at IS NULL AND is_private = FALSE`,
		threadID, accountID, ownerUserID,
	).Scan(&exists)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrThreadNotFound
		}
		return nil, fmt.Errorf("verify thread ownership: %w", err)
	}

	args := []any{threadID, accountID}
	roleClause := ""
	if roleFilter != "" {
		args = append(args, roleFilter)
		roleClause = fmt.Sprintf(" AND m.role = $%d", len(args))
	}

	order := "DESC"
	if !orderDesc {
		order = "ASC"
	}

	args = append(args, limit, offset)
	limitIdx := len(args) - 1
	offsetIdx := len(args)

	q := fmt.Sprintf(
		`SELECT m.role, m.content, m.created_at, m.thread_seq
		   FROM messages m
		  WHERE m.thread_id = $1 AND m.account_id = $2
		    AND m.deleted_at IS NULL AND m.hidden = FALSE%s
		  ORDER BY m.thread_seq %s
		  LIMIT $%d OFFSET $%d`,
		roleClause, order, limitIdx, offsetIdx,
	)

	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	msgs := make([]VisibleMessage, 0, limit)
	for rows.Next() {
		var msg VisibleMessage
		if err := rows.Scan(&msg.Role, &msg.Content, &msg.CreatedAt, &msg.ThreadSeq); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		msgs = append(msgs, msg)
	}
	return msgs, rows.Err()
}
