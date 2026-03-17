package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Run struct {
	ID              uuid.UUID
	AccountID           uuid.UUID
	ThreadID        uuid.UUID
	CreatedByUserID *uuid.UUID
	Status          string
	CreatedAt       time.Time

	// R12 lifecycle fields
	ParentRunID       *uuid.UUID
	StatusUpdatedAt   *time.Time
	CompletedAt       *time.Time
	FailedAt          *time.Time
	DurationMs        *int64
	TotalInputTokens  *int64
	TotalOutputTokens *int64
	TotalCostUSD      *float64
	Model             *string
	PersonaID         *string
	ProfileRef        *string
	WorkspaceRef      *string
	DeletedAt         *time.Time
}

type RunEvent struct {
	EventID    uuid.UUID
	RunID      uuid.UUID
	Seq        int64
	TS         time.Time
	Type       string
	DataJSON   any
	ToolName   *string
	ErrorClass *string
}

type RunNotFoundError struct {
	RunID uuid.UUID
}

func (e RunNotFoundError) Error() string {
	return "run not found"
}

type RunEventRepository struct {
	db Querier
}

func NewRunEventRepository(db Querier) (*RunEventRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &RunEventRepository{db: db}, nil
}

func (r *RunEventRepository) CreateRunWithStartedEvent(
	ctx context.Context,
	accountID uuid.UUID,
	threadID uuid.UUID,
	createdByUserID *uuid.UUID,
	startedType string,
	startedData map[string]any,
) (Run, RunEvent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return Run{}, RunEvent{}, fmt.Errorf("account_id must not be empty")
	}
	if threadID == uuid.Nil {
		return Run{}, RunEvent{}, fmt.Errorf("thread_id must not be empty")
	}

	chosenType := startedType
	if chosenType == "" {
		chosenType = "run.started"
	}

	var run Run
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO runs (account_id, thread_id, created_by_user_id, status)
		 VALUES ($1, $2, $3, 'running')
		 RETURNING id, account_id, thread_id, created_by_user_id, status, created_at,
		           parent_run_id, status_updated_at, completed_at, failed_at,
		           duration_ms, total_input_tokens, total_output_tokens, total_cost_usd,
		           model, persona_id, deleted_at`,
		accountID,
		threadID,
		createdByUserID,
	).Scan(
		&run.ID, &run.AccountID, &run.ThreadID, &run.CreatedByUserID, &run.Status, &run.CreatedAt,
		&run.ParentRunID, &run.StatusUpdatedAt, &run.CompletedAt, &run.FailedAt,
		&run.DurationMs, &run.TotalInputTokens, &run.TotalOutputTokens, &run.TotalCostUSD,
		&run.Model, &run.PersonaID, &run.DeletedAt,
	)
	if err != nil {
		return Run{}, RunEvent{}, err
	}

	event, err := r.insertEvent(ctx, run.ID, chosenType, mapOrEmpty(startedData), nil, nil)
	if err != nil {
		return Run{}, RunEvent{}, err
	}

	return run, event, nil
}

func (r *RunEventRepository) GetRun(ctx context.Context, runID uuid.UUID) (*Run, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if runID == uuid.Nil {
		return nil, fmt.Errorf("run_id must not be empty")
	}

	var run Run
	err := r.db.QueryRow(
		ctx,
		`SELECT id, account_id, thread_id, created_by_user_id, status, created_at,
		        parent_run_id, status_updated_at, completed_at, failed_at,
		        duration_ms, total_input_tokens, total_output_tokens, total_cost_usd,
		        model, persona_id, profile_ref, workspace_ref, deleted_at
		 FROM runs
		 WHERE id = $1
		 LIMIT 1`,
		runID,
	).Scan(
		&run.ID, &run.AccountID, &run.ThreadID, &run.CreatedByUserID, &run.Status, &run.CreatedAt,
		&run.ParentRunID, &run.StatusUpdatedAt, &run.CompletedAt, &run.FailedAt,
		&run.DurationMs, &run.TotalInputTokens, &run.TotalOutputTokens, &run.TotalCostUSD,
		&run.Model, &run.PersonaID, &run.ProfileRef, &run.WorkspaceRef, &run.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &run, nil
}

// GetRunForAccount returns a run only if it belongs to the specified account.
func (r *RunEventRepository) GetRunForAccount(ctx context.Context, accountID uuid.UUID, runID uuid.UUID) (*Run, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return nil, fmt.Errorf("account_id must not be empty")
	}
	if runID == uuid.Nil {
		return nil, fmt.Errorf("run_id must not be empty")
	}

	var run Run
	err := r.db.QueryRow(
		ctx,
		`SELECT id, account_id, thread_id, created_by_user_id, status, created_at,
		        parent_run_id, status_updated_at, completed_at, failed_at,
		        duration_ms, total_input_tokens, total_output_tokens, total_cost_usd,
		        model, persona_id, profile_ref, workspace_ref, deleted_at
		 FROM runs
		 WHERE id = $1 AND account_id = $2
		 LIMIT 1`,
		runID,
		accountID,
	).Scan(
		&run.ID, &run.AccountID, &run.ThreadID, &run.CreatedByUserID, &run.Status, &run.CreatedAt,
		&run.ParentRunID, &run.StatusUpdatedAt, &run.CompletedAt, &run.FailedAt,
		&run.DurationMs, &run.TotalInputTokens, &run.TotalOutputTokens, &run.TotalCostUSD,
		&run.Model, &run.PersonaID, &run.ProfileRef, &run.WorkspaceRef, &run.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &run, nil
}

func (r *RunEventRepository) ListRunsByThread(
	ctx context.Context,
	accountID uuid.UUID,
	threadID uuid.UUID,
	limit int,
) ([]Run, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return nil, fmt.Errorf("account_id must not be empty")
	}
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("thread_id must not be empty")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be positive")
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT id, account_id, thread_id, created_by_user_id, status, created_at,
		        parent_run_id, status_updated_at, completed_at, failed_at,
		        duration_ms, total_input_tokens, total_output_tokens, total_cost_usd,
		        model, persona_id, profile_ref, workspace_ref, deleted_at
		 FROM runs
		 WHERE account_id = $1
		   AND thread_id = $2
		 ORDER BY created_at DESC, id DESC
		 LIMIT $3`,
		accountID,
		threadID,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := []Run{}
	for rows.Next() {
		var run Run
		if err := rows.Scan(
			&run.ID, &run.AccountID, &run.ThreadID, &run.CreatedByUserID, &run.Status, &run.CreatedAt,
			&run.ParentRunID, &run.StatusUpdatedAt, &run.CompletedAt, &run.FailedAt,
			&run.DurationMs, &run.TotalInputTokens, &run.TotalOutputTokens, &run.TotalCostUSD,
			&run.Model, &run.PersonaID, &run.ProfileRef, &run.WorkspaceRef, &run.DeletedAt,
		); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return runs, nil
}

