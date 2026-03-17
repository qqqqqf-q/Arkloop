package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ChannelMessageReceipt struct {
	ID                uuid.UUID
	ChannelID         uuid.UUID
	PlatformChatID    string
	PlatformMessageID string
	CreatedAt         time.Time
}

type ChannelMessageReceiptsRepository struct {
	db Querier
}

func NewChannelMessageReceiptsRepository(db Querier) (*ChannelMessageReceiptsRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &ChannelMessageReceiptsRepository{db: db}, nil
}

func (r *ChannelMessageReceiptsRepository) WithTx(tx pgx.Tx) *ChannelMessageReceiptsRepository {
	return &ChannelMessageReceiptsRepository{db: tx}
}

func (r *ChannelMessageReceiptsRepository) Record(
	ctx context.Context,
	channelID uuid.UUID,
	platformChatID string,
	platformMessageID string,
) (bool, error) {
	if channelID == uuid.Nil {
		return false, fmt.Errorf("channel_message_receipts: channel_id must not be empty")
	}
	if platformChatID == "" || platformMessageID == "" {
		return false, fmt.Errorf("channel_message_receipts: platform ids must not be empty")
	}
	tag, err := r.db.Exec(
		ctx,
		`INSERT INTO channel_message_receipts (channel_id, platform_chat_id, platform_message_id)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (channel_id, platform_chat_id, platform_message_id) DO NOTHING`,
		channelID,
		platformChatID,
		platformMessageID,
	)
	if err != nil {
		return false, fmt.Errorf("channel_message_receipts.Record: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
