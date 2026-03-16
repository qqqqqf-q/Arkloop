//go:build desktop

package data

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DesktopRunsRepository provides SQLite-compatible run persistence.
type DesktopRunsRepository struct{}

func (DesktopRunsRepository) GetRun(ctx context.Context, tx pgx.Tx, runID uuid.UUID) (*Run, error) {
	var run Run
	err := tx.QueryRow(ctx,
		`SELECT r.id, r.account_id, r.thread_id,
		        t.project_id, r.parent_run_id,
		        r.created_by_user_id, r.profile_ref, r.workspace_ref
		   FROM runs r
		   LEFT JOIN threads t ON t.id = r.thread_id
		  WHERE r.id = $1
		  LIMIT 1`,
		runID,
	).Scan(&run.ID, &run.AccountID, &run.ThreadID, &run.ProjectID,
		&run.ParentRunID, &run.CreatedByUserID, &run.ProfileRef, &run.WorkspaceRef)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &run, nil
}

// LockRunRow is a no-op in SQLite (WAL mode, single writer).
func (DesktopRunsRepository) LockRunRow(_ context.Context, tx pgx.Tx, runID uuid.UUID) error {
	var exists int
	err := tx.QueryRow(context.Background(),
		`SELECT 1 FROM runs WHERE id = $1`,
		runID,
	).Scan(&exists)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("run not found: %s", runID)
		}
		return err
	}
	return nil
}

func (DesktopRunsRepository) UpdateRunMetadata(
	ctx context.Context, tx pgx.Tx, runID uuid.UUID, model string, personaID string,
) error {
	if runID == uuid.Nil {
		return fmt.Errorf("run_id must not be empty")
	}
	tag, err := tx.Exec(ctx,
		`UPDATE runs SET model = $2, persona_id = $3 WHERE id = $1`,
		runID, model, personaID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("run not found: %s", runID)
	}
	return nil
}

// UpdateRunTerminalStatus uses SQLite-compatible time arithmetic.
func (DesktopRunsRepository) UpdateRunTerminalStatus(
	ctx context.Context, tx pgx.Tx, runID uuid.UUID, u TerminalStatusUpdate,
) error {
	tag, err := tx.Exec(ctx,
		`UPDATE runs
		 SET status              = $2,
		     status_updated_at   = datetime('now'),
		     completed_at        = CASE WHEN $2 = 'completed' THEN datetime('now') ELSE completed_at END,
		     failed_at           = CASE WHEN $2 = 'failed'    THEN datetime('now') ELSE failed_at    END,
		     duration_ms         = CAST((julianday('now') - julianday(created_at)) * 86400000 AS INTEGER),
		     total_input_tokens  = $3,
		     total_output_tokens = $4,
		     total_cost_usd      = $5
		 WHERE id = $1`,
		runID, u.Status, u.TotalInputTokens, u.TotalOutputTokens, u.TotalCostUSD,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("run not found: %s", runID)
	}
	return nil
}
