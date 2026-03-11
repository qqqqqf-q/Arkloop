package data

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type SubAgentEventRecord struct {
	EventID    uuid.UUID
	SubAgentID uuid.UUID
	RunID      *uuid.UUID
	Seq        int64
	TS         time.Time
	Type       string
	DataJSON   map[string]any
	ErrorClass *string
}

type subAgentEventQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type SubAgentEventsRepository struct{}

func (r SubAgentEventsRepository) AppendEvent(
	ctx context.Context,
	tx pgx.Tx,
	subAgentID uuid.UUID,
	runID *uuid.UUID,
	eventType string,
	dataJSON map[string]any,
	errorClass *string,
) (int64, error) {
	if tx == nil {
		return 0, fmt.Errorf("tx must not be nil")
	}
	if subAgentID == uuid.Nil {
		return 0, fmt.Errorf("sub_agent_id must not be empty")
	}
	trimmedType := strings.TrimSpace(eventType)
	if trimmedType == "" {
		return 0, fmt.Errorf("event_type must not be empty")
	}

	seq, err := (RunEventsRepository{}).allocateSeq(ctx, tx)
	if err != nil {
		return 0, err
	}

	encoded, err := json.Marshal(mapOrEmpty(dataJSON))
	if err != nil {
		return 0, err
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO sub_agent_events (
			sub_agent_id, run_id, seq, type, data_json, error_class
		 ) VALUES (
			$1, $2, $3, $4, $5::jsonb, $6
		 )`,
		subAgentID,
		runID,
		seq,
		trimmedType,
		string(encoded),
		normalizeSubAgentOptionalString(errorClass),
	)
	if err != nil {
		return 0, err
	}

	return seq, nil
}

func (SubAgentEventsRepository) ListBySubAgent(
	ctx context.Context,
	db subAgentEventQuerier,
	subAgentID uuid.UUID,
	sinceSeq int64,
	limit int,
) ([]SubAgentEventRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("db must not be nil")
	}
	if subAgentID == uuid.Nil {
		return nil, fmt.Errorf("sub_agent_id must not be empty")
	}
	if limit <= 0 {
		limit = 100
	}

	rows, err := db.Query(ctx,
		`SELECT event_id, sub_agent_id, run_id, seq, ts, type, data_json, error_class
		 FROM sub_agent_events
		 WHERE sub_agent_id = $1
		   AND seq > $2
		 ORDER BY seq ASC
		 LIMIT $3`,
		subAgentID,
		sinceSeq,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]SubAgentEventRecord, 0)
	for rows.Next() {
		record, err := scanSubAgentEventFromRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanSubAgentEventFromRows(rows pgx.Rows) (SubAgentEventRecord, error) {
	var (
		record  SubAgentEventRecord
		rawJSON []byte
	)
	if err := rows.Scan(
		&record.EventID,
		&record.SubAgentID,
		&record.RunID,
		&record.Seq,
		&record.TS,
		&record.Type,
		&rawJSON,
		&record.ErrorClass,
	); err != nil {
		return SubAgentEventRecord{}, err
	}
	if len(rawJSON) == 0 {
		record.DataJSON = map[string]any{}
		return record, nil
	}
	if err := json.Unmarshal(rawJSON, &record.DataJSON); err != nil {
		return SubAgentEventRecord{}, err
	}
	if record.DataJSON == nil {
		record.DataJSON = map[string]any{}
	}
	return record, nil
}
