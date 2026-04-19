//go:build desktop

package data

import (
	"context"
	"fmt"
	"time"
)

// ListPendingForDrain 在持有写锁的事务内用 UPDATE...RETURNING 租约挑选待处理行。
// SQLite 3.35+ 支持 RETURNING；推后 next_retry_at 作为租约避免重复拉取。
func (ChannelDeliveryOutboxRepository) ListPendingForDrain(ctx context.Context, db DesktopDB, limit int) ([]ChannelDeliveryOutboxRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("db must not be nil")
	}
	if limit <= 0 {
		limit = 10
	}
	now := time.Now().UTC()
	leaseUntil := now.Add(30 * time.Second)
	rows, err := db.Query(ctx, `
		UPDATE channel_delivery_outbox
		   SET next_retry_at = $1,
		       updated_at = $1
		 WHERE id IN (
		     SELECT id
		       FROM channel_delivery_outbox
		      WHERE status = 'pending'
		        AND next_retry_at <= $2
		      ORDER BY next_retry_at
		      LIMIT $3
		 )
		RETURNING id, run_id, thread_id, channel_id, channel_type, kind, status, payload_json, segments_sent, attempts, last_error, next_retry_at, created_at, updated_at`,
		leaseUntil, now, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("channel_delivery_outbox.ListPendingForDrain: %w", err)
	}
	defer rows.Close()

	out := make([]ChannelDeliveryOutboxRecord, 0, limit)
	for rows.Next() {
		var item ChannelDeliveryOutboxRecord
		if err := rows.Scan(
			&item.ID,
			&item.RunID,
			&item.ThreadID,
			&item.ChannelID,
			&item.ChannelType,
			&item.Kind,
			&item.Status,
			&item.PayloadJSON,
			&item.SegmentsSent,
			&item.Attempts,
			&item.LastError,
			&item.NextRetryAt,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("channel_delivery_outbox.ListPendingForDrain scan: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("channel_delivery_outbox.ListPendingForDrain rows: %w", err)
	}
	return out, nil
}
