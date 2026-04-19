package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"arkloop/services/shared/pgnotify"
	"arkloop/services/shared/runkind"

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
	PreferredModel    string
	ReasoningMode     string
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

// UpdateHeartbeatConfig 更新 channel identity 的 heartbeat 配置。
func (r *ChannelIdentitiesRepository) UpdateHeartbeatConfig(ctx context.Context, id uuid.UUID, enabled bool, intervalMinutes int, model string) error {
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	if intervalMinutes <= 0 {
		intervalMinutes = runkind.DefaultHeartbeatIntervalMinutes
	}
	tag, err := r.db.Exec(ctx,
		`UPDATE channel_identities
		    SET heartbeat_enabled = $2,
		        heartbeat_interval_minutes = $3,
		        heartbeat_model = $4,
		        updated_at = now()
		  WHERE id = $1`,
		id, enabledInt, intervalMinutes, model,
	)
	if err != nil {
		return fmt.Errorf("channel_identities.UpdateHeartbeatConfig: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("channel_identities.UpdateHeartbeatConfig: not found")
	}

	now := time.Now().UTC()
	if !enabled {
		if _, err := r.db.Exec(ctx,
			`DELETE FROM scheduled_triggers WHERE channel_identity_id = $1`,
			id,
		); err != nil {
			return fmt.Errorf("channel_identities.UpdateHeartbeatConfig: delete trigger: %w", err)
		}
	} else {
		nextFire := now.Add(time.Duration(intervalMinutes) * time.Minute)
		if _, err := r.db.Exec(ctx, `
			UPDATE scheduled_triggers
			   SET interval_min = $2,
			       model = $3,
			       next_fire_at = CASE
			           WHEN interval_min <> $2 THEN $4
			           ELSE next_fire_at
			       END,
			       updated_at = now()
			 WHERE channel_identity_id = $1`,
			id,
			intervalMinutes,
			model,
			nextFire,
		); err != nil {
			return fmt.Errorf("channel_identities.UpdateHeartbeatConfig: sync trigger: %w", err)
		}
	}
	_, _ = r.db.Exec(ctx, "SELECT pg_notify($1, '')", pgnotify.ChannelHeartbeat)
	return nil
}

// GetHeartbeatConfig 返回 channel identity 的 heartbeat 配置。
func (r *ChannelIdentitiesRepository) GetHeartbeatConfig(ctx context.Context, id uuid.UUID) (enabled bool, intervalMinutes int, model string, err error) {
	var enabledInt int
	err = r.db.QueryRow(ctx,
		`SELECT heartbeat_enabled, heartbeat_interval_minutes, heartbeat_model
		   FROM channel_identities WHERE id = $1`,
		id,
	).Scan(&enabledInt, &intervalMinutes, &model)
	if err != nil {
		return false, 0, "", fmt.Errorf("channel_identities.GetHeartbeatConfig: %w", err)
	}
	return enabledInt != 0, intervalMinutes, model, nil
}

// UpdatePreferenceConfig 更新 channel identity 的模型偏好配置。
func (r *ChannelIdentitiesRepository) UpdatePreferenceConfig(ctx context.Context, id uuid.UUID, preferredModel string, reasoningMode string) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE channel_identities
		    SET preferred_model = $2,
		        reasoning_mode = $3,
		        updated_at = now()
		  WHERE id = $1`,
		id, preferredModel, reasoningMode,
	)
	if err != nil {
		return fmt.Errorf("channel_identities.UpdatePreferenceConfig: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("channel_identities.UpdatePreferenceConfig: not found")
	}
	return nil
}

// GetPreferenceConfig 返回 channel identity 的模型偏好配置。
func (r *ChannelIdentitiesRepository) GetPreferenceConfig(ctx context.Context, id uuid.UUID) (preferredModel string, reasoningMode string, err error) {
	err = r.db.QueryRow(ctx,
		`SELECT preferred_model, reasoning_mode FROM channel_identities WHERE id = $1`,
		id,
	).Scan(&preferredModel, &reasoningMode)
	if err != nil {
		return "", "", fmt.Errorf("channel_identities.GetPreferenceConfig: %w", err)
	}
	return preferredModel, reasoningMode, nil
}
