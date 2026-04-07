//go:build !desktop

package data

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

type RunPipelineEventsRepository struct {
	db RunPipelineEventsDB
}

func NewRunPipelineEventsRepository(db RunPipelineEventsDB) *RunPipelineEventsRepository {
	if db == nil {
		return nil
	}
	return &RunPipelineEventsRepository{db: db}
}

func (r *RunPipelineEventsRepository) InsertBatch(ctx context.Context, records []RunPipelineEventRecord) error {
	if r == nil || r.db == nil || len(records) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, record := range records {
		payload, err := json.Marshal(runPipelineFieldsOrEmpty(record.FieldsJSON))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO run_pipeline_events (
			    run_id, account_id, middleware, event_name, seq, fields_json
			 ) VALUES (
			    $1, $2, $3, $4, $5, $6::jsonb
			 )`,
			record.RunID,
			record.AccountID,
			record.Middleware,
			record.EventName,
			record.Seq,
			string(payload),
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *RunPipelineEventsRepository) DeleteOlderThan(ctx context.Context, cutoff time.Time) error {
	if r == nil || r.db == nil {
		return nil
	}
	_, err := r.db.Exec(ctx,
		`DELETE FROM run_pipeline_events
		  WHERE created_at < $1`,
		cutoff.UTC(),
	)
	return err
}

func runPipelineFieldsOrEmpty(fields map[string]any) map[string]any {
	if fields == nil {
		return map[string]any{}
	}
	return fields
}
