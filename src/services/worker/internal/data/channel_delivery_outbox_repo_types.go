// Package data 提供 channel_delivery_outbox 的持久化操作。
// Outbox 保证 at-least-once delivery：分段发送时若在 HTTP 成功与状态更新之间崩溃，
// 下一段会从已确认的 segments_sent 位置继续，导致该段可能重发。
package data

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type ChannelDeliveryOutboxExecer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Outbox kind 常量，对应 UNIQUE (run_id, kind) 约束。
const (
	OutboxKindMessage              = "message"
	OutboxKindInjectionBlockNotice = "injection_block_notice"
)

// OutboxMaxAttempts 最大重试次数，超过则转 dead。
const OutboxMaxAttempts = 5

// OutboxBackoffDelay 根据 attempts 返回下次重试的等待时长。
func OutboxBackoffDelay(attempts int) time.Duration {
	switch attempts {
	case 0:
		return 0
	case 1:
		return 1 * time.Second
	case 2:
		return 10 * time.Second
	case 3:
		return 30 * time.Second
	case 4:
		return 60 * time.Second
	default:
		return 300 * time.Second
	}
}

type OutboxPayload struct {
	AccountID              uuid.UUID      `json:"account_id"`
	RunID                  uuid.UUID      `json:"run_id"`
	ThreadID               *uuid.UUID     `json:"thread_id,omitempty"`
	Outputs                []string       `json:"outputs"`
	Text                   string         `json:"text,omitempty"`
	IsTerminalNotice       bool           `json:"is_terminal_notice,omitempty"`
	PlatformChatID         string         `json:"platform_chat_id"`
	ReplyToMessageID       string         `json:"reply_to_message_id,omitempty"`
	PlatformThreadID       *string        `json:"platform_thread_id,omitempty"`
	ConversationType       string         `json:"conversation_type,omitempty"`
	HeartbeatRun           bool           `json:"heartbeat_run,omitempty"`
	InboundMessageID       string         `json:"inbound_message_id,omitempty"`
	TriggerMessageID       string         `json:"trigger_message_id,omitempty"`
	ChannelReplyOverrideID string         `json:"channel_reply_override_id,omitempty"`
	Metadata               map[string]any `json:"metadata,omitempty"`
}

