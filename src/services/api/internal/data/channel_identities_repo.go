package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ChannelIdentity struct {
	ID                uuid.UUID
	ChannelType       string
	PlatformSubjectID string
	UserID            *uuid.UUID
	DisplayName       *string
	AvatarURL         *string
	Metadata          json.RawMessage
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type ChannelIdentitiesRepository struct {
	db Querier
}

func NewChannelIdentitiesRepository(db Querier) (*ChannelIdentitiesRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &ChannelIdentitiesRepository{db: db}, nil
}

func (r *ChannelIdentitiesRepository) WithTx(tx pgx.Tx) *ChannelIdentitiesRepository {
	return &ChannelIdentitiesRepository{db: tx}
}

var identityColumns = `id, channel_type, platform_subject_id, user_id, display_name, avatar_url, metadata, created_at, updated_at`

func scanIdentity(row interface{ Scan(dest ...any) error }) (ChannelIdentity, error) {
	var ci ChannelIdentity
	err := row.Scan(
		&ci.ID, &ci.ChannelType, &ci.PlatformSubjectID, &ci.UserID,
		&ci.DisplayName, &ci.AvatarURL, &ci.Metadata,
		&ci.CreatedAt, &ci.UpdatedAt,
	)
	return ci, err
}

// Upsert 按 (channel_type, platform_subject_id) 创建或更新身份记录。
func (r *ChannelIdentitiesRepository) Upsert(ctx context.Context, channelType, platformSubjectID string, displayName *string, avatarURL *string, metadata json.RawMessage) (ChannelIdentity, error) {
	if channelType == "" {
		return ChannelIdentity{}, fmt.Errorf("channel_identities: channel_type must not be empty")
	}
	if platformSubjectID == "" {
		return ChannelIdentity{}, fmt.Errorf("channel_identities: platform_subject_id must not be empty")
	}
	if metadata == nil {
		metadata = json.RawMessage(`{}`)
	}

	ci, err := scanIdentity(r.db.QueryRow(ctx,
		`INSERT INTO channel_identities (channel_type, platform_subject_id, display_name, avatar_url, metadata)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (channel_type, platform_subject_id)
		 DO UPDATE SET
		     display_name = COALESCE(EXCLUDED.display_name, channel_identities.display_name),
		     avatar_url   = COALESCE(EXCLUDED.avatar_url, channel_identities.avatar_url),
		     metadata     = EXCLUDED.metadata,
		     updated_at   = now()
		 RETURNING `+identityColumns,
		channelType, platformSubjectID, displayName, avatarURL, metadata,
	))
	if err != nil {
		return ChannelIdentity{}, fmt.Errorf("channel_identities.Upsert: %w", err)
	}
	return ci, nil
}

func (r *ChannelIdentitiesRepository) GetByID(ctx context.Context, id uuid.UUID) (*ChannelIdentity, error) {
	ci, err := scanIdentity(r.db.QueryRow(ctx,
		`SELECT `+identityColumns+` FROM channel_identities WHERE id = $1`, id,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("channel_identities.GetByID: %w", err)
	}
	return &ci, nil
}

func (r *ChannelIdentitiesRepository) GetByChannelAndSubject(ctx context.Context, channelType, platformSubjectID string) (*ChannelIdentity, error) {
	ci, err := scanIdentity(r.db.QueryRow(ctx,
		`SELECT `+identityColumns+`
		 FROM channel_identities
		 WHERE channel_type = $1 AND platform_subject_id = $2`,
		channelType, platformSubjectID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("channel_identities.GetByChannelAndSubject: %w", err)
	}
	return &ci, nil
}

func (r *ChannelIdentitiesRepository) ListByUserID(ctx context.Context, userID uuid.UUID) ([]ChannelIdentity, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+identityColumns+` FROM channel_identities WHERE user_id = $1 ORDER BY created_at ASC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("channel_identities.ListByUserID: %w", err)
	}
	defer rows.Close()

	var identities []ChannelIdentity
	for rows.Next() {
		ci, err := scanIdentity(rows)
		if err != nil {
			return nil, fmt.Errorf("channel_identities.ListByUserID scan: %w", err)
		}
		identities = append(identities, ci)
	}
	return identities, rows.Err()
}

// UpdateUserID 更新身份记录的 user_id 关联。
func (r *ChannelIdentitiesRepository) UpdateUserID(ctx context.Context, id uuid.UUID, userID *uuid.UUID) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE channel_identities SET user_id = $2, updated_at = now() WHERE id = $1`,
		id, userID,
	)
	if err != nil {
		return fmt.Errorf("channel_identities.UpdateUserID: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("channel_identities.UpdateUserID: not found")
	}
	return nil
}
