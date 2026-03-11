package data

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type SubAgentPendingInputRecord struct {
	ID         uuid.UUID
	SubAgentID uuid.UUID
	Seq        int64
	Input      string
	Priority   bool
	CreatedAt  time.Time
}

type SubAgentPendingInputsRepository struct{}

func (SubAgentPendingInputsRepository) Enqueue(ctx context.Context, tx pgx.Tx, subAgentID uuid.UUID, input string, priority bool) (SubAgentPendingInputRecord, error) {
	if tx == nil {
		return SubAgentPendingInputRecord{}, fmt.Errorf("tx must not be nil")
	}
	if subAgentID == uuid.Nil {
		return SubAgentPendingInputRecord{}, fmt.Errorf("sub_agent_id must not be empty")
	}
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return SubAgentPendingInputRecord{}, fmt.Errorf("input must not be empty")
	}
	var record SubAgentPendingInputRecord
	err := tx.QueryRow(ctx,
		`INSERT INTO sub_agent_pending_inputs (sub_agent_id, input, priority)
		 VALUES ($1, $2, $3)
		 RETURNING id, sub_agent_id, seq, input, priority, created_at`,
		subAgentID,
		trimmed,
		priority,
	).Scan(&record.ID, &record.SubAgentID, &record.Seq, &record.Input, &record.Priority, &record.CreatedAt)
	return record, err
}

func (SubAgentPendingInputsRepository) ListBySubAgentForUpdate(ctx context.Context, tx pgx.Tx, subAgentID uuid.UUID) ([]SubAgentPendingInputRecord, error) {
	if tx == nil {
		return nil, fmt.Errorf("tx must not be nil")
	}
	if subAgentID == uuid.Nil {
		return nil, fmt.Errorf("sub_agent_id must not be empty")
	}
	rows, err := tx.Query(ctx,
		`SELECT id, sub_agent_id, seq, input, priority, created_at
		 FROM sub_agent_pending_inputs
		 WHERE sub_agent_id = $1
		 ORDER BY priority DESC, seq ASC
		 FOR UPDATE`,
		subAgentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]SubAgentPendingInputRecord, 0)
	for rows.Next() {
		var item SubAgentPendingInputRecord
		if err := rows.Scan(&item.ID, &item.SubAgentID, &item.Seq, &item.Input, &item.Priority, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (SubAgentPendingInputsRepository) DeleteBatch(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) error {
	if tx == nil {
		return fmt.Errorf("tx must not be nil")
	}
	if len(ids) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx,
		`DELETE FROM sub_agent_pending_inputs WHERE id = ANY($1)`,
		ids,
	)
	return err
}
