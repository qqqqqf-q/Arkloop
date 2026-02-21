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

type ThreadWithActiveRun struct {
	Thread
	ActiveRunID *uuid.UUID // nil 表示当前无 running run
}

type ThreadRepository struct {
	db Querier
}

func NewThreadRepository(db Querier) (*ThreadRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
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
		return Thread{}, fmt.Errorf("org_id must not be empty")
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
) ([]ThreadWithActiveRun, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil {
		return nil, fmt.Errorf("org_id must not be empty")
	}
	if ownerUserID == uuid.Nil {
		return nil, fmt.Errorf("owner_user_id must not be empty")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be positive")
	}
	if (beforeCreatedAt == nil) != (beforeID == nil) {
		return nil, fmt.Errorf("before_created_at and before_id must be provided together")
	}

	sql := `SELECT t.id, t.org_id, t.created_by_user_id, t.title, t.created_at, r.id AS active_run_id
		FROM threads t
		LEFT JOIN LATERAL (
			SELECT id FROM runs
			WHERE thread_id = t.id AND status = 'running'
			ORDER BY created_at DESC
			LIMIT 1
		) r ON true
		WHERE t.org_id = $1
		  AND t.created_by_user_id = $2`
	args := []any{orgID, ownerUserID}

	if beforeCreatedAt != nil && beforeID != nil {
		sql += `
		  AND (
		    t.created_at < $3 OR (t.created_at = $3 AND t.id < $4)
		  )`
		args = append(args, beforeCreatedAt.UTC(), *beforeID)
	}

	sql += `
		ORDER BY t.created_at DESC, t.id DESC
		LIMIT $` + fmt.Sprintf("%d", len(args)+1)
	args = append(args, limit)

	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var threads []ThreadWithActiveRun
	for rows.Next() {
		var item ThreadWithActiveRun
		if err := rows.Scan(
			&item.ID, &item.OrgID, &item.CreatedByUserID, &item.Title, &item.CreatedAt,
			&item.ActiveRunID,
		); err != nil {
			return nil, err
		}
		threads = append(threads, item)
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
		return nil, fmt.Errorf("thread_id must not be empty")
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
