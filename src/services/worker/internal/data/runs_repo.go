package data

import (
	"context"
	"errors"
	"fmt"

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
	ID          uuid.UUID
	OrgID       uuid.UUID
	ThreadID    uuid.UUID
	ParentRunID *uuid.UUID // nil 表示顶级 Run，非 nil 表示子 Run
}

type RunsRepository struct{}

func (RunsRepository) GetRun(ctx context.Context, tx pgx.Tx, runID uuid.UUID) (*Run, error) {
	var run Run
	err := tx.QueryRow(
		ctx,
		`SELECT id, org_id, thread_id, parent_run_id
		 FROM runs
		 WHERE id = $1
		 LIMIT 1`,
		runID,
	).Scan(&run.ID, &run.OrgID, &run.ThreadID, &run.ParentRunID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &run, nil
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
