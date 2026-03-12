package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
"arkloop/services/shared/database"
)

type ThreadMode string

const (
	ThreadModeChat ThreadMode = "chat"
	ThreadModeClaw ThreadMode = "claw"
)

func (m ThreadMode) IsValid() bool {
	return m == ThreadModeChat || m == ThreadModeClaw
}

type Thread struct {
	ID              uuid.UUID
	OrgID           uuid.UUID
	CreatedByUserID *uuid.UUID
	Mode            ThreadMode
	Title           *string
	CreatedAt       time.Time
	// R15: 软删除 + Phase 5 project 预留
	DeletedAt *time.Time
	ProjectID *uuid.UUID
	IsPrivate bool
	ExpiresAt *time.Time
	// Fork 溯源
	ParentThreadID        *uuid.UUID
	BranchedFromMessageID *uuid.UUID
	// 用户手动命名后置 true，阻止 Worker 自动标题覆盖
	TitleLocked bool
}

type ThreadWithActiveRun struct {
	Thread
	ActiveRunID *uuid.UUID // nil 表示当前无 running run
}

type ThreadRepository struct {
	db      Querier
	dialect database.DialectHelper
}

func NewThreadRepository(db Querier, dialect ...database.DialectHelper) (*ThreadRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	d := database.DialectHelper(database.PostgresDialect{})
	if len(dialect) > 0 && dialect[0] != nil {
		d = dialect[0]
	}
	return &ThreadRepository{db: db, dialect: d}, nil
}

func escapeILikePattern(input string) string {
	replacer := strings.NewReplacer(
		"!", "!!",
		"%", "!%",
		"_", "!_",
	)
	return replacer.Replace(input)
}

