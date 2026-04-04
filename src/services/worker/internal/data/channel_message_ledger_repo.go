package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type channelMessageLedgerExecer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type ChannelMessageDirection string

const (
	ChannelMessageDirectionInbound  ChannelMessageDirection = "inbound"
	ChannelMessageDirectionOutbound ChannelMessageDirection = "outbound"
)

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

type ChannelMessageLedgerRepository struct{}

func (ChannelMessageLedgerRepository) Record(ctx context.Context, db channelMessageLedgerExecer, input ChannelMessageLedgerRecordInput) error {
	if db == nil {
		return fmt.Errorf("db must not be nil")
	}
	if input.ChannelID == uuid.Nil {
		return fmt.Errorf("channel_message_ledger: channel_id must not be empty")
	}
	if strings.TrimSpace(input.ChannelType) == "" {
		return fmt.Errorf("channel_message_ledger: channel_type must not be empty")
	}
	if input.Direction != ChannelMessageDirectionInbound && input.Direction != ChannelMessageDirectionOutbound {
		return fmt.Errorf("channel_message_ledger: direction must be inbound or outbound")
	}
	if strings.TrimSpace(input.PlatformConversationID) == "" || strings.TrimSpace(input.PlatformMessageID) == "" {
		return fmt.Errorf("channel_message_ledger: platform ids must not be empty")
	}
	metadataJSON := input.MetadataJSON
	if len(metadataJSON) == 0 {
		metadataJSON = json.RawMessage(`{}`)
	}
	_, err := db.Exec(
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
		trimOptionalLedgerString(input.PlatformParentMessageID),
		trimOptionalLedgerString(input.PlatformThreadID),
		input.SenderChannelIdentityID,
		input.MessageID,
		metadataJSON,
	)
	if err != nil {
		return fmt.Errorf("channel_message_ledger.Record: %w", err)
	}
	return nil
}

func trimOptionalLedgerString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// ChannelMessageLedgerRow is a read model for channel_message_ledger lookups.
type ChannelMessageLedgerRow struct {
	ChannelID               uuid.UUID
	ChannelType             string
	Direction               ChannelMessageDirection
	ThreadID                *uuid.UUID
	RunID                   *uuid.UUID
	PlatformConversationID  string
	PlatformMessageID       string
	PlatformParentMessageID *string
	PlatformThreadID        *string
	MetadataJSON            json.RawMessage
}

// LookupByPlatformMessage returns the latest ledger row for a platform message in a conversation.
func (ChannelMessageLedgerRepository) LookupByPlatformMessage(
	ctx context.Context,
	pool *pgxpool.Pool,
	channelID uuid.UUID,
	platformConversationID string,
	platformMessageID string,
) (*ChannelMessageLedgerRow, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool must not be nil")
	}
	if channelID == uuid.Nil {
		return nil, fmt.Errorf("channel_message_ledger: channel_id must not be empty")
	}
	if strings.TrimSpace(platformConversationID) == "" || strings.TrimSpace(platformMessageID) == "" {
		return nil, fmt.Errorf("channel_message_ledger: platform ids must not be empty")
	}
	var row ChannelMessageLedgerRow
	var direction string
	var metadata []byte
	err := pool.QueryRow(
		ctx,
		`SELECT channel_id, channel_type, direction, thread_id, run_id,
			platform_conversation_id, platform_message_id,
			platform_parent_message_id, platform_thread_id, metadata_json
		   FROM channel_message_ledger
		  WHERE channel_id = $1
		    AND platform_conversation_id = $2
		    AND platform_message_id = $3
		  ORDER BY created_at DESC
		  LIMIT 1`,
		channelID,
		strings.TrimSpace(platformConversationID),
		strings.TrimSpace(platformMessageID),
	).Scan(
		&row.ChannelID,
		&row.ChannelType,
		&direction,
		&row.ThreadID,
		&row.RunID,
		&row.PlatformConversationID,
		&row.PlatformMessageID,
		&row.PlatformParentMessageID,
		&row.PlatformThreadID,
		&metadata,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("channel_message_ledger.LookupByPlatformMessage: %w", err)
	}
	row.Direction = ChannelMessageDirection(direction)
	if len(metadata) > 0 {
		row.MetadataJSON = metadata
	} else {
		row.MetadataJSON = json.RawMessage(`{}`)
	}
	return &row, nil
}
