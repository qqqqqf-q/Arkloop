package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ChannelMessageDelivery struct {
	ID                uuid.UUID
	RunID             *uuid.UUID
	ThreadID          *uuid.UUID
	ChannelID         uuid.UUID
	PlatformChatID    string
	PlatformMessageID string
	CreatedAt         time.Time
}

type ChannelMessageDeliveriesRepository struct {
	db Querier
}

func NewChannelMessageDeliveriesRepository(db Querier) (*ChannelMessageDeliveriesRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &ChannelMessageDeliveriesRepository{db: db}, nil
}

func (r *ChannelMessageDeliveriesRepository) WithTx(tx pgx.Tx) *ChannelMessageDeliveriesRepository {
	return &ChannelMessageDeliveriesRepository{db: tx}
}

func (r *ChannelMessageDeliveriesRepository) Record(
	ctx context.Context,
	runID *uuid.UUID,
	threadID *uuid.UUID,
	channelID uuid.UUID,
	platformChatID string,
	platformMessageID string,
) (bool, error) {
	if channelID == uuid.Nil {
		return false, fmt.Errorf("channel_message_deliveries: channel_id must not be empty")
	}
	if platformChatID == "" || platformMessageID == "" {
		return false, fmt.Errorf("channel_message_deliveries: platform ids must not be empty")
	}
	tag, err := r.db.Exec(
		ctx,
		`INSERT INTO channel_message_deliveries (run_id, thread_id, channel_id, platform_chat_id, platform_message_id)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (channel_id, platform_chat_id, platform_message_id) DO NOTHING`,
		runID,
		threadID,
		channelID,
		platformChatID,
		platformMessageID,
	)
	if err != nil {
		return false, fmt.Errorf("channel_message_deliveries.Record: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
