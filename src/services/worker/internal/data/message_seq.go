package data

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func AllocateThreadSeqRange(ctx context.Context, tx pgx.Tx, accountID, threadID uuid.UUID, count int64) (int64, error) {
	if tx == nil {
		return 0, fmt.Errorf("tx must not be nil")
	}
	if accountID == uuid.Nil || threadID == uuid.Nil {
		return 0, fmt.Errorf("account_id and thread_id must not be empty")
	}
	if count <= 0 {
		return 0, fmt.Errorf("count must be positive")
	}

	var start int64
	err := tx.QueryRow(
		ctx,
		`UPDATE threads
		    SET next_message_seq = next_message_seq + $3
		  WHERE id = $2
		    AND account_id = $1
		RETURNING next_message_seq - $3`,
		accountID,
		threadID,
		count,
	).Scan(&start)
	if err != nil {
		return 0, err
	}
	return start, nil
}
