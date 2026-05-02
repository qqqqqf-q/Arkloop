package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Thread struct {
	ID              uuid.UUID
	AccountID       uuid.UUID
	CreatedByUserID *uuid.UUID
	Title           *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	// R15: 软删除 + Phase 5 project 预留
	DeletedAt                 *time.Time
	ProjectID                 *uuid.UUID
	IsPrivate                 bool
	Mode                      string
	CollaborationMode         string
	CollaborationModeRevision int64
	LearningModeEnabled       bool
	SidebarWorkFolder         *string
	SidebarPinnedAt           *time.Time
	SidebarGtdBucket          *string
	ExpiresAt                 *time.Time
	// Fork 溯源
	ParentThreadID        *uuid.UUID
	BranchedFromMessageID *uuid.UUID
	// 用户手动命名后置 true，阻止 Worker 自动标题覆盖
	TitleLocked bool
}

const (
	ThreadModeChat                 = "chat"
	ThreadModeWork                 = "work"
	ThreadCollaborationModeDefault = "default"
	ThreadCollaborationModePlan    = "plan"
	ThreadGtdBucketInbox           = "inbox"
	ThreadGtdBucketTodo            = "todo"
	ThreadGtdBucketWaiting         = "waiting"
	ThreadGtdBucketSomeday         = "someday"
	ThreadGtdBucketArchived        = "archived"
)

func NormalizeThreadMode(value string) (string, bool) {
	switch strings.TrimSpace(value) {
	case "", ThreadModeChat:
		return ThreadModeChat, true
	case ThreadModeWork:
		return ThreadModeWork, true
	default:
		return "", false
	}
}

func NormalizeThreadCollaborationMode(value string) (string, bool) {
	switch strings.TrimSpace(value) {
	case "", ThreadCollaborationModeDefault:
		return ThreadCollaborationModeDefault, true
	case ThreadCollaborationModePlan:
		return ThreadCollaborationModePlan, true
	default:
		return "", false
	}
}

func NormalizeThreadGtdBucket(value string) (string, bool) {
	switch strings.TrimSpace(value) {
	case ThreadGtdBucketInbox:
		return ThreadGtdBucketInbox, true
	case ThreadGtdBucketTodo:
		return ThreadGtdBucketTodo, true
	case ThreadGtdBucketWaiting:
		return ThreadGtdBucketWaiting, true
	case ThreadGtdBucketSomeday:
		return ThreadGtdBucketSomeday, true
	case ThreadGtdBucketArchived:
		return ThreadGtdBucketArchived, true
	default:
		return "", false
	}
}

type ThreadWithActiveRun struct {
	Thread
	ActiveRunID *uuid.UUID // nil 表示当前无 running run
}

type ThreadRepository struct {
	db Querier
}

func (r *ThreadRepository) WithTx(tx pgx.Tx) *ThreadRepository {
	return &ThreadRepository{db: tx}
}

func NewThreadRepository(db Querier) (*ThreadRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &ThreadRepository{db: db}, nil
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
	accountID uuid.UUID,
	createdByUserID *uuid.UUID,
	projectID uuid.UUID,
	title *string,
	isPrivate bool,
) (Thread, error) {
	return r.CreateWithMode(ctx, accountID, createdByUserID, projectID, title, isPrivate, ThreadModeChat)
}

