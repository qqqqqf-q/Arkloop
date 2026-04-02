package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"arkloop/services/shared/runkind"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Run struct {
	ID              uuid.UUID
	AccountID       uuid.UUID
	ThreadID        uuid.UUID
	CreatedByUserID *uuid.UUID
	Status          string
	CreatedAt       time.Time

	// R12 lifecycle fields
	ParentRunID       *uuid.UUID
	ResumeFromRunID   *uuid.UUID
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

var ErrThreadBusy = errors.New("thread has active root run")

const runStartedThreadTailMessageIDKey = "thread_tail_message_id"
const runStartedRunKindKey = "run_kind"

func (r *RunEventRepository) WithTx(tx pgx.Tx) *RunEventRepository {
	return &RunEventRepository{db: tx}
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
	return r.createRunWithStartedEvent(ctx, accountID, threadID, createdByUserID, startedType, startedData, nil)
}

func (r *RunEventRepository) createRunWithStartedEvent(
	ctx context.Context,
	accountID uuid.UUID,
	threadID uuid.UUID,
	createdByUserID *uuid.UUID,
	startedType string,
	startedData map[string]any,
	resumeFromRunID *uuid.UUID,
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
		`INSERT INTO runs (account_id, thread_id, created_by_user_id, status, resume_from_run_id)
		 VALUES ($1, $2, $3, 'running', $4)
		 RETURNING id, account_id, thread_id, created_by_user_id, status, created_at,
		           parent_run_id, resume_from_run_id, status_updated_at, completed_at, failed_at,
		           duration_ms, total_input_tokens, total_output_tokens, total_cost_usd,
		           model, persona_id, deleted_at`,
		accountID,
		threadID,
		createdByUserID,
		resumeFromRunID,
	).Scan(
		&run.ID, &run.AccountID, &run.ThreadID, &run.CreatedByUserID, &run.Status, &run.CreatedAt,
		&run.ParentRunID, &run.ResumeFromRunID, &run.StatusUpdatedAt, &run.CompletedAt, &run.FailedAt,
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

func (r *RunEventRepository) CreateRootRunWithClaim(
	ctx context.Context,
	accountID uuid.UUID,
	threadID uuid.UUID,
	createdByUserID *uuid.UUID,
	startedType string,
	startedData map[string]any,
) (Run, RunEvent, error) {
	return r.CreateRootRunWithClaimFrom(ctx, accountID, threadID, createdByUserID, startedType, startedData)
}

func (r *RunEventRepository) CreateRootRunWithResume(
	ctx context.Context,
	accountID uuid.UUID,
	threadID uuid.UUID,
	createdByUserID *uuid.UUID,
	startedType string,
	startedData map[string]any,
	resumeFromRunID uuid.UUID,
) (Run, RunEvent, error) {
	if resumeFromRunID == uuid.Nil {
		return Run{}, RunEvent{}, fmt.Errorf("resume_from_run_id must not be empty")
	}
	if err := r.LockThreadRow(ctx, threadID); err != nil {
		return Run{}, RunEvent{}, err
	}
	if active, err := r.GetActiveRootRunForThread(ctx, threadID); err != nil {
		return Run{}, RunEvent{}, err
	} else if active != nil {
		return Run{}, RunEvent{}, ErrThreadBusy
	}
	startedData, _, err := r.withThreadTailMessage(ctx, threadID, startedData)
	if err != nil {
		return Run{}, RunEvent{}, err
	}
	startedData = applyContinuationMetadata(startedData, &resumeFromRunID)
	return r.createRunWithStartedEvent(ctx, accountID, threadID, createdByUserID, startedType, startedData, &resumeFromRunID)
}

func (r *RunEventRepository) CreateRootRunWithClaimFrom(
	ctx context.Context,
	accountID uuid.UUID,
	threadID uuid.UUID,
	createdByUserID *uuid.UUID,
	startedType string,
	startedData map[string]any,
) (Run, RunEvent, error) {
	if err := r.LockThreadRow(ctx, threadID); err != nil {
		return Run{}, RunEvent{}, err
	}
	if active, err := r.GetActiveRootRunForThread(ctx, threadID); err != nil {
		return Run{}, RunEvent{}, err
	} else if active != nil {
		// heartbeat run 不阻塞 normal run，只有 heartbeat vs heartbeat 才互斥
		incomingKind := runKindFromData(startedData)
		if !strings.EqualFold(incomingKind, runkind.Heartbeat) {
			activeData, err := r.firstEventData(ctx, active)
			if err != nil {
				return Run{}, RunEvent{}, err
			}
			if strings.EqualFold(runKindFromData(activeData), runkind.Heartbeat) {
				// active 是 heartbeat，incoming 是 normal，放行
			} else {
				return Run{}, RunEvent{}, ErrThreadBusy
			}
		} else {
			return Run{}, RunEvent{}, ErrThreadBusy
		}
	}
	startedData, latestThreadMessage, err := r.withThreadTailMessage(ctx, threadID, startedData)
	if err != nil {
		return Run{}, RunEvent{}, err
	}
	latestRootRun, err := r.GetLatestRootRunForThread(ctx, threadID)
	if err != nil {
		return Run{}, RunEvent{}, err
	}
	latestRootRunStartedData, err := r.firstEventData(ctx, latestRootRun)
	if err != nil {
		return Run{}, RunEvent{}, err
	}
	resumeFromRunID := shouldResumeFromRun(
		latestRootRun,
		latestThreadMessage,
		threadTailMessageIDFromData(latestRootRunStartedData),
		runKindFromData(latestRootRunStartedData),
	)
	startedData = applyContinuationMetadata(startedData, resumeFromRunID)
	return r.createRunWithStartedEvent(ctx, accountID, threadID, createdByUserID, startedType, startedData, resumeFromRunID)
}

type threadTailMessage struct {
	ID   uuid.UUID
	Role string
}

func shouldResumeFromRun(run *Run, latestMessage *threadTailMessage, previousTailMessageID string, previousRunKind string) *uuid.UUID {
	if run == nil || latestMessage == nil {
		return nil
	}
	if latestMessage.Role != "user" {
		return nil
	}
	if strings.TrimSpace(previousTailMessageID) == "" {
		return nil
	}
	if latestMessage.ID.String() == previousTailMessageID {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(previousRunKind), runkind.Heartbeat) {
		return nil
	}
	switch run.Status {
	case "interrupted", "cancelled":
		return &run.ID
	default:
		return nil
	}
}

func threadTailMessageIDFromData(data map[string]any) string {
	if data == nil {
		return ""
	}
	raw, _ := data[runStartedThreadTailMessageIDKey].(string)
	return strings.TrimSpace(raw)
}

func runKindFromData(data map[string]any) string {
	if data == nil {
		return ""
	}
	raw, _ := data[runStartedRunKindKey].(string)
	return strings.TrimSpace(raw)
}

func applyContinuationMetadata(data map[string]any, resumeFromRunID *uuid.UUID) map[string]any {
	if data == nil {
		data = map[string]any{}
	}
	if resumeFromRunID == nil {
		data["continuation_source"] = "none"
		data["continuation_loop"] = false
		delete(data, "continuation_response")
		return data
	}
	data["continuation_source"] = "user_followup"
	data["continuation_loop"] = true
	data["continuation_response"] = true
	return data
}

func (r *RunEventRepository) withThreadTailMessage(
	ctx context.Context,
	threadID uuid.UUID,
	startedData map[string]any,
) (map[string]any, *threadTailMessage, error) {
	out := cloneMap(startedData)
	msg, err := r.getLatestThreadMessage(ctx, threadID)
	if err != nil {
		return nil, nil, err
	}
	if msg != nil {
		out[runStartedThreadTailMessageIDKey] = msg.ID.String()
	}
	return out, msg, nil
}

func (r *RunEventRepository) getLatestThreadMessage(ctx context.Context, threadID uuid.UUID) (*threadTailMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("thread_id must not be empty")
	}
	var msg threadTailMessage
	err := r.db.QueryRow(
		ctx,
		`SELECT id, role
		 FROM messages
		 WHERE thread_id = $1
		   AND hidden = FALSE
		   AND deleted_at IS NULL
		 ORDER BY created_at DESC, id DESC
		 LIMIT 1`,
		threadID,
	).Scan(&msg.ID, &msg.Role)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	msg.Role = strings.TrimSpace(msg.Role)
	return &msg, nil
}

func (r *RunEventRepository) firstEventData(ctx context.Context, run *Run) (map[string]any, error) {
	if run == nil || run.ID == uuid.Nil {
		return nil, nil
	}
	var raw []byte
	err := r.db.QueryRow(
		ctx,
		`SELECT data_json
		 FROM run_events
		 WHERE run_id = $1
		 ORDER BY seq ASC
		 LIMIT 1`,
		run.ID,
	).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
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
		        parent_run_id, resume_from_run_id, status_updated_at, completed_at, failed_at,
		        duration_ms, total_input_tokens, total_output_tokens, total_cost_usd,
		        model, persona_id, profile_ref, workspace_ref, deleted_at
		 FROM runs
		 WHERE id = $1
		 LIMIT 1`,
		runID,
	).Scan(
		&run.ID, &run.AccountID, &run.ThreadID, &run.CreatedByUserID, &run.Status, &run.CreatedAt,
		&run.ParentRunID, &run.ResumeFromRunID, &run.StatusUpdatedAt, &run.CompletedAt, &run.FailedAt,
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
		        parent_run_id, resume_from_run_id, status_updated_at, completed_at, failed_at,
		        duration_ms, total_input_tokens, total_output_tokens, total_cost_usd,
		        model, persona_id, profile_ref, workspace_ref, deleted_at
		 FROM runs
		 WHERE id = $1 AND account_id = $2
		 LIMIT 1`,
		runID,
		accountID,
	).Scan(
		&run.ID, &run.AccountID, &run.ThreadID, &run.CreatedByUserID, &run.Status, &run.CreatedAt,
		&run.ParentRunID, &run.ResumeFromRunID, &run.StatusUpdatedAt, &run.CompletedAt, &run.FailedAt,
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
		        parent_run_id, resume_from_run_id, status_updated_at, completed_at, failed_at,
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
			&run.ParentRunID, &run.ResumeFromRunID, &run.StatusUpdatedAt, &run.CompletedAt, &run.FailedAt,
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

// GetActiveRootRunForThread 返回 thread 上按创建时间最新的 running/cancelling root run。
func (r *RunEventRepository) GetActiveRootRunForThread(
	ctx context.Context,
	threadID uuid.UUID,
) (*Run, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("thread_id must not be empty")
	}
	var run Run
	err := r.db.QueryRow(
		ctx,
		`SELECT id, account_id, thread_id, created_by_user_id, status, created_at,
		        parent_run_id, resume_from_run_id, status_updated_at, completed_at, failed_at,
		        duration_ms, total_input_tokens, total_output_tokens, total_cost_usd,
		        model, persona_id, profile_ref, workspace_ref, deleted_at
		   FROM runs
		  WHERE thread_id = $1
		    AND parent_run_id IS NULL
		    AND status IN ('running', 'cancelling')
		    AND deleted_at IS NULL
		  ORDER BY created_at DESC, id DESC
		  LIMIT 1`,
		threadID,
	).Scan(
		&run.ID, &run.AccountID, &run.ThreadID, &run.CreatedByUserID, &run.Status, &run.CreatedAt,
		&run.ParentRunID, &run.ResumeFromRunID, &run.StatusUpdatedAt, &run.CompletedAt, &run.FailedAt,
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

func (r *RunEventRepository) GetLatestRootRunForThread(
	ctx context.Context,
	threadID uuid.UUID,
) (*Run, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("thread_id must not be empty")
	}
	var run Run
	err := r.db.QueryRow(
		ctx,
		`SELECT id, account_id, thread_id, created_by_user_id, status, created_at,
		        parent_run_id, resume_from_run_id, status_updated_at, completed_at, failed_at,
		        duration_ms, total_input_tokens, total_output_tokens, total_cost_usd,
		        model, persona_id, profile_ref, workspace_ref, deleted_at
		   FROM runs
		  WHERE thread_id = $1
		    AND parent_run_id IS NULL
		    AND deleted_at IS NULL
		  ORDER BY created_at DESC, id DESC
		  LIMIT 1`,
		threadID,
	).Scan(
		&run.ID, &run.AccountID, &run.ThreadID, &run.CreatedByUserID, &run.Status, &run.CreatedAt,
		&run.ParentRunID, &run.ResumeFromRunID, &run.StatusUpdatedAt, &run.CompletedAt, &run.FailedAt,
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

// LockThreadRow 在调度阶段为 thread 加锁，避免多路并发创建 run。
func (r *RunEventRepository) LockThreadRow(ctx context.Context, threadID uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if threadID == uuid.Nil {
		return fmt.Errorf("thread_id must not be empty")
	}
	var lockedID uuid.UUID
	err := r.db.QueryRow(
		ctx,
		`SELECT id
		 FROM threads
		 WHERE id = $1
		   AND deleted_at IS NULL
		 FOR UPDATE`,
		threadID,
	).Scan(&lockedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("thread not found: %s", threadID)
		}
		return err
	}
	return nil
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

func (r *RunEventRepository) HasRecoverableAssistantOutput(
	ctx context.Context,
	runID uuid.UUID,
) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if runID == uuid.Nil {
		return false, fmt.Errorf("run_id must not be empty")
	}
	eventType, err := r.GetLatestEventType(ctx, runID, []string{
		"message.delta",
		"tool.call",
		"tool.result",
	})
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(eventType) != "", nil
}

func (r *RunEventRepository) RequestCancel(
	ctx context.Context,
	runID uuid.UUID,
	requestedByUserID *uuid.UUID,
	traceID string,
	lastSeenSeq int64,
	clientCancelledAt *time.Time,
) (*RunEvent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if runID == uuid.Nil {
		return nil, fmt.Errorf("run_id must not be empty")
	}
	if lastSeenSeq < 0 {
		return nil, fmt.Errorf("last_seen_seq must be non-negative")
	}

	if err := r.lockRunRow(ctx, runID); err != nil {
		return nil, err
	}

	terminal, err := r.GetLatestEventType(ctx, runID, []string{"run.completed", "run.failed", "run.cancelled", "run.interrupted"})
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

	dataJSON := map[string]any{
		"trace_id":           traceID,
		"last_seen_seq":      lastSeenSeq,
		"visible_seq_cutoff": lastSeenSeq,
	}
	if requestedByUserID != nil && *requestedByUserID != uuid.Nil {
		dataJSON["requested_by_user_id"] = requestedByUserID.String()
	}
	if clientCancelledAt != nil {
		dataJSON["client_cancelled_at"] = clientCancelledAt.UTC().Format(time.RFC3339Nano)
	}

	now := time.Now().UTC()
	if _, err := r.db.Exec(ctx,
		`UPDATE runs
		 SET status = 'cancelling',
		     status_updated_at = $2
		 WHERE id = $1`,
		runID,
		now,
	); err != nil {
		return nil, err
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

	terminal, err := r.GetLatestEventType(ctx, runID, []string{"run.completed", "run.failed", "run.cancelled", "run.interrupted"})
	if err != nil {
		return nil, err
	}
	if terminal != "" {
		return nil, RunNotActiveError{RunID: runID}
	}

	cancelType, err := r.GetLatestEventType(ctx, runID, []string{"run.cancel_requested", "run.cancelled"})
	if err != nil {
		return nil, err
	}
	if cancelType == "run.cancel_requested" || cancelType == "run.cancelled" {
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

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
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
	AccountID      *uuid.UUID
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
		        r.parent_run_id, r.resume_from_run_id, r.status_updated_at, r.completed_at, r.failed_at,
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
			&rw.ParentRunID, &rw.ResumeFromRunID, &rw.StatusUpdatedAt, &rw.CompletedAt, &rw.FailedAt,
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

// ListStaleRunning 查询所有 status='running'/'cancelling' 且最后活跃时间早于 staleBefore 的 run。
func (r *RunEventRepository) ListStaleRunning(ctx context.Context, staleBefore time.Time) ([]Run, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT id, account_id, thread_id, created_by_user_id, status, created_at,
		        parent_run_id, resume_from_run_id, status_updated_at, completed_at, failed_at,
		        duration_ms, total_input_tokens, total_output_tokens, total_cost_usd,
		        model, persona_id, deleted_at
		 FROM runs
		 WHERE status IN ('running', 'cancelling')
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
			&run.ParentRunID, &run.ResumeFromRunID, &run.StatusUpdatedAt, &run.CompletedAt, &run.FailedAt,
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

// ForceFailRun 原子地将一个 running/cancelling 的 run 标记为 failed 并写入 run.failed 事件。
// 返回 (true, nil) 表示实际执行了更新；(false, nil) 表示 run 已不在运行态（no-op）。
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
		       AND status IN ('running', 'cancelling')
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
