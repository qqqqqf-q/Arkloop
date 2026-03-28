package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ThreadCompactionSnapshotRecord struct {
	ID                   uuid.UUID
	AccountID            uuid.UUID
	ThreadID             uuid.UUID
	SummaryText          string
	MetadataJSON         json.RawMessage
	SupersedesSnapshotID *uuid.UUID
	IsActive             bool
	CreatedAt            time.Time
}

type ThreadCompactionSnapshotsRepository struct{}

func (ThreadCompactionSnapshotsRepository) GetActiveByThread(
	ctx context.Context,
	tx pgx.Tx,
	accountID uuid.UUID,
	threadID uuid.UUID,
) (*ThreadCompactionSnapshotRecord, error) {
	if tx == nil {
		return nil, fmt.Errorf("tx must not be nil")
	}
	if accountID == uuid.Nil || threadID == uuid.Nil {
		return nil, fmt.Errorf("account_id and thread_id must not be empty")
	}
	var record ThreadCompactionSnapshotRecord
	err := tx.QueryRow(
		ctx,
		`SELECT id, account_id, thread_id, summary_text, metadata_json, supersedes_snapshot_id, is_active, created_at
		   FROM thread_compaction_snapshots
		  WHERE account_id = $1
		    AND thread_id = $2
		    AND is_active = TRUE
		  ORDER BY created_at DESC, id DESC
		  LIMIT 1`,
		accountID,
		threadID,
	).Scan(
		&record.ID,
		&record.AccountID,
		&record.ThreadID,
		&record.SummaryText,
		&record.MetadataJSON,
		&record.SupersedesSnapshotID,
		&record.IsActive,
		&record.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	record.SummaryText = strings.TrimSpace(record.SummaryText)
	return &record, nil
}

func (repo ThreadCompactionSnapshotsRepository) ReplaceActive(
	ctx context.Context,
	tx pgx.Tx,
	accountID uuid.UUID,
	threadID uuid.UUID,
	summaryText string,
	metadata json.RawMessage,
) (*ThreadCompactionSnapshotRecord, error) {
	if tx == nil {
		return nil, fmt.Errorf("tx must not be nil")
	}
	if accountID == uuid.Nil || threadID == uuid.Nil {
		return nil, fmt.Errorf("account_id and thread_id must not be empty")
	}
	summaryText = strings.TrimSpace(summaryText)
	if summaryText == "" {
		return nil, fmt.Errorf("summary_text must not be empty")
	}
	if len(metadata) == 0 {
		metadata = []byte(`{}`)
	}

	active, err := repo.GetActiveByThread(ctx, tx, accountID, threadID)
	if err != nil {
		return nil, err
	}
	if active != nil {
		if _, err := tx.Exec(
			ctx,
			`UPDATE thread_compaction_snapshots
			    SET is_active = FALSE
			  WHERE id = $1`,
			active.ID,
		); err != nil {
			return nil, err
		}
	}

	var record ThreadCompactionSnapshotRecord
	var supersedesID *uuid.UUID
	if active != nil {
		supersedesID = &active.ID
	}
	err = tx.QueryRow(
		ctx,
		`INSERT INTO thread_compaction_snapshots (
			account_id, thread_id, summary_text, metadata_json, supersedes_snapshot_id, is_active
		) VALUES (
			$1, $2, $3, $4, $5, TRUE
		)
		RETURNING id, account_id, thread_id, summary_text, metadata_json, supersedes_snapshot_id, is_active, created_at`,
		accountID,
		threadID,
		summaryText,
		string(metadata),
		supersedesID,
	).Scan(
		&record.ID,
		&record.AccountID,
		&record.ThreadID,
		&record.SummaryText,
		&record.MetadataJSON,
		&record.SupersedesSnapshotID,
		&record.IsActive,
		&record.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	record.SummaryText = strings.TrimSpace(record.SummaryText)
	return &record, nil
}
