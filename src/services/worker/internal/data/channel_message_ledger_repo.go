package data

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
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
			metadata_json
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb)
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