func (r *ThreadRepository) Create(
	ctx context.Context,
	orgID uuid.UUID,
	createdByUserID *uuid.UUID,
	projectID uuid.UUID,
	mode ThreadMode,
	title *string,
	isPrivate bool,
) (Thread, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil {
		return Thread{}, fmt.Errorf("org_id must not be empty")
	}
	if projectID == uuid.Nil {
		return Thread{}, fmt.Errorf("project_id must not be empty")
	}
	if !mode.IsValid() {
		return Thread{}, fmt.Errorf("mode must be chat or claw")
	}

	var thread Thread
	expiresExpr := "CASE WHEN $6 THEN " + r.dialect.IntervalAdd(r.dialect.Now(), "24 hours", "+24 hours") + " ELSE NULL END"
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO threads (org_id, created_by_user_id, project_id, mode, title, is_private, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, `+expiresExpr+`)
		 RETURNING id, org_id, created_by_user_id, mode, title, created_at, deleted_at, project_id, is_private, expires_at, parent_thread_id, branched_from_message_id, title_locked`,
		orgID,
		createdByUserID,
		projectID,
		string(mode),
		title,
		isPrivate,
	).Scan(&thread.ID, &thread.OrgID, &thread.CreatedByUserID, &thread.Mode, &thread.Title, &thread.CreatedAt,
		&thread.DeletedAt, &thread.ProjectID, &thread.IsPrivate, &thread.ExpiresAt,
		&thread.ParentThreadID, &thread.BranchedFromMessageID, &thread.TitleLocked)
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
		`SELECT id, org_id, created_by_user_id, mode, title, created_at, deleted_at, project_id, is_private, expires_at, parent_thread_id, branched_from_message_id, title_locked
		 FROM threads
		 WHERE id = $1
		   AND deleted_at IS NULL
		 LIMIT 1`,
		threadID,
	).Scan(&thread.ID, &thread.OrgID, &thread.CreatedByUserID, &thread.Mode, &thread.Title, &thread.CreatedAt,
		&thread.DeletedAt, &thread.ProjectID, &thread.IsPrivate, &thread.ExpiresAt,
		&thread.ParentThreadID, &thread.BranchedFromMessageID, &thread.TitleLocked)
	if err != nil {
		if errors.Is(err, database.ErrNoRows) {
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
	mode *ThreadMode,
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
	if mode != nil && !mode.IsValid() {
		return nil, fmt.Errorf("mode must be chat or claw")
	}

	sql := `SELECT t.id, t.org_id, t.created_by_user_id, t.mode, t.title, t.created_at,
		       t.deleted_at, t.project_id, t.is_private, t.expires_at,
		       t.parent_thread_id, t.branched_from_message_id, t.title_locked, r.id AS active_run_id
		FROM threads t
		LEFT JOIN (
			SELECT id, thread_id FROM (
				SELECT id, thread_id, ROW_NUMBER() OVER (PARTITION BY thread_id ORDER BY created_at DESC) AS rn
				FROM runs WHERE status = 'running'
			) sub WHERE rn = 1
		) r ON r.thread_id = t.id
		WHERE t.org_id = $1
		  AND t.created_by_user_id = $2
		  AND t.deleted_at IS NULL
		  AND t.is_private = false`
	args := []any{orgID, ownerUserID}
	if mode != nil {
		sql += `
		  AND t.mode = $3`
		args = append(args, string(*mode))
	}

	if beforeCreatedAt != nil && beforeID != nil {
		createdAtArgIdx := len(args) + 1
		idArgIdx := len(args) + 2
		sql += `
		  AND (
		    t.created_at < $` + fmt.Sprintf("%d", createdAtArgIdx) + ` OR (t.created_at = $` + fmt.Sprintf("%d", createdAtArgIdx) + ` AND t.id < $` + fmt.Sprintf("%d", idArgIdx) + `)
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
			&item.ID, &item.OrgID, &item.CreatedByUserID, &item.Mode, &item.Title, &item.CreatedAt,
			&item.DeletedAt, &item.ProjectID, &item.IsPrivate, &item.ExpiresAt,
			&item.ParentThreadID, &item.BranchedFromMessageID, &item.TitleLocked, &item.ActiveRunID,
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
		 RETURNING id, org_id, created_by_user_id, mode, title, created_at, deleted_at, project_id, is_private, expires_at, parent_thread_id, branched_from_message_id, title_locked`,
		title,
		threadID,
	).Scan(&thread.ID, &thread.OrgID, &thread.CreatedByUserID, &thread.Mode, &thread.Title, &thread.CreatedAt,
		&thread.DeletedAt, &thread.ProjectID, &thread.IsPrivate, &thread.ExpiresAt,
		&thread.ParentThreadID, &thread.BranchedFromMessageID, &thread.TitleLocked)
	if err != nil {
		if errors.Is(err, database.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &thread, nil
}

// ThreadUpdateFields 描述 PATCH 操作中要更新的字段集合。
// Set* 为 true 才写对应列，允许单独或同时更新。
type ThreadUpdateFields struct {
	SetTitle       bool
	Title          *string
	SetProjectID   bool
	ProjectID      *uuid.UUID
	SetTitleLocked bool
	TitleLocked    bool
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
	if !params.SetTitle && !params.SetProjectID && !params.SetTitleLocked {
		return nil, fmt.Errorf("no fields to update")
	}

	var thread Thread
	err := r.db.QueryRow(
		ctx,
		`UPDATE threads
		 SET title           = CASE WHEN $2 THEN $3 ELSE title END,
		     project_id      = CASE WHEN $4 THEN $5 ELSE project_id END,
		     title_locked    = CASE WHEN $6 THEN $7 ELSE title_locked END
		 WHERE id = $1
		   AND deleted_at IS NULL
		 RETURNING id, org_id, created_by_user_id, mode, title, created_at, deleted_at, project_id, is_private, expires_at, parent_thread_id, branched_from_message_id, title_locked`,
		threadID,
		params.SetTitle, params.Title,
		params.SetProjectID, params.ProjectID,
		params.SetTitleLocked, params.TitleLocked,
	).Scan(&thread.ID, &thread.OrgID, &thread.CreatedByUserID, &thread.Mode, &thread.Title, &thread.CreatedAt,
		&thread.DeletedAt, &thread.ProjectID, &thread.IsPrivate, &thread.ExpiresAt,
		&thread.ParentThreadID, &thread.BranchedFromMessageID, &thread.TitleLocked)
	if err != nil {
		if errors.Is(err, database.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &thread, nil
}

// UpdateFieldsOwned 原子更新 thread 的一个或多个字段，仅允许 owner 在同 org 内更新。
// 返回 nil 表示 thread 不存在、已删除，或 owner/org 不匹配。
func (r *ThreadRepository) UpdateFieldsOwned(
	ctx context.Context,
	threadID uuid.UUID,
	orgID uuid.UUID,
	ownerUserID uuid.UUID,
	params ThreadUpdateFields,
) (*Thread, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("thread_id must not be empty")
	}
	if orgID == uuid.Nil {
		return nil, fmt.Errorf("org_id must not be empty")
	}
	if ownerUserID == uuid.Nil {
		return nil, fmt.Errorf("owner_user_id must not be empty")
	}
	if !params.SetTitle && !params.SetProjectID && !params.SetTitleLocked {
		return nil, fmt.Errorf("no fields to update")
	}

	var thread Thread
	err := r.db.QueryRow(
		ctx,
		`UPDATE threads
		 SET title           = CASE WHEN $4 THEN $5 ELSE title END,
		     project_id      = CASE WHEN $6 THEN $7 ELSE project_id END,
		     title_locked    = CASE WHEN $8 THEN $9 ELSE title_locked END
		 WHERE id = $1
		   AND org_id = $2
		   AND created_by_user_id = $3
		   AND deleted_at IS NULL
		 RETURNING id, org_id, created_by_user_id, mode, title, created_at, deleted_at, project_id, is_private, expires_at, parent_thread_id, branched_from_message_id, title_locked`,
		threadID,
		orgID,
		ownerUserID,
		params.SetTitle, params.Title,
		params.SetProjectID, params.ProjectID,
		params.SetTitleLocked, params.TitleLocked,
	).Scan(&thread.ID, &thread.OrgID, &thread.CreatedByUserID, &thread.Mode, &thread.Title, &thread.CreatedAt,
		&thread.DeletedAt, &thread.ProjectID, &thread.IsPrivate, &thread.ExpiresAt,
		&thread.ParentThreadID, &thread.BranchedFromMessageID, &thread.TitleLocked)
	if err != nil {
		if errors.Is(err, database.ErrNoRows) {
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

// DeleteOwnedReturning 软删除 thread，仅允许 owner 在同 org 内删除。
// 返回 nil 表示 thread 不存在、已删除，或 owner/org 不匹配。
func (r *ThreadRepository) DeleteOwnedReturning(
	ctx context.Context,
	threadID uuid.UUID,
	orgID uuid.UUID,
	ownerUserID uuid.UUID,
) (*Thread, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("thread_id must not be empty")
	}
	if orgID == uuid.Nil {
		return nil, fmt.Errorf("org_id must not be empty")
	}
	if ownerUserID == uuid.Nil {
		return nil, fmt.Errorf("owner_user_id must not be empty")
	}

	var thread Thread
	err := r.db.QueryRow(
		ctx,
		`UPDATE threads
		 SET deleted_at = now()
		 WHERE id = $1
		   AND org_id = $2
		   AND created_by_user_id = $3
		   AND deleted_at IS NULL
		 RETURNING id, org_id, created_by_user_id, mode, title, created_at, deleted_at, project_id, is_private, expires_at, parent_thread_id, branched_from_message_id, title_locked`,
		threadID,
		orgID,
		ownerUserID,
	).Scan(&thread.ID, &thread.OrgID, &thread.CreatedByUserID, &thread.Mode, &thread.Title, &thread.CreatedAt,
		&thread.DeletedAt, &thread.ProjectID, &thread.IsPrivate, &thread.ExpiresAt,
		&thread.ParentThreadID, &thread.BranchedFromMessageID, &thread.TitleLocked)
	if err != nil {
		if errors.Is(err, database.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &thread, nil
}

// SearchByQuery 在 thread title 和 message content 中全文检索，返回匹配的 thread 列表（去重）。
func (r *ThreadRepository) SearchByQuery(
	ctx context.Context,
	orgID uuid.UUID,
	ownerUserID uuid.UUID,
	mode *ThreadMode,
	query string,
	limit int,
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
	if query == "" {
		return nil, fmt.Errorf("query must not be empty")
	}
	if mode != nil && !mode.IsValid() {
		return nil, fmt.Errorf("mode must be chat or claw")
	}

	like := "%" + escapeILikePattern(query) + "%"
	ilike := r.dialect.ILike()
	modeSQL := ""
	args := []any{orgID, ownerUserID, like, limit}
	if mode != nil {
		modeSQL = "\n\t\t   AND t.mode = $5"
		args = append(args, string(*mode))
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT t.id, t.org_id, t.created_by_user_id, t.mode, t.title, t.created_at,
		        t.deleted_at, t.project_id, t.is_private, t.expires_at,
		        t.parent_thread_id, t.branched_from_message_id, t.title_locked, r.id AS active_run_id
		 FROM threads t
		 LEFT JOIN messages m
		   ON m.thread_id = t.id
		  AND m.deleted_at IS NULL
		  AND m.hidden = FALSE
		 LEFT JOIN (
		   SELECT id, thread_id FROM (
		     SELECT id, thread_id, ROW_NUMBER() OVER (PARTITION BY thread_id ORDER BY created_at DESC) AS rn
		     FROM runs WHERE status = 'running'
		   ) sub WHERE rn = 1
		 ) r ON r.thread_id = t.id
		 WHERE t.org_id = $1
		   AND t.created_by_user_id = $2
		   AND t.deleted_at IS NULL
		   AND t.is_private = false
		`+modeSQL+`
		   AND (
		       t.title `+ilike+` $3 ESCAPE '!'
		    OR m.content `+ilike+` $3 ESCAPE '!'
		   )
		 GROUP BY t.id, t.org_id, t.created_by_user_id, t.mode, t.title, t.created_at,
		          t.deleted_at, t.project_id, t.is_private, t.expires_at,
		          t.parent_thread_id, t.branched_from_message_id, t.title_locked, r.id
		 ORDER BY t.created_at DESC, t.id DESC
		 LIMIT $4`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var threads []ThreadWithActiveRun
	for rows.Next() {
		var item ThreadWithActiveRun
		if err := rows.Scan(
			&item.ID, &item.OrgID, &item.CreatedByUserID, &item.Mode, &item.Title, &item.CreatedAt,
			&item.DeletedAt, &item.ProjectID, &item.IsPrivate, &item.ExpiresAt,
			&item.ParentThreadID, &item.BranchedFromMessageID, &item.TitleLocked, &item.ActiveRunID,
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

// DeleteExpiredPrivate 硬删除所有已过期的私密 thread（is_private=true AND expires_at < now()）。
// messages/runs/run_events 通过 ON DELETE CASCADE 自动清理。
func (r *ThreadRepository) DeleteExpiredPrivate(ctx context.Context) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	tag, err := r.db.Exec(
		ctx,
		`DELETE FROM threads WHERE is_private = true AND expires_at < now()`,
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// Fork 创建一个新 thread，记录其来自 parentThreadID 的 branchFromMessageID 处的分叉。
func (r *ThreadRepository) Fork(
	ctx context.Context,
	orgID uuid.UUID,
	createdByUserID *uuid.UUID,
	parentThreadID uuid.UUID,
	branchFromMessageID uuid.UUID,
	isPrivate bool,
) (Thread, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil {
		return Thread{}, fmt.Errorf("org_id must not be empty")
	}
	if parentThreadID == uuid.Nil {
		return Thread{}, fmt.Errorf("parent_thread_id must not be empty")
	}
	if branchFromMessageID == uuid.Nil {
		return Thread{}, fmt.Errorf("branched_from_message_id must not be empty")
	}

	var thread Thread
	forkExpiresExpr := "CASE WHEN $3 THEN " + r.dialect.IntervalAdd(r.dialect.Now(), "24 hours", "+24 hours") + " ELSE NULL END"
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO threads (org_id, created_by_user_id, project_id, mode, title, is_private, expires_at, parent_thread_id, branched_from_message_id)
		 SELECT $1, $2, project_id, mode, title, $3, `+forkExpiresExpr+`, $4, $5
		 FROM threads WHERE id = $4 AND deleted_at IS NULL
		 RETURNING id, org_id, created_by_user_id, mode, title, created_at, deleted_at, project_id, is_private, expires_at, parent_thread_id, branched_from_message_id, title_locked`,
		orgID,
		createdByUserID,
		isPrivate,
		parentThreadID,
		branchFromMessageID,
	).Scan(&thread.ID, &thread.OrgID, &thread.CreatedByUserID, &thread.Mode, &thread.Title, &thread.CreatedAt,
		&thread.DeletedAt, &thread.ProjectID, &thread.IsPrivate, &thread.ExpiresAt,
		&thread.ParentThreadID, &thread.BranchedFromMessageID, &thread.TitleLocked)
	if err != nil {
		return Thread{}, err
	}
	return thread, nil
}