func (r *ThreadRepository) CreateWithMode(
	ctx context.Context,
	accountID uuid.UUID,
	createdByUserID *uuid.UUID,
	projectID uuid.UUID,
	title *string,
	isPrivate bool,
	mode string,
) (Thread, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return Thread{}, fmt.Errorf("account_id must not be empty")
	}
	if projectID == uuid.Nil {
		return Thread{}, fmt.Errorf("project_id must not be empty")
	}
	normalizedMode, ok := NormalizeThreadMode(mode)
	if !ok {
		return Thread{}, fmt.Errorf("invalid mode")
	}

	var thread Thread
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO threads (account_id, created_by_user_id, project_id, title, is_private, mode, expires_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, CASE WHEN $5 THEN now() + INTERVAL '24 hours' ELSE NULL END, now())
		 RETURNING id, account_id, created_by_user_id, title, created_at, updated_at, deleted_at, project_id, is_private, mode, collaboration_mode, collaboration_mode_revision, learning_mode_enabled, sidebar_work_folder, sidebar_pinned_at, sidebar_gtd_bucket, expires_at, parent_thread_id, branched_from_message_id, title_locked`,
		accountID,
		createdByUserID,
		projectID,
		title,
		isPrivate,
		normalizedMode,
	).Scan(&thread.ID, &thread.AccountID, &thread.CreatedByUserID, &thread.Title, &thread.CreatedAt, &thread.UpdatedAt,
		&thread.DeletedAt, &thread.ProjectID, &thread.IsPrivate, &thread.Mode, &thread.CollaborationMode, &thread.CollaborationModeRevision, &thread.LearningModeEnabled, &thread.SidebarWorkFolder, &thread.SidebarPinnedAt, &thread.SidebarGtdBucket, &thread.ExpiresAt,
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
		`SELECT id, account_id, created_by_user_id, title, created_at, updated_at, deleted_at, project_id, is_private, mode, collaboration_mode, collaboration_mode_revision, learning_mode_enabled, sidebar_work_folder, sidebar_pinned_at, sidebar_gtd_bucket, expires_at, parent_thread_id, branched_from_message_id, title_locked
		 FROM threads
		 WHERE id = $1
		   AND deleted_at IS NULL
		 LIMIT 1`,
		threadID,
	).Scan(&thread.ID, &thread.AccountID, &thread.CreatedByUserID, &thread.Title, &thread.CreatedAt, &thread.UpdatedAt,
		&thread.DeletedAt, &thread.ProjectID, &thread.IsPrivate, &thread.Mode, &thread.CollaborationMode, &thread.CollaborationModeRevision, &thread.LearningModeEnabled, &thread.SidebarWorkFolder, &thread.SidebarPinnedAt, &thread.SidebarGtdBucket, &thread.ExpiresAt,
		&thread.ParentThreadID, &thread.BranchedFromMessageID, &thread.TitleLocked)
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
	accountID uuid.UUID,
	ownerUserID uuid.UUID,
	limit int,
	beforeUpdatedAt *time.Time,
	beforeID *uuid.UUID,
) ([]ThreadWithActiveRun, error) {
	return r.ListByOwnerWithMode(ctx, accountID, ownerUserID, limit, beforeUpdatedAt, beforeID, "")
}

func (r *ThreadRepository) ListByOwnerWithMode(
	ctx context.Context,
	accountID uuid.UUID,
	ownerUserID uuid.UUID,
	limit int,
	beforeUpdatedAt *time.Time,
	beforeID *uuid.UUID,
	modeFilter string,
) ([]ThreadWithActiveRun, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return nil, fmt.Errorf("account_id must not be empty")
	}
	if ownerUserID == uuid.Nil {
		return nil, fmt.Errorf("owner_user_id must not be empty")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be positive")
	}
	if (beforeUpdatedAt == nil) != (beforeID == nil) {
		return nil, fmt.Errorf("before_updated_at and before_id must be provided together")
	}
	normalizedMode := ""
	if strings.TrimSpace(modeFilter) != "" {
		var ok bool
		normalizedMode, ok = NormalizeThreadMode(modeFilter)
		if !ok {
			return nil, fmt.Errorf("invalid mode")
		}
	}

	sql := `SELECT t.id, t.account_id, t.created_by_user_id, t.title, t.created_at, t.updated_at,
		       t.deleted_at, t.project_id, t.is_private, t.mode, t.collaboration_mode, t.collaboration_mode_revision, t.learning_mode_enabled, t.sidebar_work_folder, t.sidebar_pinned_at, t.sidebar_gtd_bucket, t.expires_at,
		       t.parent_thread_id, t.branched_from_message_id, t.title_locked, r.id AS active_run_id
		FROM threads t
		LEFT JOIN LATERAL (
			SELECT rr.id FROM runs rr
			WHERE rr.thread_id = t.id
			  AND rr.status IN ('running', 'cancelling')
			  AND rr.deleted_at IS NULL
			  AND NOT EXISTS (
			    SELECT 1
			    FROM run_events re
			    WHERE re.run_id = rr.id
			      AND re.type IN ('run.completed', 'run.failed', 'run.cancelled', 'run.interrupted')
			  )
			ORDER BY rr.created_at DESC, rr.id DESC
			LIMIT 1
		) r ON true
		WHERE t.account_id = $1
		  AND t.created_by_user_id = $2
		  AND t.deleted_at IS NULL
		  AND t.is_private = false`
	args := []any{accountID, ownerUserID}

	if normalizedMode != "" {
		args = append(args, normalizedMode)
		sql += `
		  AND t.mode = $` + fmt.Sprintf("%d", len(args))
	}

	if beforeUpdatedAt != nil && beforeID != nil {
		sql += `
		  AND (
		    (t.updated_at, t.id) < ($` + fmt.Sprintf("%d", len(args)+1) + `, $` + fmt.Sprintf("%d", len(args)+2) + `)
		  )`
		args = append(args, beforeUpdatedAt.UTC(), *beforeID)
	}

	sql += `
		ORDER BY t.updated_at DESC, t.id DESC
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
			&item.ID, &item.AccountID, &item.CreatedByUserID, &item.Title, &item.CreatedAt, &item.UpdatedAt,
			&item.DeletedAt, &item.ProjectID, &item.IsPrivate, &item.Mode, &item.CollaborationMode, &item.CollaborationModeRevision, &item.LearningModeEnabled, &item.SidebarWorkFolder, &item.SidebarPinnedAt, &item.SidebarGtdBucket, &item.ExpiresAt,
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