type ChannelDeliveryOutboxRecord struct {
	ID           uuid.UUID
	RunID        uuid.UUID
	ThreadID     *uuid.UUID
	ChannelID    uuid.UUID
	ChannelType  string
	Kind         string
	Status       string // pending / sent / dead
	PayloadJSON  []byte
	SegmentsSent int
	Attempts     int
	LastError    *string
	NextRetryAt  time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type ChannelDeliveryOutboxRepository struct{}

// InsertPending creates a new pending outbox entry.
// If a non-dead record already exists for the same (run_id, kind), it returns the existing record.
func (ChannelDeliveryOutboxRepository) InsertPending(ctx context.Context, db ChannelDeliveryOutboxExecer, runID, channelID uuid.UUID, threadID *uuid.UUID, channelType, kind string, payload OutboxPayload) (*ChannelDeliveryOutboxRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("db must not be nil")
	}
	if kind == "" {
		kind = OutboxKindMessage
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	now := time.Now().UTC()
	id := uuid.New()
	var record ChannelDeliveryOutboxRecord
	err = db.QueryRow(ctx, `
		INSERT INTO channel_delivery_outbox (
			id, run_id, thread_id, channel_id, channel_type, kind, status, payload_json, segments_sent, attempts, last_error, next_retry_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, 'pending', $7, 0, 0, NULL, $8, $9, $9
		)
		ON CONFLICT (run_id, kind) WHERE status <> 'dead' DO NOTHING
		RETURNING id, run_id, thread_id, channel_id, channel_type, kind, status, payload_json, segments_sent, attempts, last_error, next_retry_at, created_at, updated_at`,
		id, runID, threadID, channelID, channelType, kind, payloadJSON, now, now,
	).Scan(
		&record.ID,
		&record.RunID,
		&record.ThreadID,
		&record.ChannelID,
		&record.ChannelType,
		&record.Kind,
		&record.Status,
		&record.PayloadJSON,
		&record.SegmentsSent,
		&record.Attempts,
		&record.LastError,
		&record.NextRetryAt,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err == nil {
		return &record, nil
	}
	// pgx returns ErrNoRows when ON CONFLICT DO NOTHING skips the insert.
	if err == pgx.ErrNoRows {
		return lookupPendingOutboxByRunIDAndKind(ctx, db, runID, kind)
	}
	return nil, fmt.Errorf("channel_delivery_outbox.InsertPending: %w", err)
}

func lookupPendingOutboxByRunIDAndKind(ctx context.Context, db ChannelDeliveryOutboxExecer, runID uuid.UUID, kind string) (*ChannelDeliveryOutboxRecord, error) {
	var record ChannelDeliveryOutboxRecord
	err := db.QueryRow(ctx, `
		SELECT id, run_id, thread_id, channel_id, channel_type, kind, status, payload_json, segments_sent, attempts, last_error, next_retry_at, created_at, updated_at
		  FROM channel_delivery_outbox
		 WHERE run_id = $1
		   AND kind = $2
		   AND status <> 'dead'`,
		runID, kind,
	).Scan(
		&record.ID,
		&record.RunID,
		&record.ThreadID,
		&record.ChannelID,
		&record.ChannelType,
		&record.Kind,
		&record.Status,
		&record.PayloadJSON,
		&record.SegmentsSent,
		&record.Attempts,
		&record.LastError,
		&record.NextRetryAt,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("channel_delivery_outbox.lookupPendingOutboxByRunIDAndKind: %w", err)
	}
	return &record, nil
}

// UpdateSent marks the outbox as sent.
func (ChannelDeliveryOutboxRepository) UpdateSent(ctx context.Context, db ChannelDeliveryOutboxExecer, id uuid.UUID) error {
	if db == nil {
		return fmt.Errorf("db must not be nil")
	}
	_, err := db.Exec(ctx, `
		UPDATE channel_delivery_outbox
		   SET status = 'sent',
		       updated_at = $2
		 WHERE id = $1`,
		id, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("channel_delivery_outbox.UpdateSent: %w", err)
	}
	return nil
}

// UpdateProgress updates segments_sent after a segment succeeds.
func (ChannelDeliveryOutboxRepository) UpdateProgress(ctx context.Context, db ChannelDeliveryOutboxExecer, id uuid.UUID, segmentsSent int) error {
	if db == nil {
		return fmt.Errorf("db must not be nil")
	}
	_, err := db.Exec(ctx, `
		UPDATE channel_delivery_outbox
		   SET segments_sent = $2,
		       updated_at = $3
		 WHERE id = $1`,
		id, segmentsSent, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("channel_delivery_outbox.UpdateProgress: %w", err)
	}
	return nil
}

// UpdateFailure updates attempts, last_error, and next_retry_at after a failure.
func (ChannelDeliveryOutboxRepository) UpdateFailure(ctx context.Context, db ChannelDeliveryOutboxExecer, id uuid.UUID, attempts int, lastError string, nextRetryAt time.Time) error {
	if db == nil {
		return fmt.Errorf("db must not be nil")
	}
	_, err := db.Exec(ctx, `
		UPDATE channel_delivery_outbox
		   SET attempts = $2,
		       last_error = $3,
		       next_retry_at = $4,
		       updated_at = $5
		 WHERE id = $1`,
		id, attempts, lastError, nextRetryAt.UTC(), time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("channel_delivery_outbox.UpdateFailure: %w", err)
	}
	return nil
}

// MarkDead marks the outbox as dead.
func (ChannelDeliveryOutboxRepository) MarkDead(ctx context.Context, db ChannelDeliveryOutboxExecer, id uuid.UUID, lastError string) error {
	if db == nil {
		return fmt.Errorf("db must not be nil")
	}
	_, err := db.Exec(ctx, `
		UPDATE channel_delivery_outbox
		   SET status = 'dead',
		       last_error = $2,
		       updated_at = $3
		 WHERE id = $1`,
		id, lastError, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("channel_delivery_outbox.MarkDead: %w", err)
	}
	return nil
}

// Cleanup 按 status 清理已完成且超过保留时长的行，返回删除行数。
func (ChannelDeliveryOutboxRepository) Cleanup(ctx context.Context, db ChannelDeliveryOutboxExecer, status string, olderThan time.Time) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("db must not be nil")
	}
	tag, err := db.Exec(ctx, `
		DELETE FROM channel_delivery_outbox
		 WHERE status = $1
		   AND updated_at < $2`,
		status, olderThan.UTC(),
	)
	if err != nil {
		return 0, fmt.Errorf("channel_delivery_outbox.Cleanup(%s): %w", status, err)
	}
	return tag.RowsAffected(), nil
}

// Payload unmarshals PayloadJSON into OutboxPayload.
func (r ChannelDeliveryOutboxRecord) Payload() (OutboxPayload, error) {
	var p OutboxPayload
	if err := json.Unmarshal(r.PayloadJSON, &p); err != nil {
		return p, fmt.Errorf("unmarshal outbox payload: %w", err)
	}
	return p, nil
}
