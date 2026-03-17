package data

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	workerevents "arkloop/services/worker/internal/events"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type RunEventsRepository struct{}

func (RunEventsRepository) GetLatestEventType(
	ctx context.Context,
	tx pgx.Tx,
	runID uuid.UUID,
	types []string,
) (string, error) {
	if len(types) == 0 {
		return "", nil
	}

	var eventType string
	err := tx.QueryRow(
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

func (r RunEventsRepository) AppendEvent(
	ctx context.Context,
	tx pgx.Tx,
	runID uuid.UUID,
	eventType string,
	dataJSON map[string]any,
	toolName *string,
	errorClass *string,
) (int64, error) {
	return r.appendEventAt(ctx, tx, runID, eventType, dataJSON, toolName, errorClass, nil)
}

func (r RunEventsRepository) AppendRunEvent(
	ctx context.Context,
	tx pgx.Tx,
	runID uuid.UUID,
	ev workerevents.RunEvent,
) (int64, error) {
	occurredAt := ev.OccurredAt
	if occurredAt.IsZero() {
		return r.appendEventAt(ctx, tx, runID, ev.Type, ev.DataJSON, ev.ToolName, ev.ErrorClass, nil)
	}
	return r.appendEventAt(ctx, tx, runID, ev.Type, ev.DataJSON, ev.ToolName, ev.ErrorClass, &occurredAt)
}

func (r RunEventsRepository) appendEventAt(
	ctx context.Context,
	tx pgx.Tx,
	runID uuid.UUID,
	eventType string,
	dataJSON map[string]any,
	toolName *string,
	errorClass *string,
	occurredAt *time.Time,
) (int64, error) {
	seq, err := r.allocateSeq(ctx, tx, runID)
	if err != nil {
		return 0, err
	}

	encoded, err := json.Marshal(mapOrEmpty(dataJSON))
	if err != nil {
		return 0, err
	}

	if occurredAt != nil && !occurredAt.IsZero() {
		_, err = tx.Exec(
			ctx,
			`INSERT INTO run_events (
				run_id, seq, ts, type, data_json, tool_name, error_class
			) VALUES (
				$1, $2, $3, $4, $5::jsonb, $6, $7
			)`,
			runID,
			seq,
			occurredAt.UTC(),
			eventType,
			string(encoded),
			toolName,
			errorClass,
		)
	} else {
		_, err = tx.Exec(
			ctx,
			`INSERT INTO run_events (
				run_id, seq, type, data_json, tool_name, error_class
			) VALUES (
				$1, $2, $3, $4::jsonb, $5, $6
			)`,
			runID,
			seq,
			eventType,
			string(encoded),
			toolName,
			errorClass,
		)
	}
	if err != nil {
		return 0, err
	}

	return seq, nil
}

func (RunEventsRepository) FirstEventData(
	ctx context.Context,
	tx pgx.Tx,
	runID uuid.UUID,
) (string, map[string]any, error) {
	var (
		eventType string
		rawJSON   []byte
	)
	err := tx.QueryRow(
		ctx,
		`SELECT type, data_json
		 FROM run_events
		 WHERE run_id = $1
		 ORDER BY seq ASC
		 LIMIT 1`,
		runID,
	).Scan(&eventType, &rawJSON)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil, nil
		}
		return "", nil, err
	}

	if len(rawJSON) == 0 {
		return eventType, nil, nil
	}

	var parsed any
	if err := json.Unmarshal(rawJSON, &parsed); err != nil {
		return eventType, nil, nil
	}
	obj, ok := parsed.(map[string]any)
	if !ok {
		return eventType, nil, nil
	}
	return eventType, obj, nil
}

// allocateSeq returns a gapless per-run sequence number.
// It locks the runs row to serialize concurrent allocations for the same run,
// then computes MAX(seq)+1 from committed run_events.
func (RunEventsRepository) allocateSeq(ctx context.Context, tx pgx.Tx, runID uuid.UUID) (int64, error) {
	if _, err := tx.Exec(ctx, `SELECT 1 FROM runs WHERE id = $1 FOR UPDATE`, runID); err != nil {
		return 0, err
	}
	var seq int64
	err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM run_events WHERE run_id = $1`,
		runID,
	).Scan(&seq)
	return seq, err
}

func mapOrEmpty(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}