func (r *ThreadRepository) Touch(ctx context.Context, threadID uuid.UUID) error {
	_, err := r.db.Exec(ctx,
		`UPDATE threads SET updated_at = now() WHERE id = $1 AND deleted_at IS NULL`,
		threadID,
	)
	return err
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
		 SET title = $1,
		     updated_at = now()
		 WHERE id = $2
		   AND deleted_at IS NULL
		 RETURNING id, account_id, created_by_user_id, title, created_at, updated_at, deleted_at, project_id, is_private, mode, collaboration_mode, collaboration_mode_revision, learning_mode_enabled, sidebar_work_folder, sidebar_pinned_at, sidebar_gtd_bucket, expires_at, parent_thread_id, branched_from_message_id, title_locked`,
		title,
		threadID,
	).Scan(&thread.ID, &thread.AccountID, &thread.CreatedByUserID, &thread.Title, &thread.CreatedAt, &thread.UpdatedAt,
		&thread.DeletedAt, &thread.ProjectID, &thread.IsPrivate, &thread.Mode, &thread.CollaborationMode, &thread.CollaborationModeRevision, &thread.LearningModeEnabled, &thread.SidebarWorkFolder, &thread.SidebarPinnedAt, &thread.SidebarGtdBucket, &thread.ExpiresAt,
		&thread.ParentThreadID, &thread.BranchedFromMessageID, &thread.TitleLocked)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &thread, nil
}

func (r *ThreadRepository) UpdateOwner(ctx context.Context, threadID uuid.UUID, ownerUserID *uuid.UUID) (*Thread, error) {
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
		 SET created_by_user_id = $2
		 WHERE id = $1
		   AND deleted_at IS NULL
		 RETURNING id, account_id, created_by_user_id, title, created_at, updated_at, deleted_at, project_id, is_private, mode, collaboration_mode, collaboration_mode_revision, learning_mode_enabled, sidebar_work_folder, sidebar_pinned_at, sidebar_gtd_bucket, expires_at, parent_thread_id, branched_from_message_id, title_locked`,
		threadID,
		ownerUserID,
	).Scan(&thread.ID, &thread.AccountID, &thread.CreatedByUserID, &thread.Title, &thread.CreatedAt, &thread.UpdatedAt,
		&thread.DeletedAt, &thread.ProjectID, &thread.IsPrivate, &thread.Mode, &thread.CollaborationMode, &thread.CollaborationModeRevision, &thread.LearningModeEnabled, &thread.SidebarWorkFolder, &thread.SidebarPinnedAt, &thread.SidebarGtdBucket, &thread.ExpiresAt,
		&thread.ParentThreadID, &thread.BranchedFromMessageID, &thread.TitleLocked)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &thread, nil
}

