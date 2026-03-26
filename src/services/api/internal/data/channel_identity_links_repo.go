package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ChannelIdentityLink struct {
	ID                uuid.UUID
	ChannelID         uuid.UUID
	ChannelIdentityID uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type ChannelBinding struct {
	BindingID                uuid.UUID
	ChannelID                uuid.UUID
	ChannelIdentityID        uuid.UUID
	UserID                   *uuid.UUID
	DisplayName              *string
	PlatformSubjectID        string
	IsOwner                  bool
	HeartbeatEnabled         bool
	HeartbeatIntervalMinutes int
	HeartbeatModel           string
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

type ChannelIdentityLinksRepository struct {
	db Querier
}

func NewChannelIdentityLinksRepository(db Querier) (*ChannelIdentityLinksRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &ChannelIdentityLinksRepository{db: db}, nil
}

func (r *ChannelIdentityLinksRepository) WithTx(tx pgx.Tx) *ChannelIdentityLinksRepository {
	return &ChannelIdentityLinksRepository{db: tx}
}

const channelIdentityLinkColumns = `id, channel_id, channel_identity_id, created_at, updated_at`

func scanChannelIdentityLink(row interface{ Scan(dest ...any) error }) (ChannelIdentityLink, error) {
	var item ChannelIdentityLink
	err := row.Scan(
		&item.ID,
		&item.ChannelID,
		&item.ChannelIdentityID,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func (r *ChannelIdentityLinksRepository) Upsert(
	ctx context.Context,
	channelID uuid.UUID,
	channelIdentityID uuid.UUID,
) (ChannelIdentityLink, error) {
	if channelID == uuid.Nil || channelIdentityID == uuid.Nil {
		return ChannelIdentityLink{}, fmt.Errorf("channel_identity_links: ids must not be empty")
	}
	item, err := scanChannelIdentityLink(r.db.QueryRow(
		ctx,
		`INSERT INTO channel_identity_links (channel_id, channel_identity_id)
		 VALUES ($1, $2)
		 ON CONFLICT (channel_id, channel_identity_id)
		 DO UPDATE SET updated_at = now()
		 RETURNING `+channelIdentityLinkColumns,
		channelID,
		channelIdentityID,
	))
	if err != nil {
		return ChannelIdentityLink{}, fmt.Errorf("channel_identity_links.Upsert: %w", err)
	}
	return item, nil
}

func (r *ChannelIdentityLinksRepository) GetBinding(
	ctx context.Context,
	accountID uuid.UUID,
	channelID uuid.UUID,
	bindingID uuid.UUID,
) (*ChannelBinding, error) {
	item, err := scanChannelBinding(r.db.QueryRow(
		ctx,
		`SELECT cil.id,
		        cil.channel_id,
		        ci.id,
		        ci.user_id,
		        ci.display_name,
		        ci.platform_subject_id,
		        CASE
		            WHEN ch.owner_user_id IS NOT NULL AND ci.user_id = ch.owner_user_id THEN TRUE
		            ELSE FALSE
		        END AS is_owner,
		        COALESCE(ci.heartbeat_enabled, 0),
		        COALESCE(ci.heartbeat_interval_minutes, 30),
		        COALESCE(ci.heartbeat_model, ''),
		        cil.created_at,
		        cil.updated_at
		   FROM channel_identity_links cil
		   JOIN channels ch ON ch.id = cil.channel_id
		   JOIN channel_identities ci ON ci.id = cil.channel_identity_id
		  WHERE cil.id = $1
		    AND cil.channel_id = $2
		    AND ch.account_id = $3`,
		bindingID,
		channelID,
		accountID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("channel_identity_links.GetBinding: %w", err)
	}
	return &item, nil
}

func (r *ChannelIdentityLinksRepository) ListBindings(
	ctx context.Context,
	accountID uuid.UUID,
	channelID uuid.UUID,
) ([]ChannelBinding, error) {
	rows, err := r.db.Query(
		ctx,
		`SELECT cil.id,
		        cil.channel_id,
		        ci.id,
		        ci.user_id,
		        ci.display_name,
		        ci.platform_subject_id,
		        CASE
		            WHEN ch.owner_user_id IS NOT NULL AND ci.user_id = ch.owner_user_id THEN TRUE
		            ELSE FALSE
		        END AS is_owner,
		        COALESCE(ci.heartbeat_enabled, 0),
		        COALESCE(ci.heartbeat_interval_minutes, 30),
		        COALESCE(ci.heartbeat_model, ''),
		        cil.created_at,
		        cil.updated_at
		   FROM channel_identity_links cil
		   JOIN channels ch ON ch.id = cil.channel_id
		   JOIN channel_identities ci ON ci.id = cil.channel_identity_id
		  WHERE cil.channel_id = $1
		    AND ch.account_id = $2
		  ORDER BY is_owner DESC, cil.created_at ASC`,
		channelID,
		accountID,
	)
	if err != nil {
		return nil, fmt.Errorf("channel_identity_links.ListBindings: %w", err)
	}
	defer rows.Close()

	items := make([]ChannelBinding, 0)
	for rows.Next() {
		item, scanErr := scanChannelBinding(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("channel_identity_links.ListBindings scan: %w", scanErr)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *ChannelIdentityLinksRepository) DeleteBinding(
	ctx context.Context,
	accountID uuid.UUID,
	channelID uuid.UUID,
	bindingID uuid.UUID,
) error {
	tag, err := r.db.Exec(
		ctx,
		`DELETE FROM channel_identity_links
		  WHERE id = $1
		    AND channel_id = $2
		    AND EXISTS (
		        SELECT 1
		          FROM channels
		         WHERE channels.id = channel_identity_links.channel_id
		           AND channels.account_id = $3
		    )`,
		bindingID,
		channelID,
		accountID,
	)
	if err != nil {
		return fmt.Errorf("channel_identity_links.DeleteBinding: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("channel_identity_links.DeleteBinding: not found")
	}
	return nil
}

func scanChannelBinding(row interface{ Scan(dest ...any) error }) (ChannelBinding, error) {
	var item ChannelBinding
	var enabledInt int
	err := row.Scan(
		&item.BindingID,
		&item.ChannelID,
		&item.ChannelIdentityID,
		&item.UserID,
		&item.DisplayName,
		&item.PlatformSubjectID,
		&item.IsOwner,
		&enabledInt,
		&item.HeartbeatIntervalMinutes,
		&item.HeartbeatModel,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return ChannelBinding{}, err
	}
	item.HeartbeatEnabled = enabledInt != 0
	return item, nil
}
