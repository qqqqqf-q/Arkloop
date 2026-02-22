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
	// R15: 软删除 + Phase 5 project 预留
	DeletedAt *time.Time
	ProjectID *uuid.UUID
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
		 RETURNING id, org_id, created_by_user_id, title, created_at, deleted_at, project_id`,
		orgID,
		createdByUserID,
		title,
	).Scan(&thread.ID, &thread.OrgID, &thread.CreatedByUserID, &thread.Title, &thread.CreatedAt,
		&thread.DeletedAt, &thread.ProjectID)
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
		`SELECT id, org_id, created_by_user_id, title, created_at, deleted_at, project_id
		 FROM threads
		 WHERE id = $1
		   AND deleted_at IS NULL
		 LIMIT 1`,
		threadID,
	).Scan(&thread.ID, &thread.OrgID, &thread.CreatedByUserID, &thread.Title, &thread.CreatedAt,
		&thread.DeletedAt, &thread.ProjectID)
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

	sql := `SELECT t.id, t.org_id, t.created_by_user_id, t.title, t.created_at,
		       t.deleted_at, t.project_id, r.id AS active_run_id
		FROM threads t
		LEFT JOIN LATERAL (
			SELECT id FROM runs
			WHERE thread_id = t.id AND status = 'running'
			ORDER BY created_at DESC
			LIMIT 1
		) r ON true
		WHERE t.org_id = $1
		  AND t.created_by_user_id = $2
		  AND t.deleted_at IS NULL`
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
			&item.DeletedAt, &item.ProjectID, &item.ActiveRunID,
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
		   AND deleted_at IS NULL
		 RETURNING id, org_id, created_by_user_id, title, created_at, deleted_at, project_id`,
		title,
		threadID,
	).Scan(&thread.ID, &thread.OrgID, &thread.CreatedByUserID, &thread.Title, &thread.CreatedAt,
		&thread.DeletedAt, &thread.ProjectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &thread, nil
}

// ThreadUpdateFields 描述 PATCH 操作中要更新的字段集合。
// SetTitle/SetProjectID 为 true 才写对应列，允许单独或同时更新。
type ThreadUpdateFields struct {
	SetTitle     bool
	Title        *string
	SetProjectID bool
	ProjectID    *uuid.UUID
}

// UpdateFields 原子更新 thread 的一个或多个字段，单条 SQL 保证原子性。
// 返回 nil 表示 thread 不存在或已删除。
func (r *ThreadRepository) UpdateFields(ctx context.Context, threadID uuid.UUID, params ThreadUpdateFields) (*Thread, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("thread_id must not be empty")
	}
	if !params.SetTitle && !params.SetProjectID {
		return nil, fmt.Errorf("no fields to update")
	}

	var thread Thread
	err := r.db.QueryRow(
		ctx,
		`UPDATE threads
		 SET title      = CASE WHEN $2 THEN $3 ELSE title END,
		     project_id = CASE WHEN $4 THEN $5 ELSE project_id END
		 WHERE id = $1
		   AND deleted_at IS NULL
		 RETURNING id, org_id, created_by_user_id, title, created_at, deleted_at, project_id`,
		threadID,
		params.SetTitle, params.Title,
		params.SetProjectID, params.ProjectID,
	).Scan(&thread.ID, &thread.OrgID, &thread.CreatedByUserID, &thread.Title, &thread.CreatedAt,
		&thread.DeletedAt, &thread.ProjectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &thread, nil
}

// Delete 软删除 thread，返回 false 表示 thread 不存在或已删除。
func (r *ThreadRepository) Delete(ctx context.Context, threadID uuid.UUID) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if threadID == uuid.Nil {
		return false, fmt.Errorf("thread_id must not be empty")
	}

	tag, err := r.db.Exec(
		ctx,
		`UPDATE threads SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`,
		threadID,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
