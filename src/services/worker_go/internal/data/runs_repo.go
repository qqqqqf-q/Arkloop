package data

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Run struct {
	ID       uuid.UUID
	OrgID    uuid.UUID
	ThreadID uuid.UUID
}

type RunsRepository struct{}

func (RunsRepository) GetRun(ctx context.Context, tx pgx.Tx, runID uuid.UUID) (*Run, error) {
	var run Run
	err := tx.QueryRow(
		ctx,
		`SELECT id, org_id, thread_id
		 FROM runs
		 WHERE id = $1
		 LIMIT 1`,
		runID,
	).Scan(&run.ID, &run.OrgID, &run.ThreadID)
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
			return fmt.Errorf("run 不存在: %s", runID)
		}
		return err
	}
	return nil
}
