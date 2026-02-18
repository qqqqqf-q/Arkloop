package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Thread struct {
	ID              uuid.UUID
	OrgID           uuid.UUID
	CreatedByUserID *uuid.UUID
	Title           *string
	CreatedAt       time.Time
}

type ThreadRepository struct {
	db Querier
}

func NewThreadRepository(db Querier) (*ThreadRepository, error) {
	if db == nil {
		return nil, errors.New("db 不能为空")
	}
	return &ThreadRepository{db: db}, nil
}

func (r *ThreadRepository) Create(
	ctx context.Context,
	orgID uuid.UUID,
	createdByUserID *uuid.UUID,
	title *string,
) (Thread, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil {
		return Thread{}, fmt.Errorf("org_id 不能为空")
	}

	var thread Thread
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO threads (org_id, created_by_user_id, title)
		 VALUES ($1, $2, $3)
		 RETURNING id, org_id, created_by_user_id, title, created_at`,
		orgID,
		createdByUserID,
		title,
	).Scan(&thread.ID, &thread.OrgID, &thread.CreatedByUserID, &thread.Title, &thread.CreatedAt)
	if err != nil {
		return Thread{}, err
	}
	return thread, nil
}

func (r *ThreadRepository) GetByID(ctx context.Context, threadID uuid.UUID) (*Thread, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var thread Thread
	err := r.db.QueryRow(
		ctx,
		`SELECT id, org_id, created_by_user_id, title, created_at
		 FROM threads
		 WHERE id = $1
		 LIMIT 1`,
		threadID,
	).Scan(&thread.ID, &thread.OrgID, &thread.CreatedByUserID, &thread.Title, &thread.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &thread, nil
}

func (r *ThreadRepository) ListByOwner(
	ctx context.Context,
	orgID uuid.UUID,
	ownerUserID uuid.UUID,
	limit int,
	beforeCreatedAt *time.Time,
	beforeID *uuid.UUID,
) ([]Thread, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil {
		return nil, fmt.Errorf("org_id 不能为空")
	}
	if ownerUserID == uuid.Nil {
		return nil, fmt.Errorf("owner_user_id 不能为空")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("limit 必须为正数")
	}
	if (beforeCreatedAt == nil) != (beforeID == nil) {
		return nil, fmt.Errorf("before_created_at 与 before_id 需要同时提供")
	}

	sql := `SELECT id, org_id, created_by_user_id, title, created_at
		FROM threads
		WHERE org_id = $1
		  AND created_by_user_id = $2`
	args := []any{orgID, ownerUserID}

	if beforeCreatedAt != nil && beforeID != nil {
		sql += `
		  AND (
		    created_at < $3 OR (created_at = $3 AND id < $4)
		  )`
		args = append(args, beforeCreatedAt.UTC(), *beforeID)
	}

	sql += `
		ORDER BY created_at DESC, id DESC
		LIMIT $` + fmt.Sprintf("%d", len(args)+1)
	args = append(args, limit)

	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var threads []Thread
	for rows.Next() {
		var thread Thread
		if err := rows.Scan(&thread.ID, &thread.OrgID, &thread.CreatedByUserID, &thread.Title, &thread.CreatedAt); err != nil {
			return nil, err
		}
		threads = append(threads, thread)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return threads, nil
}

func (r *ThreadRepository) UpdateTitle(ctx context.Context, threadID uuid.UUID, title *string) (*Thread, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("thread_id 不能为空")
	}

	var thread Thread
	err := r.db.QueryRow(
		ctx,
		`UPDATE threads
		 SET title = $1
		 WHERE id = $2
		 RETURNING id, org_id, created_by_user_id, title, created_at`,
		title,
		threadID,
	).Scan(&thread.ID, &thread.OrgID, &thread.CreatedByUserID, &thread.Title, &thread.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &thread, nil
}
