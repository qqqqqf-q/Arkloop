//go:build !desktop

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

const (
	SubAgentStatusCreated      = "created"
	SubAgentStatusQueued       = "queued"
	SubAgentStatusRunning      = "running"
	SubAgentStatusWaitingInput = "waiting_input"
	SubAgentStatusCompleted    = "completed"
	SubAgentStatusFailed       = "failed"
	SubAgentStatusCancelled    = "cancelled"
	SubAgentStatusClosed       = "closed"
	SubAgentStatusResumable    = "resumable"

	SubAgentSourceTypeThreadSpawn         = "thread_spawn"
	SubAgentSourceTypeReview              = "review"
	SubAgentSourceTypeMemoryConsolidation = "memory_consolidation"
	SubAgentSourceTypeAgentJob            = "agent_job"
	SubAgentSourceTypePlatformAgent       = "platform_agent"
	SubAgentSourceTypeOther               = "other"

	SubAgentContextModeIsolated            = "isolated"
	SubAgentContextModeForkRecent          = "fork_recent"
	SubAgentContextModeForkThread          = "fork_thread"
	SubAgentContextModeForkSelected        = "fork_selected"
	SubAgentContextModeSharedWorkspaceOnly = "shared_workspace_only"
)

type SubAgentRecord struct {
	ID                 uuid.UUID
	AccountID          uuid.UUID
	ParentRunID        uuid.UUID
	ParentThreadID     uuid.UUID
	RootRunID          uuid.UUID
	RootThreadID       uuid.UUID
	Depth              int
	Role               *string
	PersonaID          *string
	Nickname           *string
	SourceType         string
	ContextMode        string
	Status             string
	CurrentRunID       *uuid.UUID
	LastCompletedRunID *uuid.UUID
	LastOutputRef      *string
	LastError          *string
	CreatedAt          time.Time
	StartedAt          *time.Time
	CompletedAt        *time.Time
	ClosedAt           *time.Time
}

type SubAgentCreateParams struct {
	ID             uuid.UUID
	AccountID      uuid.UUID
	ParentRunID    uuid.UUID
	ParentThreadID uuid.UUID
	RootRunID      uuid.UUID
	RootThreadID   uuid.UUID
	Depth          int
	Role           *string
	PersonaID      *string
	Nickname       *string
	SourceType     string
	ContextMode    string
}

type SubAgentRepository struct{}

func (SubAgentRepository) Create(ctx context.Context, tx pgx.Tx, params SubAgentCreateParams) (SubAgentRecord, error) {
	if tx == nil {
		return SubAgentRecord{}, fmt.Errorf("tx must not be nil")
	}
	if params.ID == uuid.Nil {
		params.ID = uuid.New()
	}
	if params.AccountID == uuid.Nil {
		return SubAgentRecord{}, fmt.Errorf("account_id must not be empty")
	}
	if params.ParentRunID == uuid.Nil {
		return SubAgentRecord{}, fmt.Errorf("parent_run_id must not be empty")
	}
	if params.ParentThreadID == uuid.Nil {
		return SubAgentRecord{}, fmt.Errorf("parent_thread_id must not be empty")
	}
	if params.RootRunID == uuid.Nil {
		return SubAgentRecord{}, fmt.Errorf("root_run_id must not be empty")
	}
	if params.RootThreadID == uuid.Nil {
		return SubAgentRecord{}, fmt.Errorf("root_thread_id must not be empty")
	}
	if params.Depth < 0 {
		return SubAgentRecord{}, fmt.Errorf("depth must not be negative")
	}
	if err := validateSubAgentSourceType(params.SourceType); err != nil {
		return SubAgentRecord{}, err
	}
	if err := validateSubAgentContextMode(params.ContextMode); err != nil {
		return SubAgentRecord{}, err
	}

	return scanSubAgent(tx.QueryRow(
		ctx,
		`INSERT INTO sub_agents (
			id, account_id, parent_run_id, parent_thread_id, root_run_id, root_thread_id,
			depth, role, persona_id, nickname, source_type, context_mode, status
		 ) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11, $12, $13
		 )
		 RETURNING id, account_id, parent_run_id, parent_thread_id, root_run_id, root_thread_id,
		           depth, role, persona_id, nickname, source_type, context_mode, status,
		           current_run_id, last_completed_run_id, last_output_ref, last_error,
		           created_at, started_at, completed_at, closed_at`,
		params.ID,
		params.AccountID,
		params.ParentRunID,
		params.ParentThreadID,
		params.RootRunID,
		params.RootThreadID,
		params.Depth,
		normalizeSubAgentOptionalString(params.Role),
		normalizeSubAgentOptionalString(params.PersonaID),
		normalizeSubAgentOptionalString(params.Nickname),
		params.SourceType,
		params.ContextMode,
		SubAgentStatusCreated,
	))
}

