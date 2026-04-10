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

type ThreadContextReplacementRecord struct {
	ID             uuid.UUID
	AccountID      uuid.UUID
	ThreadID       uuid.UUID
	StartThreadSeq int64
	EndThreadSeq   int64
	SummaryText    string
	Layer          int
	MetadataJSON   json.RawMessage
	SupersededAt   *time.Time
	CreatedAt      time.Time
}

type ThreadContextReplacementInsertInput struct {
	AccountID      uuid.UUID
	ThreadID       uuid.UUID
	StartThreadSeq int64
	EndThreadSeq   int64
	SummaryText    string
	Layer          int
	MetadataJSON   json.RawMessage
}

type ThreadContextReplacementsRepository struct{}

func (ThreadContextReplacementsRepository) ListActiveByThreadUpToSeq(
	ctx context.Context,
	tx pgx.Tx,
	accountID uuid.UUID,
	threadID uuid.UUID,
	upperBoundThreadSeq *int64,
) ([]ThreadContextReplacementRecord, error) {
	if tx == nil {
		return nil, fmt.Errorf("tx must not be nil")
	}
	if accountID == uuid.Nil || threadID == uuid.Nil {
		return nil, fmt.Errorf("account_id and thread_id must not be empty")
	}

	args := []any{accountID, threadID}
	query := `SELECT id, account_id, thread_id, start_thread_seq, end_thread_seq,
	                 summary_text, layer, metadata_json, superseded_at, created_at
	            FROM thread_context_replacements
	           WHERE account_id = $1
	             AND thread_id = $2
	             AND superseded_at IS NULL`
	if upperBoundThreadSeq != nil {
		query += ` AND start_thread_seq <= $3`
		args = append(args, *upperBoundThreadSeq)
	}
	query += ` ORDER BY layer DESC, created_at DESC, id DESC`

	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ThreadContextReplacementRecord, 0)
	for rows.Next() {
		var item ThreadContextReplacementRecord
		if err := rows.Scan(
			&item.ID,
			&item.AccountID,
			&item.ThreadID,
			&item.StartThreadSeq,
			&item.EndThreadSeq,
			&item.SummaryText,
			&item.Layer,
			&item.MetadataJSON,
			&item.SupersededAt,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		item.SummaryText = strings.TrimSpace(item.SummaryText)
		if item.SummaryText == "" {
			continue
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (ThreadContextReplacementsRepository) GetLeadingActiveByThread(
	ctx context.Context,
	tx pgx.Tx,
	accountID uuid.UUID,
	threadID uuid.UUID,
) (*ThreadContextReplacementRecord, error) {
	if tx == nil {
		return nil, fmt.Errorf("tx must not be nil")
	}
	if accountID == uuid.Nil || threadID == uuid.Nil {
		return nil, fmt.Errorf("account_id and thread_id must not be empty")
	}
	var item ThreadContextReplacementRecord
	err := tx.QueryRow(
		ctx,
		`SELECT id, account_id, thread_id, start_thread_seq, end_thread_seq,
		        summary_text, layer, metadata_json, superseded_at, created_at
		   FROM thread_context_replacements
		  WHERE account_id = $1
		    AND thread_id = $2
		    AND superseded_at IS NULL
		  ORDER BY start_thread_seq ASC, layer DESC, created_at DESC, id DESC
		  LIMIT 1`,
		accountID,
		threadID,
	).Scan(
		&item.ID,
		&item.AccountID,
		&item.ThreadID,
		&item.StartThreadSeq,
		&item.EndThreadSeq,
		&item.SummaryText,
		&item.Layer,
		&item.MetadataJSON,
		&item.SupersededAt,
		&item.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	item.SummaryText = strings.TrimSpace(item.SummaryText)
	if item.SummaryText == "" {
		return nil, nil
	}
	return &item, nil
}

func (ThreadContextReplacementsRepository) Insert(
	ctx context.Context,
	tx pgx.Tx,
	input ThreadContextReplacementInsertInput,
) (*ThreadContextReplacementRecord, error) {
	if tx == nil {
		return nil, fmt.Errorf("tx must not be nil")
	}
	if input.AccountID == uuid.Nil || input.ThreadID == uuid.Nil {
		return nil, fmt.Errorf("account_id and thread_id must not be empty")
	}
	if input.StartThreadSeq <= 0 || input.EndThreadSeq <= 0 || input.StartThreadSeq > input.EndThreadSeq {
		return nil, fmt.Errorf("invalid thread seq range")
	}
	summaryText := strings.TrimSpace(input.SummaryText)
	if summaryText == "" {
		return nil, fmt.Errorf("summary_text must not be empty")
	}
	layer := input.Layer
	if layer <= 0 {
		layer = 1
	}
	metadata := input.MetadataJSON
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}

	var item ThreadContextReplacementRecord
	err := tx.QueryRow(
		ctx,
		`INSERT INTO thread_context_replacements (
			account_id, thread_id, start_thread_seq, end_thread_seq, summary_text, layer, metadata_json
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7::jsonb
		)
		RETURNING id, account_id, thread_id, start_thread_seq, end_thread_seq,
		          summary_text, layer, metadata_json, superseded_at, created_at`,
		input.AccountID,
		input.ThreadID,
		input.StartThreadSeq,
		input.EndThreadSeq,
		summaryText,
		layer,
		string(metadata),
	).Scan(
		&item.ID,
		&item.AccountID,
		&item.ThreadID,
		&item.StartThreadSeq,
		&item.EndThreadSeq,
		&item.SummaryText,
		&item.Layer,
		&item.MetadataJSON,
		&item.SupersededAt,
		&item.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	item.SummaryText = strings.TrimSpace(item.SummaryText)
	return &item, nil
}

func (ThreadContextReplacementsRepository) SupersedeActiveOverlaps(
	ctx context.Context,
	tx pgx.Tx,
	accountID uuid.UUID,
	threadID uuid.UUID,
	startThreadSeq int64,
	endThreadSeq int64,
	exceptID uuid.UUID,
) error {
	if tx == nil {
		return fmt.Errorf("tx must not be nil")
	}
	if accountID == uuid.Nil || threadID == uuid.Nil {
		return fmt.Errorf("account_id and thread_id must not be empty")
	}
	if startThreadSeq <= 0 || endThreadSeq <= 0 || startThreadSeq > endThreadSeq {
		return fmt.Errorf("invalid thread seq range")
	}
	_, err := tx.Exec(
		ctx,
		`UPDATE thread_context_replacements
		    SET superseded_at = now()
		  WHERE account_id = $1
		    AND thread_id = $2
		    AND superseded_at IS NULL
		    AND id <> $3
		    AND start_thread_seq <= $5
		    AND end_thread_seq >= $4`,
		accountID,
		threadID,
		exceptID,
		startThreadSeq,
		endThreadSeq,
	)
	return err
}
