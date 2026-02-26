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
	OrgID       uuid.UUID
	Type        string
	Title       string
	Body        string
	PayloadJSON map[string]any
	ReadAt      *time.Time
	CreatedAt   time.Time
}

type NotificationBroadcast struct {
	ID          uuid.UUID
	Type        string
	Title       string
	Body        string
	TargetType  string
	TargetID    *uuid.UUID
	PayloadJSON map[string]any
	Status      string
	SentCount   int
	CreatedBy   uuid.UUID
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
	orgID uuid.UUID,
	notifType string,
	title string,
	body string,
	payloadJSON map[string]any,
) (Notification, error) {
	if userID == uuid.Nil {
		return Notification{}, fmt.Errorf("notifications: user_id must not be empty")
	}
	if orgID == uuid.Nil {
		return Notification{}, fmt.Errorf("notifications: org_id must not be empty")
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
		`INSERT INTO notifications (user_id, org_id, type, title, body, payload_json)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, user_id, org_id, type, title, body, payload_json, read_at, created_at`,
		userID, orgID, notifType, title, body, payloadJSON,
	).Scan(
		&n.ID, &n.UserID, &n.OrgID, &n.Type, &n.Title,
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
		`SELECT n.id, n.user_id, n.org_id, n.type, n.title, n.body, n.payload_json, n.read_at, n.created_at
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
			&n.ID, &n.UserID, &n.OrgID, &n.Type, &n.Title,
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
		`SELECT n.id, n.user_id, n.org_id, n.type, n.title, n.body, n.payload_json, n.read_at, n.created_at
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
			&n.ID, &n.UserID, &n.OrgID, &n.Type, &n.Title,
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

func (r *NotificationsRepository) CreateBroadcast(
	ctx context.Context,
	broadcastType string,
	title string,
	body string,
	targetType string,
	targetID *uuid.UUID,
	payloadJSON map[string]any,
	createdBy uuid.UUID,
) (NotificationBroadcast, error) {
	if broadcastType == "" {
		return NotificationBroadcast{}, fmt.Errorf("broadcasts: type must not be empty")
	}
	if title == "" {
		return NotificationBroadcast{}, fmt.Errorf("broadcasts: title must not be empty")
	}
	if targetType != "all" && targetType != "org" {
		return NotificationBroadcast{}, fmt.Errorf("broadcasts: target_type must be 'all' or 'org'")
	}
	if targetType == "org" && (targetID == nil || *targetID == uuid.Nil) {
		return NotificationBroadcast{}, fmt.Errorf("broadcasts: target_id required when target_type is 'org'")
	}
	if createdBy == uuid.Nil {
		return NotificationBroadcast{}, fmt.Errorf("broadcasts: created_by must not be empty")
	}
	if payloadJSON == nil {
		payloadJSON = map[string]any{}
	}

	var b NotificationBroadcast
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO notification_broadcasts (type, title, body, target_type, target_id, payload_json, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, type, title, body, target_type, target_id, payload_json, status, sent_count, created_by, created_at`,
		broadcastType, title, body, targetType, targetID, payloadJSON, createdBy,
	).Scan(
		&b.ID, &b.Type, &b.Title, &b.Body, &b.TargetType, &b.TargetID,
		&b.PayloadJSON, &b.Status, &b.SentCount, &b.CreatedBy, &b.CreatedAt,
	)
	if err != nil {
		return NotificationBroadcast{}, fmt.Errorf("broadcasts.Create: %w", err)
	}
	return b, nil
}

// BroadcastToAll 将广播展开到所有用户，按 org 分批插入通知。
func (r *NotificationsRepository) BroadcastToAll(ctx context.Context, broadcast NotificationBroadcast) (int, error) {
	tag, err := r.db.Exec(
		ctx,
		`INSERT INTO notifications (user_id, org_id, type, title, body, payload_json, broadcast_id)
		 SELECT m.user_id, m.org_id, $2, $3, $4, $5, $1
		 FROM org_memberships m
		 ON CONFLICT DO NOTHING`,
		broadcast.ID, broadcast.Type, broadcast.Title, broadcast.Body, broadcast.PayloadJSON,
	)
	if err != nil {
		return 0, fmt.Errorf("broadcasts.BroadcastToAll: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// BroadcastToOrg 将广播展开到指定 org 的所有成员。
func (r *NotificationsRepository) BroadcastToOrg(ctx context.Context, broadcast NotificationBroadcast, orgID uuid.UUID) (int, error) {
	if orgID == uuid.Nil {
		return 0, fmt.Errorf("broadcasts: org_id must not be empty")
	}
	tag, err := r.db.Exec(
		ctx,
		`INSERT INTO notifications (user_id, org_id, type, title, body, payload_json, broadcast_id)
		 SELECT m.user_id, m.org_id, $2, $3, $4, $5, $1
		 FROM org_memberships m
		 WHERE m.org_id = $6
		 ON CONFLICT DO NOTHING`,
		broadcast.ID, broadcast.Type, broadcast.Title, broadcast.Body, broadcast.PayloadJSON, orgID,
	)
	if err != nil {
		return 0, fmt.Errorf("broadcasts.BroadcastToOrg: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (r *NotificationsRepository) UpdateBroadcastStatus(ctx context.Context, id uuid.UUID, status string, sentCount int) error {
	if id == uuid.Nil {
		return fmt.Errorf("broadcasts: id must not be empty")
	}
	_, err := r.db.Exec(
		ctx,
		`UPDATE notification_broadcasts SET status = $2, sent_count = $3 WHERE id = $1`,
		id, status, sentCount,
	)
	if err != nil {
		return fmt.Errorf("broadcasts.UpdateStatus: %w", err)
	}
	return nil
}

func (r *NotificationsRepository) GetBroadcast(ctx context.Context, id uuid.UUID) (NotificationBroadcast, error) {
	if id == uuid.Nil {
		return NotificationBroadcast{}, fmt.Errorf("broadcasts: id must not be empty")
	}
	var b NotificationBroadcast
	err := r.db.QueryRow(
		ctx,
		`SELECT id, type, title, body, target_type, target_id, payload_json, status, sent_count, created_by, created_at
		 FROM notification_broadcasts
		 WHERE id = $1 AND deleted_at IS NULL`,
		id,
	).Scan(
		&b.ID, &b.Type, &b.Title, &b.Body, &b.TargetType, &b.TargetID,
		&b.PayloadJSON, &b.Status, &b.SentCount, &b.CreatedBy, &b.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return NotificationBroadcast{}, pgx.ErrNoRows
		}
		return NotificationBroadcast{}, fmt.Errorf("broadcasts.Get: %w", err)
	}
	return b, nil
}

func (r *NotificationsRepository) DeleteBroadcast(ctx context.Context, id uuid.UUID) error {
	if id == uuid.Nil {
		return fmt.Errorf("broadcasts: id must not be empty")
	}
	tag, err := r.db.Exec(
		ctx,
		`UPDATE notification_broadcasts SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`,
		id,
	)
	if err != nil {
		return fmt.Errorf("broadcasts.Delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// BackfillBroadcastsForMembership 为新成员补发加入前已存在的历史广播通知。
func (r *NotificationsRepository) BackfillBroadcastsForMembership(ctx context.Context, userID, orgID uuid.UUID) (int, error) {
	if userID == uuid.Nil {
		return 0, fmt.Errorf("notifications: user_id must not be empty")
	}
	if orgID == uuid.Nil {
		return 0, fmt.Errorf("notifications: org_id must not be empty")
	}
	tag, err := r.db.Exec(
		ctx,
		`INSERT INTO notifications (user_id, org_id, type, title, body, payload_json, broadcast_id)
		 SELECT $1, $2, nb.type, nb.title, nb.body, nb.payload_json, nb.id
		 FROM notification_broadcasts nb
		 WHERE nb.deleted_at IS NULL
		   AND (nb.target_type = 'all' OR (nb.target_type = 'org' AND nb.target_id = $2))
		 ON CONFLICT DO NOTHING`,
		userID, orgID,
	)
	if err != nil {
		return 0, fmt.Errorf("notifications.BackfillBroadcasts: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (r *NotificationsRepository) ListBroadcasts(
	ctx context.Context,
	limit int,
	beforeCreatedAt *time.Time,
	beforeID *uuid.UUID,
) ([]NotificationBroadcast, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	var (
		rows pgx.Rows
		err  error
	)

	if beforeCreatedAt != nil && beforeID != nil {
		rows, err = r.db.Query(
			ctx,
			`SELECT id, type, title, body, target_type, target_id, payload_json, status, sent_count, created_by, created_at
			 FROM notification_broadcasts
			 WHERE deleted_at IS NULL AND (created_at, id) < ($2, $3)
			 ORDER BY created_at DESC, id DESC
			 LIMIT $1`,
			limit, beforeCreatedAt, beforeID,
		)
	} else {
		rows, err = r.db.Query(
			ctx,
			`SELECT id, type, title, body, target_type, target_id, payload_json, status, sent_count, created_by, created_at
			 FROM notification_broadcasts
			 WHERE deleted_at IS NULL
			 ORDER BY created_at DESC, id DESC
			 LIMIT $1`,
			limit,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("broadcasts.List: %w", err)
	}
	defer rows.Close()

	var results []NotificationBroadcast
	for rows.Next() {
		var b NotificationBroadcast
		if err := rows.Scan(
			&b.ID, &b.Type, &b.Title, &b.Body, &b.TargetType, &b.TargetID,
			&b.PayloadJSON, &b.Status, &b.SentCount, &b.CreatedBy, &b.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("broadcasts.List scan: %w", err)
		}
		results = append(results, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("broadcasts.List rows: %w", err)
	}
	return results, nil
}
