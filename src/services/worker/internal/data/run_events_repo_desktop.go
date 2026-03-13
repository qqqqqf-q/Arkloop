//go:build desktop

package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DesktopRunEventsRepository provides SQLite-compatible event persistence.
type DesktopRunEventsRepository struct{}

func (DesktopRunEventsRepository) GetLatestEventType(
	ctx context.Context,
	tx pgx.Tx,
	runID uuid.UUID,
	types []string,
) (string, error) {
	if len(types) == 0 {
		return "", nil
	}

	placeholders := make([]string, len(types))
	args := []any{runID}
	for i, t := range types {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args = append(args, t)
	}

	query := fmt.Sprintf(
		`SELECT type FROM run_events
		 WHERE run_id = $1 AND type IN (%s)
		 ORDER BY seq DESC LIMIT 1`,
		strings.Join(placeholders, ","),
	)

	var eventType string
	err := tx.QueryRow(ctx, query, args...).Scan(&eventType)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return eventType, nil
}

func (r DesktopRunEventsRepository) AppendEvent(
	ctx context.Context,
	tx pgx.Tx,
	runID uuid.UUID,
	eventType string,
	dataJSON map[string]any,
	toolName *string,
	errorClass *string,
) (int64, error) {
	seq, err := r.allocateSeq(ctx, tx, runID)
	if err != nil {
		return 0, err
	}

	encoded, err := json.Marshal(desktopMapOrEmpty(dataJSON))
	if err != nil {
		return 0, err
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO run_events (run_id, seq, type, data_json, tool_name, error_class)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		runID, seq, eventType, string(encoded), toolName, errorClass,
	)
	return seq, err
}

func (DesktopRunEventsRepository) FirstEventData(
	ctx context.Context,
	tx pgx.Tx,
	runID uuid.UUID,
) (string, map[string]any, error) {
	var (
		eventType string
		rawJSON   []byte
	)
	err := tx.QueryRow(ctx,
		`SELECT type, data_json FROM run_events
		 WHERE run_id = $1 ORDER BY seq ASC LIMIT 1`,
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

// allocateSeq returns MAX(seq)+1 scoped to the run. Safe under SQLite single-writer.
func (DesktopRunEventsRepository) allocateSeq(ctx context.Context, tx pgx.Tx, runID uuid.UUID) (int64, error) {
	var seq int64
	err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM run_events WHERE run_id = $1`,
		runID,
	).Scan(&seq)
	return seq, err
}

func desktopMapOrEmpty(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}