func (r *RunEventRepository) GetLatestEventType(
	ctx context.Context,
	runID uuid.UUID,
	types []string,
) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if runID == uuid.Nil {
		return "", fmt.Errorf("run_id must not be empty")
	}
	if len(types) == 0 {
		return "", nil
	}

	var eventType string
	err := r.db.QueryRow(
		ctx,
		`SELECT type
		 FROM run_events
		 WHERE run_id = $1
		   AND type = ANY($2)
		 ORDER BY seq DESC
		 LIMIT 1`,
		runID,
		types,
	).Scan(&eventType)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return eventType, nil
}

func (r *RunEventRepository) RequestCancel(
	ctx context.Context,
	runID uuid.UUID,
	requestedByUserID *uuid.UUID,
	traceID string,
) (*RunEvent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if runID == uuid.Nil {
		return nil, fmt.Errorf("run_id must not be empty")
	}

	if err := r.lockRunRow(ctx, runID); err != nil {
		return nil, err
	}

	terminal, err := r.GetLatestEventType(ctx, runID, []string{"run.completed", "run.failed", "run.cancelled"})
	if err != nil {
		return nil, err
	}
	if terminal != "" {
		return nil, nil
	}

	existing, err := r.GetLatestEventType(ctx, runID, []string{"run.cancel_requested", "run.cancelled"})
	if err != nil {
		return nil, err
	}
	if existing != "" {
		return nil, nil
	}

	dataJSON := map[string]any{"trace_id": traceID}
	if requestedByUserID != nil && *requestedByUserID != uuid.Nil {
		dataJSON["requested_by_user_id"] = requestedByUserID.String()
	}

	event, err := r.insertEvent(ctx, runID, "run.cancel_requested", dataJSON, nil, nil)
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func (r *RunEventRepository) ListEvents(
	ctx context.Context,
	runID uuid.UUID,
	afterSeq int64,
	limit int,
) ([]RunEvent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if runID == uuid.Nil {
		return nil, fmt.Errorf("run_id must not be empty")
	}
	if afterSeq < 0 {
		return nil, fmt.Errorf("after_seq must be non-negative")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be positive")
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT event_id, run_id, seq, ts, type, data_json, tool_name, error_class
		 FROM run_events
		 WHERE run_id = $1
		   AND seq > $2
		 ORDER BY seq ASC
		 LIMIT $3`,
		runID,
		afterSeq,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []RunEvent{}
	for rows.Next() {
		var (
			event   RunEvent
			rawJSON []byte
		)
		if err := rows.Scan(
			&event.EventID,
			&event.RunID,
			&event.Seq,
			&event.TS,
			&event.Type,
			&rawJSON,
			&event.ToolName,
			&event.ErrorClass,
		); err != nil {
			return nil, err
		}

		if len(rawJSON) > 0 {
			var parsed any
			if err := json.Unmarshal(rawJSON, &parsed); err == nil {
				event.DataJSON = parsed
			}
		}

		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func (r *RunEventRepository) lockRunRow(ctx context.Context, runID uuid.UUID) error {
	var lockedID uuid.UUID
	err := r.db.QueryRow(
		ctx,
		`SELECT id
		 FROM runs
		 WHERE id = $1
		 FOR UPDATE`,
		runID,
	).Scan(&lockedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RunNotFoundError{RunID: runID}
		}
		return err
	}
	return nil
}

func (r *RunEventRepository) insertEvent(
	ctx context.Context,
	runID uuid.UUID,
	eventType string,
	dataJSON any,
	toolName *string,
	errorClass *string,
) (RunEvent, error) {
	seq, err := r.allocateSeq(ctx, runID)
	if err != nil {
		return RunEvent{}, err
	}

	payload := dataJSON
	if payload == nil {
		payload = map[string]any{}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return RunEvent{}, err
	}

	var event RunEvent
	err = r.db.QueryRow(
		ctx,
		`INSERT INTO run_events (run_id, seq, type, data_json, tool_name, error_class)
		 VALUES ($1, $2, $3, $4::jsonb, $5, $6)
		 RETURNING event_id, run_id, seq, ts, type, tool_name, error_class`,
		runID,
		seq,
		eventType,
		string(encoded),
		toolName,
		errorClass,
	).Scan(
		&event.EventID,
		&event.RunID,
		&event.Seq,
		&event.TS,
		&event.Type,
		&event.ToolName,
		&event.ErrorClass,
	)
	if err != nil {
		return RunEvent{}, err
	}
	event.DataJSON = payload
	return event, nil
}

// allocateSeq returns a gapless per-run sequence number.
// Requires r.db to be a transaction for cross-query lock persistence.
func (r *RunEventRepository) allocateSeq(ctx context.Context, runID uuid.UUID) (int64, error) {
	if _, err := r.db.Exec(ctx, `SELECT 1 FROM runs WHERE id = $1 FOR UPDATE`, runID); err != nil {
		return 0, err
	}
	var seq int64
	err := r.db.QueryRow(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM run_events WHERE run_id = $1`,
		runID,
	).Scan(&seq)
	return seq, err
}

// ProvideInput 向运行中的 run 注入用户输入。
// 检查 run 非终态后写入 run.input_provided 事件，调用方负责提交事务并 pg_notify。
func (r *RunEventRepository) ProvideInput(
	ctx context.Context,
	runID uuid.UUID,
	content string,
	traceID string,
) (*RunEvent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if runID == uuid.Nil {
		return nil, fmt.Errorf("run_id must not be empty")
	}
	if content == "" {
		return nil, fmt.Errorf("content must not be empty")
	}

	if err := r.lockRunRow(ctx, runID); err != nil {
		return nil, err
	}

	terminal, err := r.GetLatestEventType(ctx, runID, []string{"run.completed", "run.failed", "run.cancelled"})
	if err != nil {
		return nil, err
	}
	if terminal != "" {
		return nil, RunNotActiveError{RunID: runID}
	}

	dataJSON := map[string]any{"content": content}
	if traceID != "" {
		dataJSON["trace_id"] = traceID
	}

	event, err := r.insertEvent(ctx, runID, "run.input_provided", dataJSON, nil, nil)
	if err != nil {
		return nil, err
	}
	return &event, nil
}

// RunNotActiveError 表示 run 已处于终态，无法接收输入。
type RunNotActiveError struct {
	RunID uuid.UUID
}

func (e RunNotActiveError) Error() string {
	return "run is not active"
}

func mapOrEmpty(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

// RunWithUser 在 Run 基础上附加创建者的用户信息（LEFT JOIN users）。
type RunWithUser struct {
	Run
	UserUsername        *string
	UserEmail           *string
	CacheReadTokens     *int64
	CacheCreationTokens *int64
	CachedTokens        *int64
	CreditsUsed         *int64 // 本次 run 扣除的积分（来自 credit_transactions）
}

// ListRunsParams 控制 ListRuns 的过滤和分页行为。
// AccountID 为 nil 时不按 account 过滤（平台管理员全局查询专用）。
type ListRunsParams struct {
	RunID          *uuid.UUID
	RunIDPrefix    *string
	AccountID          *uuid.UUID
	ThreadID       *uuid.UUID
	ThreadIDPrefix *string
	UserID         *uuid.UUID
	ParentRunID    *uuid.UUID
	Status         *string
	Model          *string
	PersonaID      *string
	Since          *time.Time
	Until          *time.Time
	Limit          int
	Offset         int
}

// ListRuns 跨 thread 查询 runs，LEFT JOIN users 附带创建者信息，返回结果列表和满足条件的总行数。
func (r *RunEventRepository) ListRuns(ctx context.Context, params ListRunsParams) ([]RunWithUser, int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	limit := params.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}

	args := []any{}
	conds := []string{"r.deleted_at IS NULL"}

	addArg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	if params.AccountID != nil {
		conds = append(conds, "r.account_id = "+addArg(*params.AccountID))
	}
	if params.RunID != nil {
		conds = append(conds, "r.id = "+addArg(*params.RunID))
	} else if params.RunIDPrefix != nil {
		conds = append(conds, "r.id::text ILIKE "+addArg(*params.RunIDPrefix)+" || '%'")
	}
	if params.ThreadID != nil {
		conds = append(conds, "r.thread_id = "+addArg(*params.ThreadID))
	} else if params.ThreadIDPrefix != nil {
		conds = append(conds, "r.thread_id::text ILIKE "+addArg(*params.ThreadIDPrefix)+" || '%'")
	}
	if params.UserID != nil {
		conds = append(conds, "r.created_by_user_id = "+addArg(*params.UserID))
	}
	if params.ParentRunID != nil {
		conds = append(conds, "r.parent_run_id = "+addArg(*params.ParentRunID))
	}
	if params.Status != nil {
		conds = append(conds, "r.status = "+addArg(*params.Status))
	}
	if params.Model != nil {
		conds = append(conds, "COALESCE(r.model, '') ILIKE '%' || "+addArg(*params.Model)+" || '%'")
	}
	if params.PersonaID != nil {
		conds = append(conds, "COALESCE(r.persona_id, '') ILIKE '%' || "+addArg(*params.PersonaID)+" || '%'")
	}
	if params.Since != nil {
		conds = append(conds, "r.created_at >= "+addArg(*params.Since))
	}
	if params.Until != nil {
		conds = append(conds, "r.created_at <= "+addArg(*params.Until))
	}

	where := " WHERE " + strings.Join(conds, " AND ")

	var total int64
	if err := r.db.QueryRow(ctx,
		"SELECT COUNT(*) FROM runs r LEFT JOIN users u ON u.id = r.created_by_user_id"+where,
		args...,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count runs: %w", err)
	}

	// 用关联子查询替代 LEFT JOIN LATERAL，兼容 PostgreSQL 和 SQLite。
	query := fmt.Sprintf(`SELECT r.id, r.account_id, r.thread_id, r.created_by_user_id, r.status, r.created_at,
		        r.parent_run_id, r.status_updated_at, r.completed_at, r.failed_at,
		        r.duration_ms, r.total_input_tokens, r.total_output_tokens, r.total_cost_usd,
		        r.model, r.persona_id, r.deleted_at,
		        u.username, u.email,
		        (SELECT SUM(ur2.cache_read_tokens)     FROM usage_records ur2 WHERE ur2.run_id = r.id) AS cache_read_tokens,
		        (SELECT SUM(ur2.cache_creation_tokens) FROM usage_records ur2 WHERE ur2.run_id = r.id) AS cache_creation_tokens,
		        (SELECT SUM(ur2.cached_tokens)         FROM usage_records ur2 WHERE ur2.run_id = r.id) AS cached_tokens,
		        (SELECT ABS(SUM(ct2.amount)) FROM credit_transactions ct2 WHERE ct2.reference_id = r.id AND ct2.type = 'consumption') AS credits_used
		 FROM runs r
		 LEFT JOIN users u ON u.id = r.created_by_user_id%s
		 ORDER BY r.created_at DESC, r.id DESC
		 LIMIT %s OFFSET %s`,
		where, addArg(limit), addArg(offset),
	)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	runs := []RunWithUser{}
	for rows.Next() {
		var rw RunWithUser
		if err := rows.Scan(
			&rw.ID, &rw.AccountID, &rw.ThreadID, &rw.CreatedByUserID, &rw.Status, &rw.CreatedAt,
			&rw.ParentRunID, &rw.StatusUpdatedAt, &rw.CompletedAt, &rw.FailedAt,
			&rw.DurationMs, &rw.TotalInputTokens, &rw.TotalOutputTokens, &rw.TotalCostUSD,
			&rw.Model, &rw.PersonaID, &rw.DeletedAt,
			&rw.UserUsername, &rw.UserEmail,
			&rw.CacheReadTokens, &rw.CacheCreationTokens, &rw.CachedTokens,
			&rw.CreditsUsed,
		); err != nil {
			return nil, 0, err
		}
		runs = append(runs, rw)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return runs, total, nil
}

func (r *RunEventRepository) CountAll(ctx context.Context) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var count int64
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM runs WHERE deleted_at IS NULL`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("runs.CountAll: %w", err)
	}
	return count, nil
}

// ListStaleRunning 查询所有 status='running' 且最后活跃时间早于 staleBefore 的 run。
func (r *RunEventRepository) ListStaleRunning(ctx context.Context, staleBefore time.Time) ([]Run, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT id, account_id, thread_id, created_by_user_id, status, created_at,
		        parent_run_id, status_updated_at, completed_at, failed_at,
		        duration_ms, total_input_tokens, total_output_tokens, total_cost_usd,
		        model, persona_id, deleted_at
		 FROM runs
		 WHERE status = 'running'
		   AND COALESCE(status_updated_at, created_at) < $1`,
		staleBefore.UTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("ListStaleRunning: %w", err)
	}
	defer rows.Close()

	var runs []Run
	for rows.Next() {
		var run Run
		if err := rows.Scan(
			&run.ID, &run.AccountID, &run.ThreadID, &run.CreatedByUserID, &run.Status, &run.CreatedAt,
			&run.ParentRunID, &run.StatusUpdatedAt, &run.CompletedAt, &run.FailedAt,
			&run.DurationMs, &run.TotalInputTokens, &run.TotalOutputTokens, &run.TotalCostUSD,
			&run.Model, &run.PersonaID, &run.DeletedAt,
		); err != nil {
			return nil, fmt.Errorf("ListStaleRunning scan: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListStaleRunning rows: %w", err)
	}
	return runs, nil
}

// ListChildRunIDs 返回指定 run 的所有子 run ID，按创建时间升序。
func (r *RunEventRepository) ListChildRunIDs(ctx context.Context, parentRunID uuid.UUID) ([]uuid.UUID, error) {
	if parentRunID == uuid.Nil {
		return nil, nil
	}
	rows, err := r.db.Query(ctx,
		`SELECT id FROM runs WHERE parent_run_id = $1 ORDER BY created_at ASC`,
		parentRunID,
	)
	if err != nil {
		return nil, fmt.Errorf("ListChildRunIDs: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("ListChildRunIDs scan: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ForceFailRun 原子地将一个 running 的 run 标记为 failed 并写入 run.failed 事件。
// 返回 (true, nil) 表示实际执行了更新；(false, nil) 表示 run 已不在 running 状态（no-op）。
func (r *RunEventRepository) ForceFailRun(ctx context.Context, runID uuid.UUID) (bool, error) {
	if runID == uuid.Nil {
		return false, fmt.Errorf("run_id must not be empty")
	}

	// UPDATE takes an exclusive lock on the runs row, serializing seq allocation.
	tag, err := r.db.Exec(
		ctx,
		`WITH updated AS (
		     UPDATE runs
		     SET status = 'failed',
		         failed_at = now(),
		         status_updated_at = now()
		     WHERE id = $1
		       AND status = 'running'
		     RETURNING id
		 ),
		 next_seq AS (
		     SELECT COALESCE(MAX(seq), 0) + 1 AS seq
		     FROM run_events
		     WHERE run_id = $1
		 )
		 INSERT INTO run_events (run_id, seq, type, data_json, error_class)
		 SELECT updated.id,
		        next_seq.seq,
		        'run.failed',
		        '{"reason":"stale run reaped by system"}'::jsonb,
		        'worker.timeout'
		 FROM updated, next_seq`,
		runID,
	)
	if err != nil {
		return false, fmt.Errorf("ForceFailRun: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *RunEventRepository) CountSince(ctx context.Context, since time.Time) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var count int64
	err := r.db.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM runs WHERE deleted_at IS NULL AND created_at >= $1`,
		since.UTC(),
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("runs.CountSince: %w", err)
	}
	return count, nil
}