// ThreadUpdateFields 描述 PATCH 操作中要更新的字段集合。
// Set* 为 true 才写对应列，允许单独或同时更新。
type ThreadUpdateFields struct {
	SetTitle               bool
	Title                  *string
	SetProjectID           bool
	ProjectID              *uuid.UUID
	SetTitleLocked         bool
	TitleLocked            bool
	SetCollaborationMode   bool
	CollaborationMode      string
	SetLearningModeEnabled bool
	LearningModeEnabled    bool
	SetMode                bool
	Mode                   string
	SetSidebarWorkFolder   bool
	SidebarWorkFolder      *string
	SetSidebarPinnedAt     bool
	SidebarPinnedAt        *time.Time
	SetSidebarGtdBucket    bool
	SidebarGtdBucket       *string
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
	if !params.SetTitle && !params.SetProjectID && !params.SetTitleLocked && !params.SetCollaborationMode && !params.SetLearningModeEnabled && !params.SetMode && !params.SetSidebarWorkFolder && !params.SetSidebarPinnedAt && !params.SetSidebarGtdBucket {
		return nil, fmt.Errorf("no fields to update")
	}
	if params.SetCollaborationMode {
		normalized, ok := NormalizeThreadCollaborationMode(params.CollaborationMode)
		if !ok {
			return nil, fmt.Errorf("invalid collaboration_mode")
		}
		params.CollaborationMode = normalized
	}
	if params.SetMode {
		normalized, ok := NormalizeThreadMode(params.Mode)
		if !ok {
			return nil, fmt.Errorf("invalid mode")
		}
		params.Mode = normalized
	}
	if params.SetSidebarGtdBucket && params.SidebarGtdBucket != nil {
		normalized, ok := NormalizeThreadGtdBucket(*params.SidebarGtdBucket)
		if !ok {
			return nil, fmt.Errorf("invalid sidebar_gtd_bucket")
		}
		params.SidebarGtdBucket = &normalized
	}

	var thread Thread
	err := r.db.QueryRow(
		ctx,
		`UPDATE threads
		 SET title           = CASE WHEN $2 THEN $3 ELSE title END,
		     project_id      = CASE WHEN $4 THEN $5 ELSE project_id END,
		     title_locked    = CASE WHEN $6 THEN $7 ELSE title_locked END,
		     collaboration_mode = CASE WHEN $8 THEN $9 ELSE collaboration_mode END,
		     collaboration_mode_revision = CASE WHEN $8 AND collaboration_mode <> $9 THEN collaboration_mode_revision + 1 ELSE collaboration_mode_revision END,
		     learning_mode_enabled = CASE WHEN $10 THEN $11 ELSE learning_mode_enabled END,
		     mode            = CASE WHEN $12 THEN $13 ELSE mode END,
		     sidebar_work_folder = CASE WHEN $14 THEN $15 ELSE sidebar_work_folder END,
		     sidebar_pinned_at = CASE WHEN $16 THEN $17 ELSE sidebar_pinned_at END,
		     sidebar_gtd_bucket = CASE WHEN $18 THEN $19 ELSE sidebar_gtd_bucket END,
		     updated_at      = CASE WHEN $2 OR ($8 AND collaboration_mode <> $9) OR ($10 AND learning_mode_enabled <> $11) THEN now() ELSE updated_at END
		 WHERE id = $1
		   AND deleted_at IS NULL
		 RETURNING id, account_id, created_by_user_id, title, created_at, updated_at, deleted_at, project_id, is_private, mode, collaboration_mode, collaboration_mode_revision, learning_mode_enabled, sidebar_work_folder, sidebar_pinned_at, sidebar_gtd_bucket, expires_at, parent_thread_id, branched_from_message_id, title_locked`,
		threadID,
		params.SetTitle, params.Title,
		params.SetProjectID, params.ProjectID,
		params.SetTitleLocked, params.TitleLocked,
		params.SetCollaborationMode, params.CollaborationMode,
		params.SetLearningModeEnabled, params.LearningModeEnabled,
		params.SetMode, params.Mode,
		params.SetSidebarWorkFolder, params.SidebarWorkFolder,
		params.SetSidebarPinnedAt, params.SidebarPinnedAt,
		params.SetSidebarGtdBucket, params.SidebarGtdBucket,
	).Scan(&thread.ID, &thread.AccountID, &thread.CreatedByUserID, &thread.Title, &thread.CreatedAt, &thread.UpdatedAt,
		&thread.DeletedAt, &thread.ProjectID, &thread.IsPrivate, &thread.Mode, &thread.CollaborationMode, &thread.CollaborationModeRevision, &thread.LearningModeEnabled, &thread.SidebarWorkFolder, &thread.SidebarPinnedAt, &thread.SidebarGtdBucket, &thread.ExpiresAt,
		&thread.ParentThreadID, &thread.BranchedFromMessageID, &thread.TitleLocked)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &thread, nil
}

