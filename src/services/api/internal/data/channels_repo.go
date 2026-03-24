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

type Channel struct {
	ID            uuid.UUID
	AccountID     uuid.UUID
	ChannelType   string
	PersonaID     *uuid.UUID
	CredentialsID *uuid.UUID
	OwnerUserID   *uuid.UUID
	WebhookSecret *string
	WebhookURL    *string
	IsActive      bool
	ConfigJSON    json.RawMessage
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type ChannelsRepository struct {
	db Querier
}

func NewChannelsRepository(db Querier) (*ChannelsRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &ChannelsRepository{db: db}, nil
}

func (r *ChannelsRepository) WithTx(tx pgx.Tx) *ChannelsRepository {
	return &ChannelsRepository{db: tx}
}

func ChannelSecretName(channelID uuid.UUID) string {
	return fmt.Sprintf("channel_cred:%s", channelID.String())
}

var channelColumns = `id, account_id, channel_type, persona_id, credentials_id, owner_user_id,
	webhook_secret, webhook_url, is_active, config_json, created_at, updated_at`

func scanChannel(row interface{ Scan(dest ...any) error }) (Channel, error) {
	var ch Channel
	err := row.Scan(
		&ch.ID, &ch.AccountID, &ch.ChannelType, &ch.PersonaID, &ch.CredentialsID,
		&ch.OwnerUserID,
		&ch.WebhookSecret, &ch.WebhookURL, &ch.IsActive, &ch.ConfigJSON,
		&ch.CreatedAt, &ch.UpdatedAt,
	)
	return ch, err
}

func (r *ChannelsRepository) Create(ctx context.Context, id uuid.UUID, accountID uuid.UUID, channelType string, personaID *uuid.UUID, credentialsID *uuid.UUID, ownerUserID *uuid.UUID, webhookSecret, webhookURL string, configJSON json.RawMessage) (Channel, error) {
	if id == uuid.Nil {
		id = uuid.New()
	}
	if accountID == uuid.Nil {
		return Channel{}, fmt.Errorf("channels: account_id must not be empty")
	}
	channelType = strings.TrimSpace(channelType)
	if channelType == "" {
		return Channel{}, fmt.Errorf("channels: channel_type must not be empty")
	}
	if configJSON == nil {
		configJSON = json.RawMessage(`{}`)
	}

	ch, err := scanChannel(r.db.QueryRow(ctx,
		`INSERT INTO channels (id, account_id, channel_type, persona_id, credentials_id, owner_user_id, webhook_secret, webhook_url, config_json)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING `+channelColumns,
		id, accountID, channelType, personaID, credentialsID, ownerUserID, webhookSecret, webhookURL, configJSON,
	))
	if err != nil {
		return Channel{}, fmt.Errorf("channels.Create: %w", err)
	}
	return ch, nil
}

func (r *ChannelsRepository) GetByID(ctx context.Context, id uuid.UUID) (*Channel, error) {
	ch, err := scanChannel(r.db.QueryRow(ctx,
		`SELECT `+channelColumns+` FROM channels WHERE id = $1`, id,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("channels.GetByID: %w", err)
	}
	return &ch, nil
}

func (r *ChannelsRepository) GetByAccountAndType(ctx context.Context, accountID uuid.UUID, channelType string) (*Channel, error) {
	ch, err := scanChannel(r.db.QueryRow(ctx,
		`SELECT `+channelColumns+` FROM channels WHERE account_id = $1 AND channel_type = $2`,
		accountID, channelType,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("channels.GetByAccountAndType: %w", err)
	}
	return &ch, nil
}

func (r *ChannelsRepository) ListByAccount(ctx context.Context, accountID uuid.UUID) ([]Channel, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+channelColumns+` FROM channels WHERE account_id = $1 ORDER BY created_at ASC`,
		accountID,
	)
	if err != nil {
		return nil, fmt.Errorf("channels.ListByAccount: %w", err)
	}
	defer rows.Close()

	var channels []Channel
	for rows.Next() {
		ch, err := scanChannel(rows)
		if err != nil {
			return nil, fmt.Errorf("channels.ListByAccount scan: %w", err)
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

func (r *ChannelsRepository) ListActiveByType(ctx context.Context, channelType string) ([]Channel, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+channelColumns+`
		   FROM channels
		  WHERE channel_type = $1
		    AND is_active = true
		  ORDER BY created_at ASC`,
		channelType,
	)
	if err != nil {
		return nil, fmt.Errorf("channels.ListActiveByType: %w", err)
	}
	defer rows.Close()

	items := make([]Channel, 0)
	for rows.Next() {
		ch, err := scanChannel(rows)
		if err != nil {
			return nil, fmt.Errorf("channels.ListActiveByType scan: %w", err)
		}
		items = append(items, ch)
	}
	return items, rows.Err()
}

type ChannelUpdate struct {
	PersonaID     **uuid.UUID
	CredentialsID **uuid.UUID
	OwnerUserID   **uuid.UUID
	IsActive      *bool
	ConfigJSON    *json.RawMessage
}

func (r *ChannelsRepository) Update(ctx context.Context, id uuid.UUID, accountID uuid.UUID, upd ChannelUpdate) (*Channel, error) {
	sets := []string{}
	args := []any{}
	idx := 1

	if upd.PersonaID != nil {
		sets = append(sets, fmt.Sprintf("persona_id = $%d", idx))
		args = append(args, *upd.PersonaID)
		idx++
	}
	if upd.CredentialsID != nil {
		sets = append(sets, fmt.Sprintf("credentials_id = $%d", idx))
		args = append(args, *upd.CredentialsID)
		idx++
	}
	if upd.OwnerUserID != nil {
		sets = append(sets, fmt.Sprintf("owner_user_id = $%d", idx))
		args = append(args, *upd.OwnerUserID)
		idx++
	}
	if upd.IsActive != nil {
		sets = append(sets, fmt.Sprintf("is_active = $%d", idx))
		args = append(args, *upd.IsActive)
		idx++
	}
	if upd.ConfigJSON != nil {
		sets = append(sets, fmt.Sprintf("config_json = $%d", idx))
		args = append(args, *upd.ConfigJSON)
		idx++
	}

	if len(sets) == 0 {
		return r.GetByID(ctx, id)
	}

	sets = append(sets, "updated_at = now()")
	args = append(args, id, accountID)

	query := fmt.Sprintf(
		`UPDATE channels SET %s WHERE id = $%d AND account_id = $%d RETURNING %s`,
		strings.Join(sets, ", "), idx, idx+1, channelColumns,
	)

	ch, err := scanChannel(r.db.QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("channels.Update: %w", err)
	}
	return &ch, nil
}

func (r *ChannelsRepository) Delete(ctx context.Context, id uuid.UUID, accountID uuid.UUID) error {
	tag, err := r.db.Exec(ctx,
		`DELETE FROM channels WHERE id = $1 AND account_id = $2`,
		id, accountID,
	)
	if err != nil {
		return fmt.Errorf("channels.Delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("channels.Delete: not found")
	}
	return nil
}
