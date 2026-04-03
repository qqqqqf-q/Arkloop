package data

import (
	"context"
	"errors"
	"fmt"

	workercrypto "arkloop/services/worker/internal/crypto"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type channelDeliveryExecer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type ChannelIdentityRecord struct {
	ID     uuid.UUID
	UserID *uuid.UUID
}

type DeliveryChannelRecord struct {
	ID          uuid.UUID
	ChannelType string
	Token       string
	ConfigJSON  []byte
}

type ChannelDeliveryRepository struct{}

func (ChannelDeliveryRepository) GetIdentity(ctx context.Context, pool *pgxpool.Pool, identityID uuid.UUID) (*ChannelIdentityRecord, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool must not be nil")
	}
	var item ChannelIdentityRecord
	err := pool.QueryRow(
		ctx,
		`SELECT id, user_id
		 FROM channel_identities
		 WHERE id = $1`,
		identityID,
	).Scan(&item.ID, &item.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("channel_delivery.GetIdentity: %w", err)
	}
	return &item, nil
}

func (ChannelDeliveryRepository) GetChannel(ctx context.Context, pool *pgxpool.Pool, channelID uuid.UUID) (*DeliveryChannelRecord, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool must not be nil")
	}
	var (
		item           DeliveryChannelRecord
		encryptedValue *string
		keyVersion     *int
		configJSON     []byte
	)
	err := pool.QueryRow(
		ctx,
		`SELECT c.id, c.channel_type, s.encrypted_value, s.key_version, COALESCE(c.config_json, '{}'::jsonb)
		 FROM channels c
		 LEFT JOIN secrets s ON s.id = c.credentials_id
		 WHERE c.id = $1 AND c.is_active = true`,
		channelID,
	).Scan(&item.ID, &item.ChannelType, &encryptedValue, &keyVersion, &configJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("channel_delivery.GetChannel: %w", err)
	}
	if encryptedValue == nil || *encryptedValue == "" || keyVersion == nil {
		return nil, fmt.Errorf("channel_delivery.GetChannel: missing channel token")
	}
	plaintext, err := workercrypto.DecryptWithKeyVersion(*encryptedValue, *keyVersion)
	if err != nil {
		return nil, fmt.Errorf("channel_delivery.GetChannel: decrypt token: %w", err)
	}
	item.Token = string(plaintext)
	item.ConfigJSON = configJSON
	return &item, nil
}

func (ChannelDeliveryRepository) GetChannelOwner(ctx context.Context, pool *pgxpool.Pool, channelID uuid.UUID) (*uuid.UUID, error) {
	if pool == nil {
		return nil, nil
	}
	var ownerUserID *uuid.UUID
	err := pool.QueryRow(ctx,
		`SELECT owner_user_id FROM channels WHERE id = $1`,
		channelID,
	).Scan(&ownerUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("channel_delivery.GetChannelOwner: %w", err)
	}
	return ownerUserID, nil
}

func (ChannelDeliveryRepository) GetChannelConfigJSON(ctx context.Context, pool *pgxpool.Pool, channelID uuid.UUID) ([]byte, error) {
	if pool == nil {
		return nil, nil
	}
	var configJSON []byte
	err := pool.QueryRow(ctx,
		`SELECT COALESCE(config_json, '{}'::jsonb) FROM channels WHERE id = $1`,
		channelID,
	).Scan(&configJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("channel_delivery.GetChannelConfigJSON: %w", err)
	}
	return configJSON, nil
}

func (ChannelDeliveryRepository) RecordDelivery(
	ctx context.Context,
	db channelDeliveryExecer,
	runID uuid.UUID,
	threadID uuid.UUID,
	channelID uuid.UUID,
	platformChatID string,
	platformMessageID string,
) error {
	if db == nil {
		return fmt.Errorf("db must not be nil")
	}
	if channelID == uuid.Nil {
		return fmt.Errorf("channel_id must not be empty")
	}
	if platformChatID == "" || platformMessageID == "" {
		return fmt.Errorf("platform ids must not be empty")
	}
	var runRef *uuid.UUID
	if runID != uuid.Nil {
		runRef = &runID
	}
	var threadRef *uuid.UUID
	if threadID != uuid.Nil {
		threadRef = &threadID
	}
	_, err := db.Exec(
		ctx,
		`INSERT INTO channel_message_deliveries (run_id, thread_id, channel_id, platform_chat_id, platform_message_id)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (channel_id, platform_chat_id, platform_message_id) DO NOTHING`,
		runRef,
		threadRef,
		channelID,
		platformChatID,
		platformMessageID,
	)
	if err != nil {
		return fmt.Errorf("channel_delivery.RecordDelivery: %w", err)
	}
	return nil
}
