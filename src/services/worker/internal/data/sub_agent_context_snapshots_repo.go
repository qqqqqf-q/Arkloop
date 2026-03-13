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

type SubAgentContextSnapshotRecord struct {
	SubAgentID   uuid.UUID
	SnapshotJSON json.RawMessage
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type SubAgentContextSnapshotsRepository struct{}

func (SubAgentContextSnapshotsRepository) Upsert(ctx context.Context, tx pgx.Tx, subAgentID uuid.UUID, snapshotJSON []byte) error {
	if tx == nil {
		return fmt.Errorf("tx must not be nil")
	}
	if subAgentID == uuid.Nil {
		return fmt.Errorf("sub_agent_id must not be empty")
	}
	if len(snapshotJSON) == 0 {
		return fmt.Errorf("snapshot_json must not be empty")
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO sub_agent_context_snapshots (sub_agent_id, snapshot_json)
		 VALUES ($1, $2::jsonb)
		 ON CONFLICT (sub_agent_id)
		 DO UPDATE SET snapshot_json = EXCLUDED.snapshot_json,
		               updated_at = now()`,
		subAgentID,
		string(snapshotJSON),
	)
	return err
}

func (SubAgentContextSnapshotsRepository) GetBySubAgentID(ctx context.Context, tx pgx.Tx, subAgentID uuid.UUID) (*SubAgentContextSnapshotRecord, error) {
	if tx == nil {
		return nil, fmt.Errorf("tx must not be nil")
	}
	if subAgentID == uuid.Nil {
		return nil, fmt.Errorf("sub_agent_id must not be empty")
	}
	var record SubAgentContextSnapshotRecord
	err := tx.QueryRow(ctx,
		`SELECT sub_agent_id, snapshot_json, created_at, updated_at
		   FROM sub_agent_context_snapshots
		  WHERE sub_agent_id = $1
		  LIMIT 1`,
		subAgentID,
	).Scan(&record.SubAgentID, &record.SnapshotJSON, &record.CreatedAt, &record.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

func (SubAgentContextSnapshotsRepository) GetByCurrentRunID(ctx context.Context, tx pgx.Tx, runID uuid.UUID) (*SubAgentContextSnapshotRecord, error) {
	if tx == nil {
		return nil, fmt.Errorf("tx must not be nil")
	}
	if runID == uuid.Nil {
		return nil, fmt.Errorf("run_id must not be empty")
	}
	var record SubAgentContextSnapshotRecord
	err := tx.QueryRow(ctx,
		`SELECT cs.sub_agent_id, cs.snapshot_json, cs.created_at, cs.updated_at
		   FROM sub_agent_context_snapshots cs
		   JOIN sub_agents sa ON sa.id = cs.sub_agent_id
		  WHERE sa.current_run_id = $1
		  LIMIT 1`,
		runID,
	).Scan(&record.SubAgentID, &record.SnapshotJSON, &record.CreatedAt, &record.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}