// UpdateFieldsOwned 原子更新 thread 的一个或多个字段，仅允许 owner 在同 account 内更新。
// 返回 nil 表示 thread 不存在、已删除，或 owner/account 不匹配。
func (r *ThreadRepository) UpdateFieldsOwned(
	ctx context.Context,
	threadID uuid.UUID,
	accountID uuid.UUID,
	ownerUserID uuid.UUID,
	params ThreadUpdateFields,
) (*Thread, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("thread_id must not be empty")
	}
	if accountID == uuid.Nil {
		return nil, fmt.Errorf("account_id must not be empty")
	}
	if ownerUserID == uuid.Nil {
		return nil, fmt.Errorf("owner_user_id must not be empty")
	}
	if !params.SetTitle && !params.SetProjectID && !params.SetTitleLocked && !params.SetCollaborationMode && !params.SetLearningModeEnabled && !params.SetMode && !params.SetSidebarWorkFolder && !params.SetSidebarPinnedAt && !params.SetSidebarGtdBucket {
		return nil, fmt.Errorf("no fields to update")
	}
	if params.SetCollaborationMode {
		normalized, ok := NormalizeThreadCollaborationMode(params.CollaborationMode)
		if !ok {
			return nil, fmt.Errorf("invalid collaboration_mode")
		}
		params.CollaborationMode = normalized
	}
	if params.SetMode {
		normalized, ok := NormalizeThreadMode(params.Mode)
		if !ok {
			return nil, fmt.Errorf("invalid mode")
		}
		params.Mode = normalized
	}
	if params.SetSidebarGtdBucket && params.SidebarGtdBucket != nil {
		normalized, ok := NormalizeThreadGtdBucket(*params.SidebarGtdBucket)
		if !ok {
			return nil, fmt.Errorf("invalid sidebar_gtd_bucket")
		}
		params.SidebarGtdBucket = &normalized
	}

	var thread Thread
	err := r.db.QueryRow(
		ctx,
		`UPDATE threads
		 SET title           = CASE WHEN $4 THEN $5 ELSE title END,
		     project_id      = CASE WHEN $6 THEN $7 ELSE project_id END,
		     title_locked    = CASE WHEN $8 THEN $9 ELSE title_locked END,
		     collaboration_mode = CASE WHEN $10 THEN $11 ELSE collaboration_mode END,
		     collaboration_mode_revision = CASE WHEN $10 AND collaboration_mode <> $11 THEN collaboration_mode_revision + 1 ELSE collaboration_mode_revision END,
		     learning_mode_enabled = CASE WHEN $12 THEN $13 ELSE learning_mode_enabled END,
		     mode            = CASE WHEN $14 THEN $15 ELSE mode END,
		     sidebar_work_folder = CASE WHEN $16 THEN $17 ELSE sidebar_work_folder END,
		     sidebar_pinned_at = CASE WHEN $18 THEN $19 ELSE sidebar_pinned_at END,
		     sidebar_gtd_bucket = CASE WHEN $20 THEN $21 ELSE sidebar_gtd_bucket END,
		     updated_at      = CASE WHEN $4 OR ($10 AND collaboration_mode <> $11) OR ($12 AND learning_mode_enabled <> $13) THEN now() ELSE updated_at END
		 WHERE id = $1
		   AND account_id = $2
		   AND created_by_user_id = $3
		   AND deleted_at IS NULL
		 RETURNING id, account_id, created_by_user_id, title, created_at, updated_at, deleted_at, project_id, is_private, mode, collaboration_mode, collaboration_mode_revision, learning_mode_enabled, sidebar_work_folder, sidebar_pinned_at, sidebar_gtd_bucket, expires_at, parent_thread_id, branched_from_message_id, title_locked`,
		threadID,
		accountID,
		ownerUserID,
		params.SetTitle, params.Title,
		params.SetProjectID, params.ProjectID,
		params.SetTitleLocked, params.TitleLocked,
		params.SetCollaborationMode, params.CollaborationMode,
		params.SetLearningModeEnabled, params.LearningModeEnabled,
		params.SetMode, params.Mode,
		params.SetSidebarWorkFolder, params.SidebarWorkFolder,
		params.SetSidebarPinnedAt, params.SidebarPinnedAt,
		params.SetSidebarGtdBucket, params.SidebarGtdBucket,
	).Scan(&thread.ID, &thread.AccountID, &thread.CreatedByUserID, &thread.Title, &thread.CreatedAt, &thread.UpdatedAt,
		&thread.DeletedAt, &thread.ProjectID, &thread.IsPrivate, &thread.Mode, &thread.CollaborationMode, &thread.CollaborationModeRevision, &thread.LearningModeEnabled, &thread.SidebarWorkFolder, &thread.SidebarPinnedAt, &thread.SidebarGtdBucket, &thread.ExpiresAt,
		&thread.ParentThreadID, &thread.BranchedFromMessageID, &thread.TitleLocked)
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

