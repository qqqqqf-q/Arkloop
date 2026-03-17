package data

import (
	"context"
	"errors"
	"fmt"

	workercrypto "arkloop/services/worker/internal/crypto"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ChannelIdentityRecord struct {
	ID     uuid.UUID
	UserID *uuid.UUID
}

type DeliveryChannelRecord struct {
	ID          uuid.UUID
	ChannelType string
	Token       string
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
	)
	err := pool.QueryRow(
		ctx,
		`SELECT c.id, c.channel_type, s.encrypted_value
		 FROM channels c
		 LEFT JOIN secrets s ON s.id = c.credentials_id
		 WHERE c.id = $1 AND c.is_active = true`,
		channelID,
	).Scan(&item.ID, &item.ChannelType, &encryptedValue)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("channel_delivery.GetChannel: %w", err)
	}
	if encryptedValue == nil || *encryptedValue == "" {
		return nil, fmt.Errorf("channel_delivery.GetChannel: missing telegram token")
	}
	plaintext, err := workercrypto.DecryptGCM(*encryptedValue)
	if err != nil {
		return nil, fmt.Errorf("channel_delivery.GetChannel: decrypt token: %w", err)
	}
	item.Token = string(plaintext)
	return &item, nil
}
