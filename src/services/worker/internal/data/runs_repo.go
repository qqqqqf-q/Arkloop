package data

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TerminalStatusUpdate 携带终态写入所需的所有字段，供 R30 的 eventWriter 使用。
type TerminalStatusUpdate struct {
	// Status 必须是 'completed'、'failed' 或 'cancelled' 之一
	Status            string
	TotalInputTokens  int64
	TotalOutputTokens int64
	TotalCostUSD      float64
}

type Run struct {
	ID              uuid.UUID
	OrgID           uuid.UUID
	ThreadID        uuid.UUID
	ProjectID       *uuid.UUID
	ParentRunID     *uuid.UUID // nil 表示顶级 Run，非 nil 表示子 Run
	CreatedByUserID *uuid.UUID // nil 表示系统触发或用户已删除，Memory 层按此隔离
	ProfileRef      *string
	WorkspaceRef    *string
}

type RunLineage struct {
	RootRunID    uuid.UUID
	RootThreadID uuid.UUID
	Depth        int
}

type RunsRepository struct{}

// UpdateRunMetadata 写入 runs.model / runs.persona_id，用于列表展示与筛选。
func (RunsRepository) UpdateRunMetadata(
	ctx context.Context,
	tx pgx.Tx,
	runID uuid.UUID,
	model string,
	personaID string,
) error {
	if runID == uuid.Nil {
		return fmt.Errorf("run_id must not be empty")
	}
	tag, err := tx.Exec(ctx,
		`UPDATE runs
		 SET model = $2,
		     persona_id = $3
		 WHERE id = $1`,
		runID,
		model,
		personaID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("run not found: %s", runID)
	}
	return nil
}

func (RunsRepository) GetRun(ctx context.Context, tx pgx.Tx, runID uuid.UUID) (*Run, error) {
	var run Run
	err := tx.QueryRow(
		ctx,
		`SELECT r.id,
		        r.org_id,
		        r.thread_id,
		        t.project_id,
		        r.parent_run_id,
		        r.created_by_user_id,
		        r.profile_ref,
		        r.workspace_ref
		   FROM runs r
		   LEFT JOIN threads t ON t.id = r.thread_id
		  WHERE r.id = $1
		  LIMIT 1`,
		runID,
	).Scan(&run.ID, &run.OrgID, &run.ThreadID, &run.ProjectID, &run.ParentRunID, &run.CreatedByUserID, &run.ProfileRef, &run.WorkspaceRef)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &run, nil
}

func (RunsRepository) GetLineage(ctx context.Context, tx pgx.Tx, runID uuid.UUID) (RunLineage, error) {
	if tx == nil {
		return RunLineage{}, fmt.Errorf("tx must not be nil")
	}
	if runID == uuid.Nil {
		return RunLineage{}, fmt.Errorf("run_id must not be empty")
	}

	var lineage RunLineage
	err := tx.QueryRow(ctx,
		`WITH RECURSIVE lineage AS (
			SELECT id, parent_run_id, thread_id, 0 AS depth
			  FROM runs
			 WHERE id = $1
			UNION ALL
			SELECT parent.id, parent.parent_run_id, parent.thread_id, lineage.depth + 1
			  FROM runs parent
			  JOIN lineage ON lineage.parent_run_id = parent.id
		)
		SELECT id, thread_id, depth
		  FROM lineage
		 WHERE parent_run_id IS NULL
		 ORDER BY depth DESC
		 LIMIT 1`,
		runID,
	).Scan(&lineage.RootRunID, &lineage.RootThreadID, &lineage.Depth)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RunLineage{}, fmt.Errorf("run not found: %s", runID)
		}
		return RunLineage{}, err
	}
	return lineage, nil
}

func (RunsRepository) UpdateEnvironmentBindings(
	ctx context.Context,
	tx pgx.Tx,
	runID uuid.UUID,
	profileRef string,
	workspaceRef string,
) error {
	if runID == uuid.Nil {
		return fmt.Errorf("run_id must not be empty")
	}
	if strings.TrimSpace(profileRef) == "" {
		return fmt.Errorf("profile_ref must not be empty")
	}
	if strings.TrimSpace(workspaceRef) == "" {
		return fmt.Errorf("workspace_ref must not be empty")
	}
	tag, err := tx.Exec(
		ctx,
		`UPDATE runs
		    SET profile_ref = $2,
		        workspace_ref = $3
		  WHERE id = $1`,
		runID,
		profileRef,
		workspaceRef,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("run not found: %s", runID)
	}
	return nil
}

func (RunsRepository) LockRunRow(ctx context.Context, tx pgx.Tx, runID uuid.UUID) error {
	var ignored int
	err := tx.QueryRow(
		ctx,
		`SELECT 1
		 FROM runs
		 WHERE id = $1
		 FOR UPDATE`,
		runID,
	).Scan(&ignored)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("run not found: %s", runID)
		}
		return err
	}
	return nil
}

// UpdateRunTerminalStatus 在终态事件提交时同步更新 runs 的生命周期字段。
// 由 R30 的 eventWriter 在同一事务内调用。
func (RunsRepository) UpdateRunTerminalStatus(
	ctx context.Context,
	tx pgx.Tx,
	runID uuid.UUID,
	u TerminalStatusUpdate,
) error {
	tag, err := tx.Exec(ctx,
		`UPDATE runs
		 SET status              = $2,
		     status_updated_at   = now(),
		     completed_at        = CASE WHEN $2 = 'completed' THEN now() ELSE completed_at END,
		     failed_at           = CASE WHEN $2 = 'failed'    THEN now() ELSE failed_at    END,
		     duration_ms         = EXTRACT(EPOCH FROM (now() - created_at)) * 1000,
		     total_input_tokens  = $3,
		     total_output_tokens = $4,
		     total_cost_usd      = $5
		 WHERE id = $1`,
		runID,
		u.Status,
		u.TotalInputTokens,
		u.TotalOutputTokens,
		u.TotalCostUSD,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("run not found: %s", runID)
	}
	return nil
}