// DeleteOwnedReturning 软删除 thread，仅允许 owner 在同 account 内删除。
// 返回 nil 表示 thread 不存在、已删除，或 owner/account 不匹配。
func (r *ThreadRepository) DeleteOwnedReturning(
	ctx context.Context,
	threadID uuid.UUID,
	accountID uuid.UUID,
	ownerUserID uuid.UUID,
) (*Thread, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("thread_id must not be empty")
	}
	if accountID == uuid.Nil {
		return nil, fmt.Errorf("account_id must not be empty")
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
		   AND account_id = $2
		   AND created_by_user_id = $3
		   AND deleted_at IS NULL
		 RETURNING id, account_id, created_by_user_id, title, created_at, updated_at, deleted_at, project_id, is_private, mode, collaboration_mode, collaboration_mode_revision, learning_mode_enabled, sidebar_work_folder, sidebar_pinned_at, sidebar_gtd_bucket, expires_at, parent_thread_id, branched_from_message_id, title_locked`,
		threadID,
		accountID,
		ownerUserID,
	).Scan(&thread.ID, &thread.AccountID, &thread.CreatedByUserID, &thread.Title, &thread.CreatedAt, &thread.UpdatedAt,
		&thread.DeletedAt, &thread.ProjectID, &thread.IsPrivate, &thread.Mode, &thread.CollaborationMode, &thread.CollaborationModeRevision, &thread.LearningModeEnabled, &thread.SidebarWorkFolder, &thread.SidebarPinnedAt, &thread.SidebarGtdBucket, &thread.ExpiresAt,
		&thread.ParentThreadID, &thread.BranchedFromMessageID, &thread.TitleLocked)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &thread, nil
}

// SearchByQuery 在 thread title 和 message content 中全文检索，返回匹配的 thread 列表（去重）。
func (r *ThreadRepository) SearchByQuery(
	ctx context.Context,
	accountID uuid.UUID,
	ownerUserID uuid.UUID,
	query string,
	limit int,
) ([]ThreadWithActiveRun, error) {
	return r.SearchByQueryWithMode(ctx, accountID, ownerUserID, query, limit, "")
}

func (r *ThreadRepository) SearchByQueryWithMode(
	ctx context.Context,
	accountID uuid.UUID,
	ownerUserID uuid.UUID,
	query string,
	limit int,
	modeFilter string,
) ([]ThreadWithActiveRun, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return nil, fmt.Errorf("account_id must not be empty")
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
	normalizedMode := ""
	if strings.TrimSpace(modeFilter) != "" {
		var ok bool
		normalizedMode, ok = NormalizeThreadMode(modeFilter)
		if !ok {
			return nil, fmt.Errorf("invalid mode")
		}
	}

	like := "%" + escapeILikePattern(query) + "%"

	sql := `SELECT DISTINCT ON (t.updated_at, t.id)
		        t.id, t.account_id, t.created_by_user_id, t.title, t.created_at, t.updated_at,
		        t.deleted_at, t.project_id, t.is_private, t.mode, t.collaboration_mode, t.collaboration_mode_revision, t.learning_mode_enabled, t.sidebar_work_folder, t.sidebar_pinned_at, t.sidebar_gtd_bucket, t.expires_at,
		        t.parent_thread_id, t.branched_from_message_id, t.title_locked, r.id AS active_run_id
		 FROM threads t
			 LEFT JOIN messages m
			   ON m.thread_id = t.id
			  AND m.deleted_at IS NULL
			  AND m.hidden = FALSE
			 LEFT JOIN LATERAL (
			   SELECT rr.id FROM runs rr
			   WHERE rr.thread_id = t.id
			     AND rr.status IN ('running', 'cancelling')
			     AND rr.deleted_at IS NULL
			     AND NOT EXISTS (
			       SELECT 1
			       FROM run_events re
			       WHERE re.run_id = rr.id
			         AND re.type IN ('run.completed', 'run.failed', 'run.cancelled', 'run.interrupted')
			     )
			   ORDER BY rr.created_at DESC, rr.id DESC
			   LIMIT 1
			 ) r ON true
		 WHERE t.account_id = $1
		   AND t.created_by_user_id = $2
		   AND t.deleted_at IS NULL
		   AND t.is_private = false`
	args := []any{accountID, ownerUserID}
	if normalizedMode != "" {
		args = append(args, normalizedMode)
		sql += `
		   AND t.mode = $` + fmt.Sprintf("%d", len(args))
	}
	args = append(args, like)
	likeParam := fmt.Sprintf("$%d", len(args))
	sql += `
		   AND (
		     t.title ILIKE ` + likeParam + ` ESCAPE '!'
		     OR m.content ILIKE ` + likeParam + ` ESCAPE '!'
		   )
		 ORDER BY t.updated_at DESC, t.id DESC
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
			&item.ID, &item.AccountID, &item.CreatedByUserID, &item.Title, &item.CreatedAt, &item.UpdatedAt,
			&item.DeletedAt, &item.ProjectID, &item.IsPrivate, &item.Mode, &item.CollaborationMode, &item.CollaborationModeRevision, &item.LearningModeEnabled, &item.SidebarWorkFolder, &item.SidebarPinnedAt, &item.SidebarGtdBucket, &item.ExpiresAt,
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
	accountID uuid.UUID,
	createdByUserID *uuid.UUID,
	parentThreadID uuid.UUID,
	branchFromMessageID uuid.UUID,
	isPrivate bool,
) (Thread, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return Thread{}, fmt.Errorf("account_id must not be empty")
	}
	if parentThreadID == uuid.Nil {
		return Thread{}, fmt.Errorf("parent_thread_id must not be empty")
	}
	if branchFromMessageID == uuid.Nil {
		return Thread{}, fmt.Errorf("branched_from_message_id must not be empty")
	}

	var thread Thread
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO threads (account_id, created_by_user_id, project_id, title, is_private, mode, sidebar_work_folder, sidebar_gtd_bucket, expires_at, updated_at, parent_thread_id, branched_from_message_id, collaboration_mode, learning_mode_enabled)
		 SELECT $1, $2, project_id, title, $3, mode, sidebar_work_folder, sidebar_gtd_bucket, CASE WHEN $3 THEN now() + INTERVAL '24 hours' ELSE NULL END, now(), $4, $5, collaboration_mode, learning_mode_enabled
		 FROM threads WHERE id = $4 AND deleted_at IS NULL
		 RETURNING id, account_id, created_by_user_id, title, created_at, updated_at, deleted_at, project_id, is_private, mode, collaboration_mode, collaboration_mode_revision, learning_mode_enabled, sidebar_work_folder, sidebar_pinned_at, sidebar_gtd_bucket, expires_at, parent_thread_id, branched_from_message_id, title_locked`,
		accountID,
		createdByUserID,
		isPrivate,
		parentThreadID,
		branchFromMessageID,
	).Scan(&thread.ID, &thread.AccountID, &thread.CreatedByUserID, &thread.Title, &thread.CreatedAt, &thread.UpdatedAt,
		&thread.DeletedAt, &thread.ProjectID, &thread.IsPrivate, &thread.Mode, &thread.CollaborationMode, &thread.CollaborationModeRevision, &thread.LearningModeEnabled, &thread.SidebarWorkFolder, &thread.SidebarPinnedAt, &thread.SidebarGtdBucket, &thread.ExpiresAt,
		&thread.ParentThreadID, &thread.BranchedFromMessageID, &thread.TitleLocked)
	if err != nil {
		return Thread{}, err
	}
	return thread, nil
}
