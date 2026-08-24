package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Notification struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	AccountID       uuid.UUID
	Type        string
	Title       string
	Body        string
	PayloadJSON map[string]any
	ReadAt      *time.Time
	CreatedAt   time.Time
}

type NotificationsRepository struct {
	db Querier
}

func NewNotificationsRepository(db Querier) (*NotificationsRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &NotificationsRepository{db: db}, nil
}

func (r *NotificationsRepository) Create(
	ctx context.Context,
	userID uuid.UUID,
	accountID uuid.UUID,
	notifType string,
	title string,
	body string,
	payloadJSON map[string]any,
) (Notification, error) {
	if userID == uuid.Nil {
		return Notification{}, fmt.Errorf("notifications: user_id must not be empty")
	}
	if accountID == uuid.Nil {
		return Notification{}, fmt.Errorf("notifications: account_id must not be empty")
	}
	if notifType == "" {
		return Notification{}, fmt.Errorf("notifications: type must not be empty")
	}
	if title == "" {
		return Notification{}, fmt.Errorf("notifications: title must not be empty")
	}
	if payloadJSON == nil {
		payloadJSON = map[string]any{}
	}

	var n Notification
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO notifications (user_id, account_id, type, title, body, payload_json)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, user_id, account_id, type, title, body, payload_json, read_at, created_at`,
		userID, accountID, notifType, title, body, payloadJSON,
	).Scan(
		&n.ID, &n.UserID, &n.AccountID, &n.Type, &n.Title,
		&n.Body, &n.PayloadJSON, &n.ReadAt, &n.CreatedAt,
	)
	if err != nil {
		return Notification{}, fmt.Errorf("notifications.Create: %w", err)
	}
	return n, nil
}

func (r *NotificationsRepository) ListUnread(ctx context.Context, userID uuid.UUID) ([]Notification, error) {
	if userID == uuid.Nil {
		return nil, fmt.Errorf("notifications: user_id must not be empty")
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT n.id, n.user_id, n.account_id, n.type, n.title, n.body, n.payload_json, n.read_at, n.created_at
		 FROM notifications n
		 LEFT JOIN notification_broadcasts nb ON nb.id = n.broadcast_id
		 WHERE n.user_id = $1 AND n.read_at IS NULL
		   AND (n.broadcast_id IS NULL OR nb.deleted_at IS NULL)
		 ORDER BY n.created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("notifications.ListUnread: %w", err)
	}
	defer rows.Close()

	var results []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(
			&n.ID, &n.UserID, &n.AccountID, &n.Type, &n.Title,
			&n.Body, &n.PayloadJSON, &n.ReadAt, &n.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("notifications.ListUnread scan: %w", err)
		}
		results = append(results, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notifications.ListUnread rows: %w", err)
	}
	return results, nil
}

func (r *NotificationsRepository) List(ctx context.Context, userID uuid.UUID, limit int) ([]Notification, error) {
	if userID == uuid.Nil {
		return nil, fmt.Errorf("notifications: user_id must not be empty")
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT n.id, n.user_id, n.account_id, n.type, n.title, n.body, n.payload_json, n.read_at, n.created_at
		 FROM notifications n
		 LEFT JOIN notification_broadcasts nb ON nb.id = n.broadcast_id
		 WHERE n.user_id = $1
		   AND (n.broadcast_id IS NULL OR nb.deleted_at IS NULL)
		 ORDER BY n.created_at DESC
		 LIMIT $2`,
		userID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("notifications.List: %w", err)
	}
	defer rows.Close()

	var results []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(
			&n.ID, &n.UserID, &n.AccountID, &n.Type, &n.Title,
			&n.Body, &n.PayloadJSON, &n.ReadAt, &n.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("notifications.List scan: %w", err)
		}
		results = append(results, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notifications.List rows: %w", err)
	}
	return results, nil
}

func (r *NotificationsRepository) MarkAllRead(ctx context.Context, userID uuid.UUID) (int, error) {
	if userID == uuid.Nil {
		return 0, fmt.Errorf("notifications: user_id must not be empty")
	}
	tag, err := r.db.Exec(
		ctx,
		`UPDATE notifications SET read_at = now() WHERE user_id = $1 AND read_at IS NULL`,
		userID,
	)
	if err != nil {
		return 0, fmt.Errorf("notifications.MarkAllRead: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (r *NotificationsRepository) MarkRead(ctx context.Context, userID uuid.UUID, id uuid.UUID) error {
	if userID == uuid.Nil {
		return fmt.Errorf("notifications: user_id must not be empty")
	}
	if id == uuid.Nil {
		return fmt.Errorf("notifications: id must not be empty")
	}

	tag, err := r.db.Exec(
		ctx,
		`UPDATE notifications
		 SET read_at = now()
		 WHERE id = $1 AND user_id = $2 AND read_at IS NULL`,
		id, userID,
	)
	if err != nil {
		return fmt.Errorf("notifications.MarkRead: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// already read or not found — treat as no-op
		return pgx.ErrNoRows
	}
	return nil
}

// BackfillBroadcastsForMembership 为新成员补发加入前已存在的历史广播通知。
func (r *NotificationsRepository) BackfillBroadcastsForMembership(ctx context.Context, userID, accountID uuid.UUID) (int, error) {
	if userID == uuid.Nil {
		return 0, fmt.Errorf("notifications: user_id must not be empty")
	}
	if accountID == uuid.Nil {
		return 0, fmt.Errorf("notifications: account_id must not be empty")
	}
	tag, err := r.db.Exec(
		ctx,
		`INSERT INTO notifications (user_id, account_id, type, title, body, payload_json, broadcast_id)
		 SELECT $1, $2, nb.type, nb.title, nb.body, nb.payload_json, nb.id
		 FROM notification_broadcasts nb
		 WHERE nb.deleted_at IS NULL
		   AND (nb.target_type = 'all' OR (nb.target_type = 'account' AND nb.target_id = $2))
		 ON CONFLICT DO NOTHING`,
		userID, accountID,
	)
	if err != nil {
		return 0, fmt.Errorf("notifications.BackfillBroadcasts: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