func (SubAgentRepository) Get(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*SubAgentRecord, error) {
	if tx == nil {
		return nil, fmt.Errorf("tx must not be nil")
	}
	if id == uuid.Nil {
		return nil, fmt.Errorf("id must not be empty")
	}
	return scanSubAgentNullable(tx.QueryRow(ctx, subAgentSelectBy+` WHERE id = $1 LIMIT 1`, id))
}

func (SubAgentRepository) GetByCurrentRunID(ctx context.Context, tx pgx.Tx, runID uuid.UUID) (*SubAgentRecord, error) {
	if tx == nil {
		return nil, fmt.Errorf("tx must not be nil")
	}
	if runID == uuid.Nil {
		return nil, fmt.Errorf("run_id must not be empty")
	}
	return scanSubAgentNullable(tx.QueryRow(ctx, subAgentSelectBy+` WHERE current_run_id = $1 LIMIT 1`, runID))
}

func (SubAgentRepository) ListByParentRun(ctx context.Context, db QueryDB, parentRunID uuid.UUID) ([]SubAgentRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("pool must not be nil")
	}
	if parentRunID == uuid.Nil {
		return nil, fmt.Errorf("parent_run_id must not be empty")
	}
	rows, err := db.Query(ctx, subAgentSelectBy+` WHERE parent_run_id = $1 ORDER BY created_at ASC, id ASC`, parentRunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]SubAgentRecord, 0)
	for rows.Next() {
		record, err := scanSubAgentFromRows(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (repo SubAgentRepository) TransitionToQueued(ctx context.Context, tx pgx.Tx, id uuid.UUID, runID uuid.UUID) error {
	if tx == nil {
		return fmt.Errorf("tx must not be nil")
	}
	if id == uuid.Nil {
		return fmt.Errorf("id must not be empty")
	}
	if runID == uuid.Nil {
		return fmt.Errorf("run_id must not be empty")
	}
	record, err := repo.getForUpdateByID(ctx, tx, id)
	if err != nil {
		return err
	}
	if record == nil {
		return fmt.Errorf("sub_agent not found: %s", id)
	}
	if err := validateSubAgentStatusTransition(record.Status, SubAgentStatusQueued); err != nil {
		return err
	}
	_, err = tx.Exec(ctx,
		`UPDATE sub_agents
		 SET status = $2,
		     current_run_id = $3,
		     last_error = NULL,
		     started_at = NULL,
		     completed_at = NULL
		 WHERE id = $1`,
		id,
		SubAgentStatusQueued,
		runID,
	)
	return err
}

func (repo SubAgentRepository) TransitionToRunning(ctx context.Context, tx pgx.Tx, runID uuid.UUID) error {
	if tx == nil {
		return fmt.Errorf("tx must not be nil")
	}
	if runID == uuid.Nil {
		return fmt.Errorf("run_id must not be empty")
	}
	record, err := repo.getForUpdateByCurrentRunID(ctx, tx, runID)
	if err != nil {
		return err
	}
	if record == nil {
		return nil
	}
	if record.Status == SubAgentStatusRunning {
		return nil
	}
	if err := validateSubAgentStatusTransition(record.Status, SubAgentStatusRunning); err != nil {
		return err
	}
	_, err = tx.Exec(ctx,
		`UPDATE sub_agents
		 SET status = $2,
		     started_at = COALESCE(started_at, now())
		 WHERE id = $1`,
		record.ID,
		SubAgentStatusRunning,
	)
	return err
}

func (repo SubAgentRepository) TransitionToTerminal(ctx context.Context, tx pgx.Tx, runID uuid.UUID, status string, lastError *string) error {
	if tx == nil {
		return fmt.Errorf("tx must not be nil")
	}
	if runID == uuid.Nil {
		return fmt.Errorf("run_id must not be empty")
	}
	if err := validateSubAgentStatus(status); err != nil {
		return err
	}
	record, err := repo.getForUpdateByCurrentRunID(ctx, tx, runID)
	if err != nil {
		return err
	}
	if record == nil {
		return nil
	}
	if err := validateSubAgentStatusTransition(record.Status, status); err != nil {
		return err
	}

	var completedRunID *uuid.UUID
	if status == SubAgentStatusCompleted {
		completedRunID = &runID
		lastError = nil
	}

	_, err = tx.Exec(ctx,
		`UPDATE sub_agents
		 SET status = $2,
		     current_run_id = NULL,
		     last_completed_run_id = CASE WHEN $3::uuid IS NULL THEN last_completed_run_id ELSE $3::uuid END,
		     last_error = $4,
		     completed_at = now()
		 WHERE id = $1`,
		record.ID,
		status,
		completedRunID,
		normalizeSubAgentOptionalString(lastError),
	)
	return err
}

func (repo SubAgentRepository) TransitionToResumable(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	if tx == nil {
		return fmt.Errorf("tx must not be nil")
	}
	if id == uuid.Nil {
		return fmt.Errorf("id must not be empty")
	}
	record, err := repo.getForUpdateByID(ctx, tx, id)
	if err != nil {
		return err
	}
	if record == nil {
		return fmt.Errorf("sub_agent not found: %s", id)
	}
	if record.Status == SubAgentStatusResumable {
		return nil
	}
	if err := validateSubAgentStatusTransition(record.Status, SubAgentStatusResumable); err != nil {
		return err
	}
	_, err = tx.Exec(ctx,
		`UPDATE sub_agents
		 SET status = $2
		 WHERE id = $1`,
		id,
		SubAgentStatusResumable,
	)
	return err
}

func (repo SubAgentRepository) TransitionToClosed(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	if tx == nil {
		return fmt.Errorf("tx must not be nil")
	}
	if id == uuid.Nil {
		return fmt.Errorf("id must not be empty")
	}
	record, err := repo.getForUpdateByID(ctx, tx, id)
	if err != nil {
		return err
	}
	if record == nil {
		return fmt.Errorf("sub_agent not found: %s", id)
	}
	if record.Status == SubAgentStatusClosed {
		return nil
	}
	if err := validateSubAgentStatusTransition(record.Status, SubAgentStatusClosed); err != nil {
		return err
	}
	_, err = tx.Exec(ctx,
		`UPDATE sub_agents
		 SET status = $2,
		     current_run_id = NULL,
		     closed_at = COALESCE(closed_at, now())
		 WHERE id = $1`,
		id,
		SubAgentStatusClosed,
	)
	return err
}

func (repo SubAgentRepository) SetLastOutputRefByLastCompletedRunID(ctx context.Context, tx pgx.Tx, runID uuid.UUID, outputRef string) error {
	if tx == nil {
		return fmt.Errorf("tx must not be nil")
	}
	if runID == uuid.Nil {
		return fmt.Errorf("run_id must not be empty")
	}
	if strings.TrimSpace(outputRef) == "" {
		return fmt.Errorf("output_ref must not be empty")
	}
	_, err := tx.Exec(ctx,
		`UPDATE sub_agents
		 SET last_output_ref = $2
		 WHERE last_completed_run_id = $1`,
		runID,
		strings.TrimSpace(outputRef),
	)
	return err
}

func (SubAgentRepository) CountActiveByRootRun(ctx context.Context, tx pgx.Tx, rootRunID uuid.UUID) (int, error) {
	if tx == nil {
		return 0, fmt.Errorf("tx must not be nil")
	}
	if rootRunID == uuid.Nil {
		return 0, fmt.Errorf("root_run_id must not be empty")
	}
	var count int
	err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM sub_agents
		 WHERE root_run_id = $1
		   AND status IN ($2, $3, $4)`,
		rootRunID,
		SubAgentStatusCreated,
		SubAgentStatusQueued,
		SubAgentStatusRunning,
	).Scan(&count)
	return count, err
}

func (SubAgentRepository) CountActiveByParentRun(ctx context.Context, tx pgx.Tx, parentRunID uuid.UUID) (int, error) {
	if tx == nil {
		return 0, fmt.Errorf("tx must not be nil")
	}
	if parentRunID == uuid.Nil {
		return 0, fmt.Errorf("parent_run_id must not be empty")
	}
	var count int
	err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM sub_agents
		 WHERE parent_run_id = $1
		   AND status IN ($2, $3, $4)`,
		parentRunID,
		SubAgentStatusCreated,
		SubAgentStatusQueued,
		SubAgentStatusRunning,
	).Scan(&count)
	return count, err
}

func (SubAgentRepository) CountByRootRun(ctx context.Context, tx pgx.Tx, rootRunID uuid.UUID) (int, error) {
	if tx == nil {
		return 0, fmt.Errorf("tx must not be nil")
	}
	if rootRunID == uuid.Nil {
		return 0, fmt.Errorf("root_run_id must not be empty")
	}
	var count int
	err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM sub_agents
		 WHERE root_run_id = $1`,
		rootRunID,
	).Scan(&count)
	return count, err
}

func (repo SubAgentRepository) getForUpdateByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*SubAgentRecord, error) {
	return scanSubAgentNullable(tx.QueryRow(ctx, subAgentSelectBy+` WHERE id = $1 FOR UPDATE`, id))
}

func (repo SubAgentRepository) getForUpdateByCurrentRunID(ctx context.Context, tx pgx.Tx, runID uuid.UUID) (*SubAgentRecord, error) {
	return scanSubAgentNullable(tx.QueryRow(ctx, subAgentSelectBy+` WHERE current_run_id = $1 FOR UPDATE`, runID))
}

const subAgentSelectBy = `SELECT id, account_id, parent_run_id, parent_thread_id, root_run_id, root_thread_id,
	       depth, role, persona_id, nickname, source_type, context_mode, status,
	       current_run_id, last_completed_run_id, last_output_ref, last_error,
	       created_at, started_at, completed_at, closed_at
	  FROM sub_agents`

func scanSubAgent(row pgx.Row) (SubAgentRecord, error) {
	var record SubAgentRecord
	err := row.Scan(
		&record.ID,
		&record.AccountID,
		&record.ParentRunID,
		&record.ParentThreadID,
		&record.RootRunID,
		&record.RootThreadID,
		&record.Depth,
		&record.Role,
		&record.PersonaID,
		&record.Nickname,
		&record.SourceType,
		&record.ContextMode,
		&record.Status,
		&record.CurrentRunID,
		&record.LastCompletedRunID,
		&record.LastOutputRef,
		&record.LastError,
		&record.CreatedAt,
		&record.StartedAt,
		&record.CompletedAt,
		&record.ClosedAt,
	)
	return record, err
}

func scanSubAgentNullable(row pgx.Row) (*SubAgentRecord, error) {
	record, err := scanSubAgent(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

func scanSubAgentFromRows(rows pgx.Rows) (SubAgentRecord, error) {
	var record SubAgentRecord
	err := rows.Scan(
		&record.ID,
		&record.AccountID,
		&record.ParentRunID,
		&record.ParentThreadID,
		&record.RootRunID,
		&record.RootThreadID,
		&record.Depth,
		&record.Role,
		&record.PersonaID,
		&record.Nickname,
		&record.SourceType,
		&record.ContextMode,
		&record.Status,
		&record.CurrentRunID,
		&record.LastCompletedRunID,
		&record.LastOutputRef,
		&record.LastError,
		&record.CreatedAt,
		&record.StartedAt,
		&record.CompletedAt,
		&record.ClosedAt,
	)
	return record, err
}

func validateSubAgentStatus(status string) error {
	switch status {
	case SubAgentStatusCreated,
		SubAgentStatusQueued,
		SubAgentStatusRunning,
		SubAgentStatusWaitingInput,
		SubAgentStatusCompleted,
		SubAgentStatusFailed,
		SubAgentStatusCancelled,
		SubAgentStatusClosed,
		SubAgentStatusResumable:
		return nil
	default:
		return fmt.Errorf("invalid sub_agent status: %s", status)
	}
}

func validateSubAgentStatusTransition(from string, to string) error {
	if err := validateSubAgentStatus(from); err != nil {
		return err
	}
	if err := validateSubAgentStatus(to); err != nil {
		return err
	}
	allowed := map[string]map[string]struct{}{
		SubAgentStatusCreated: {
			SubAgentStatusQueued: {},
		},
		SubAgentStatusQueued: {
			SubAgentStatusRunning: {},
			SubAgentStatusFailed:  {},
			SubAgentStatusClosed:  {},
		},
		SubAgentStatusRunning: {
			SubAgentStatusCompleted: {},
			SubAgentStatusFailed:    {},
			SubAgentStatusCancelled: {},
		},
		SubAgentStatusCompleted: {
			SubAgentStatusQueued:    {},
			SubAgentStatusResumable: {},
			SubAgentStatusClosed:    {},
		},
		SubAgentStatusFailed: {
			SubAgentStatusQueued: {},
			SubAgentStatusClosed: {},
		},
		SubAgentStatusCancelled: {
			SubAgentStatusQueued: {},
			SubAgentStatusClosed: {},
		},
		SubAgentStatusWaitingInput: {
			SubAgentStatusQueued:    {},
			SubAgentStatusResumable: {},
			SubAgentStatusClosed:    {},
		},
		SubAgentStatusResumable: {
			SubAgentStatusQueued: {},
			SubAgentStatusClosed: {},
		},
	}
	if _, ok := allowed[from][to]; !ok {
		return fmt.Errorf("invalid sub_agent status transition: %s -> %s", from, to)
	}
	return nil
}

func validateSubAgentSourceType(sourceType string) error {
	switch strings.TrimSpace(sourceType) {
	case SubAgentSourceTypeThreadSpawn,
		SubAgentSourceTypeReview,
		SubAgentSourceTypeMemoryConsolidation,
		SubAgentSourceTypeAgentJob,
		SubAgentSourceTypePlatformAgent,
		SubAgentSourceTypeOther:
		return nil
	default:
		return fmt.Errorf("invalid sub_agent source_type: %s", sourceType)
	}
}

func validateSubAgentContextMode(contextMode string) error {
	switch strings.TrimSpace(contextMode) {
	case SubAgentContextModeIsolated,
		SubAgentContextModeForkRecent,
		SubAgentContextModeForkThread,
		SubAgentContextModeForkSelected,
		SubAgentContextModeSharedWorkspaceOnly:
		return nil
	default:
		return fmt.Errorf("invalid sub_agent context_mode: %s", contextMode)
	}
}

func normalizeSubAgentOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
