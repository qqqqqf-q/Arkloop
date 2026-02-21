package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Run struct {
	ID              uuid.UUID
	OrgID           uuid.UUID
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
	SkillID           *string
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
	orgID uuid.UUID,
	threadID uuid.UUID,
	createdByUserID *uuid.UUID,
	startedType string,
	startedData map[string]any,
) (Run, RunEvent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil {
		return Run{}, RunEvent{}, fmt.Errorf("org_id must not be empty")
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
		`INSERT INTO runs (org_id, thread_id, created_by_user_id, status)
		 VALUES ($1, $2, $3, 'running')
		 RETURNING id, org_id, thread_id, created_by_user_id, status, created_at,
		           parent_run_id, status_updated_at, completed_at, failed_at,
		           duration_ms, total_input_tokens, total_output_tokens, total_cost_usd,
		           model, skill_id, deleted_at`,
		orgID,
		threadID,
		createdByUserID,
	).Scan(
		&run.ID, &run.OrgID, &run.ThreadID, &run.CreatedByUserID, &run.Status, &run.CreatedAt,
		&run.ParentRunID, &run.StatusUpdatedAt, &run.CompletedAt, &run.FailedAt,
		&run.DurationMs, &run.TotalInputTokens, &run.TotalOutputTokens, &run.TotalCostUSD,
		&run.Model, &run.SkillID, &run.DeletedAt,
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
		`SELECT id, org_id, thread_id, created_by_user_id, status, created_at,
		        parent_run_id, status_updated_at, completed_at, failed_at,
		        duration_ms, total_input_tokens, total_output_tokens, total_cost_usd,
		        model, skill_id, deleted_at
		 FROM runs
		 WHERE id = $1
		 LIMIT 1`,
		runID,
	).Scan(
		&run.ID, &run.OrgID, &run.ThreadID, &run.CreatedByUserID, &run.Status, &run.CreatedAt,
		&run.ParentRunID, &run.StatusUpdatedAt, &run.CompletedAt, &run.FailedAt,
		&run.DurationMs, &run.TotalInputTokens, &run.TotalOutputTokens, &run.TotalCostUSD,
		&run.Model, &run.SkillID, &run.DeletedAt,
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
	orgID uuid.UUID,
	threadID uuid.UUID,
	limit int,
) ([]Run, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil {
		return nil, fmt.Errorf("org_id must not be empty")
	}
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("thread_id must not be empty")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be positive")
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT id, org_id, thread_id, created_by_user_id, status, created_at,
		        parent_run_id, status_updated_at, completed_at, failed_at,
		        duration_ms, total_input_tokens, total_output_tokens, total_cost_usd,
		        model, skill_id, deleted_at
		 FROM runs
		 WHERE org_id = $1
		   AND thread_id = $2
		 ORDER BY created_at DESC, id DESC
		 LIMIT $3`,
		orgID,
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
			&run.ID, &run.OrgID, &run.ThreadID, &run.CreatedByUserID, &run.Status, &run.CreatedAt,
			&run.ParentRunID, &run.StatusUpdatedAt, &run.CompletedAt, &run.FailedAt,
			&run.DurationMs, &run.TotalInputTokens, &run.TotalOutputTokens, &run.TotalCostUSD,
			&run.Model, &run.SkillID, &run.DeletedAt,
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

func (r *RunEventRepository) allocateSeq(ctx context.Context, runID uuid.UUID) (int64, error) {
	var nextSeq int64
	err := r.db.QueryRow(
		ctx,
		`UPDATE runs
		 SET next_event_seq = next_event_seq + 1
		 WHERE id = $1
		 RETURNING next_event_seq`,
		runID,
	).Scan(&nextSeq)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, RunNotFoundError{RunID: runID}
		}
		return 0, err
	}
	return nextSeq - 1, nil
}

func mapOrEmpty(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}
