//go:build !desktop

package data

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ListPendingForDrain 用租约模式原子锁定待处理行。
// 一条 UPDATE 同时推后 next_retry_at（租约），连接归还池后其他 drainer
// 因 next_retry_at > now() 不会再抓到同一行；租约到期自动回收。
func (ChannelDeliveryOutboxRepository) ListPendingForDrain(ctx context.Context, pool *pgxpool.Pool, limit int) ([]ChannelDeliveryOutboxRecord, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool must not be nil")
	}
	if limit <= 0 {
		limit = 10
	}
	leaseUntil := time.Now().UTC().Add(30 * time.Second)
	rows, err := pool.Query(ctx, `
		UPDATE channel_delivery_outbox
		   SET next_retry_at = $1,
		       updated_at = now()
		 WHERE id IN (
		     SELECT id
		       FROM channel_delivery_outbox
		      WHERE status = 'pending'
		        AND next_retry_at <= now()
		      ORDER BY next_retry_at
		      FOR UPDATE SKIP LOCKED
		      LIMIT $2
		 )
		RETURNING id, run_id, thread_id, channel_id, channel_type, kind, status, payload_json, segments_sent, attempts, last_error, next_retry_at, created_at, updated_at`,
		leaseUntil, limit,
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
