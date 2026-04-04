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

type ChannelMessageDirection string

const (
	ChannelMessageDirectionInbound  ChannelMessageDirection = "inbound"
	ChannelMessageDirectionOutbound ChannelMessageDirection = "outbound"
)

type ChannelMessageLedgerEntry struct {
	ID                      uuid.UUID
	ChannelID               uuid.UUID
	ChannelType             string
	Direction               ChannelMessageDirection
	ThreadID                *uuid.UUID
	RunID                   *uuid.UUID
	PlatformConversationID  string
	PlatformMessageID       string
	PlatformParentMessageID *string
	PlatformThreadID        *string
	SenderChannelIdentityID *uuid.UUID
	MessageID               *uuid.UUID
	MetadataJSON            json.RawMessage
	CreatedAt               time.Time
}

type ChannelMessageLedgerRecordInput struct {
	ChannelID               uuid.UUID
	ChannelType             string
	Direction               ChannelMessageDirection
	ThreadID                *uuid.UUID
	RunID                   *uuid.UUID
	PlatformConversationID  string
	PlatformMessageID       string
	PlatformParentMessageID *string
	PlatformThreadID        *string
	SenderChannelIdentityID *uuid.UUID
	MessageID               *uuid.UUID
	MetadataJSON            json.RawMessage
}

type ChannelMessageLedgerRepository struct {
	db Querier
}

func NewChannelMessageLedgerRepository(db Querier) (*ChannelMessageLedgerRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &ChannelMessageLedgerRepository{db: db}, nil
}

func (r *ChannelMessageLedgerRepository) WithTx(tx pgx.Tx) *ChannelMessageLedgerRepository {
	return &ChannelMessageLedgerRepository{db: tx}
}

func (r *ChannelMessageLedgerRepository) Record(ctx context.Context, input ChannelMessageLedgerRecordInput) (bool, error) {
	if input.ChannelID == uuid.Nil {
		return false, fmt.Errorf("channel_message_ledger: channel_id must not be empty")
	}
	if strings.TrimSpace(input.ChannelType) == "" {
		return false, fmt.Errorf("channel_message_ledger: channel_type must not be empty")
	}
	if input.Direction != ChannelMessageDirectionInbound && input.Direction != ChannelMessageDirectionOutbound {
		return false, fmt.Errorf("channel_message_ledger: direction must be inbound or outbound")
	}
	if strings.TrimSpace(input.PlatformConversationID) == "" || strings.TrimSpace(input.PlatformMessageID) == "" {
		return false, fmt.Errorf("channel_message_ledger: platform ids must not be empty")
	}
	metadataJSON := input.MetadataJSON
	if len(metadataJSON) == 0 {
		metadataJSON = json.RawMessage(`{}`)
	}
	tag, err := r.db.Exec(
		ctx,
		`INSERT INTO channel_message_ledger (
			channel_id,
			channel_type,
			direction,
			thread_id,
			run_id,
			platform_conversation_id,
			platform_message_id,
			platform_parent_message_id,
			platform_thread_id,
			sender_channel_identity_id,
			message_id,
			metadata_json
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb)
		ON CONFLICT (channel_id, direction, platform_conversation_id, platform_message_id) DO NOTHING`,
		input.ChannelID,
		strings.TrimSpace(input.ChannelType),
		string(input.Direction),
		input.ThreadID,
		input.RunID,
		strings.TrimSpace(input.PlatformConversationID),
		strings.TrimSpace(input.PlatformMessageID),
		trimOptionalStringPtr(input.PlatformParentMessageID),
		trimOptionalStringPtr(input.PlatformThreadID),
		input.SenderChannelIdentityID,
		input.MessageID,
		metadataJSON,
	)
	if err != nil {
		return false, fmt.Errorf("channel_message_ledger.Record: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func trimOptionalStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func (r *ChannelMessageLedgerRepository) HasOutboundForRun(ctx context.Context, runID uuid.UUID) (bool, error) {
	if runID == uuid.Nil {
		return false, nil
	}
	var exists bool
	err := r.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM channel_message_ledger WHERE run_id = $1 AND direction = 'outbound')`,
		runID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("channel_message_ledger.HasOutboundForRun: %w", err)
	}
	return exists, nil
}

func (r *ChannelMessageLedgerRepository) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM channel_message_ledger WHERE created_at < $1`, cutoff.UTC())
	if err != nil {
		return 0, fmt.Errorf("channel_message_ledger.DeleteOlderThan: %w", err)
	}
	return tag.RowsAffected(), nil
}

// LookupInboundMessage 通过 channel 的 inbound 平台消息 ID 查找对应的 ledger 记录。
func (r *ChannelMessageLedgerRepository) LookupInboundMessage(
	ctx context.Context,
	channelID uuid.UUID,
	platformConversationID string,
	platformMessageID string,
) (*uuid.UUID, *uuid.UUID, error) {
	var messageID, threadID *uuid.UUID
	err := r.db.QueryRow(ctx,
		`SELECT message_id, thread_id FROM channel_message_ledger
		 WHERE channel_id = $1
		   AND direction = 'inbound'
		   AND platform_conversation_id = $2
		   AND platform_message_id = $3`,
		channelID,
		strings.TrimSpace(platformConversationID),
		strings.TrimSpace(platformMessageID),
	).Scan(&messageID, &threadID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("channel_message_ledger.LookupInboundMessage: %w", err)
	}
	return messageID, threadID, nil
}
