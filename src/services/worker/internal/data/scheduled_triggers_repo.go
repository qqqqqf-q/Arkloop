//go:build !desktop

package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"arkloop/services/shared/runkind"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ScheduledTriggerRow 是 scheduled_triggers 表的一行。
type ScheduledTriggerRow struct {
	ID                uuid.UUID
	ChannelID         uuid.UUID
	ChannelIdentityID uuid.UUID
	PersonaKey        string
	AccountID         uuid.UUID
	Model             string
	IntervalMin       int
	NextFireAt        time.Time
	CooldownLevel     int
}

// ScheduledTriggersRepository 提供 heartbeat 调度操作（cloud / Postgres）。
type ScheduledTriggersRepository struct{}

func normalizeHeartbeatInterval(intervalMin int) int {
	if intervalMin <= 0 {
		return runkind.DefaultHeartbeatIntervalMinutes
	}
	return intervalMin
}

// UpsertHeartbeat 注册或更新某个 channel identity 的 heartbeat 调度。
func (ScheduledTriggersRepository) UpsertHeartbeat(
	ctx context.Context,
	db DB,
	accountID uuid.UUID,
	channelID uuid.UUID,
	channelIdentityID uuid.UUID,
	personaKey string,
	model string,
	intervalMin int,
) error {
	if channelID == uuid.Nil {
		return errors.New("channel_id must not be empty")
	}
	if channelIdentityID == uuid.Nil {
		return errors.New("channel_identity_id must not be empty")
	}
	intervalMin = normalizeHeartbeatInterval(intervalMin)
	nextFire := time.Now().UTC().Add(time.Duration(intervalMin) * time.Minute)
	_, err := db.Exec(ctx, `
		INSERT INTO scheduled_triggers
		    (id, channel_id, channel_identity_id, persona_key, account_id, model, interval_min, next_fire_at)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (channel_id, channel_identity_id) DO UPDATE
		    SET persona_key   = excluded.persona_key,
		        account_id    = excluded.account_id,
		        model         = excluded.model,
		        interval_min  = excluded.interval_min,
		        updated_at    = now()`,
		channelID, channelIdentityID, personaKey, accountID, model, intervalMin, nextFire,
	)
	return err
}

// GetHeartbeat returns the existing trigger for a channel identity.
func (ScheduledTriggersRepository) GetHeartbeat(
	ctx context.Context,
	db DB,
	channelID uuid.UUID,
	channelIdentityID uuid.UUID,
) (*ScheduledTriggerRow, error) {
	if channelID == uuid.Nil {
		return nil, errors.New("channel_id must not be empty")
	}
	if channelIdentityID == uuid.Nil {
		return nil, errors.New("channel_identity_id must not be empty")
	}

	var row ScheduledTriggerRow
	err := db.QueryRow(ctx, `
		SELECT id, channel_id, channel_identity_id, persona_key, account_id, model, interval_min, next_fire_at, cooldown_level
		  FROM scheduled_triggers
		 WHERE channel_id = $1
		   AND channel_identity_id = $2`,
		channelID,
		channelIdentityID,
	).Scan(
		&row.ID,
		&row.ChannelID,
		&row.ChannelIdentityID,
		&row.PersonaKey,
		&row.AccountID,
		&row.Model,
		&row.IntervalMin,
		&row.NextFireAt,
		&row.CooldownLevel,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

// ResetCooldownLevelAndNextFire resets cooldown_level and next_fire_at for a channel identity.
func (ScheduledTriggersRepository) ResetCooldownLevelAndNextFire(
	ctx context.Context,
	db DB,
	channelID uuid.UUID,
	channelIdentityID uuid.UUID,
	cooldownLevel int,
	nextFireAt time.Time,
) error {
	if channelID == uuid.Nil {
		return errors.New("channel_id must not be empty")
	}
	if channelIdentityID == uuid.Nil {
		return errors.New("channel_identity_id must not be empty")
	}
	_, err := db.Exec(ctx, `
		UPDATE scheduled_triggers
		   SET cooldown_level = $1,
		       next_fire_at = $2,
		       updated_at = now()
		 WHERE channel_id = $3
		   AND channel_identity_id = $4`,
		cooldownLevel, nextFireAt, channelID, channelIdentityID,
	)
	return err
}

// UpdateCooldownAfterHeartbeat updates cooldown_level and next_fire_at after a heartbeat run.
func (ScheduledTriggersRepository) UpdateCooldownAfterHeartbeat(
	ctx context.Context,
	db DB,
	channelID uuid.UUID,
	channelIdentityID uuid.UUID,
	newCooldownLevel int,
	nextFireAt time.Time,
) error {
	if channelID == uuid.Nil {
		return errors.New("channel_id must not be empty")
	}
	if channelIdentityID == uuid.Nil {
		return errors.New("channel_identity_id must not be empty")
	}
	_, err := db.Exec(ctx, `
		UPDATE scheduled_triggers
		   SET cooldown_level = $1,
		       next_fire_at = $2,
		       updated_at = now()
		 WHERE channel_id = $3
		   AND channel_identity_id = $4`,
		newCooldownLevel, nextFireAt, channelID, channelIdentityID,
	)
	return err
}

// ResetHeartbeatNextFire sets next_fire_at to now + interval_min for the provided channel identity.
func (ScheduledTriggersRepository) ResetHeartbeatNextFire(
	ctx context.Context,
	db DB,
	channelID uuid.UUID,
	channelIdentityID uuid.UUID,
	intervalMin int,
) (time.Time, error) {
	if channelID == uuid.Nil {
		return time.Time{}, errors.New("channel_id must not be empty")
	}
	if channelIdentityID == uuid.Nil {
		return time.Time{}, errors.New("channel_identity_id must not be empty")
	}
	intervalMin = normalizeHeartbeatInterval(intervalMin)
	nextFire := time.Now().UTC().Add(time.Duration(intervalMin) * time.Minute)
	cmd, err := db.Exec(ctx, `
		UPDATE scheduled_triggers
		   SET interval_min = $1,
		       next_fire_at = $2,
		       cooldown_level = 0,
		       updated_at = now()
		 WHERE channel_id = $3
		   AND channel_identity_id = $4`,
		intervalMin, nextFire, channelID, channelIdentityID,
	)
	if err != nil {
		return time.Time{}, err
	}
	if cmd.RowsAffected() == 0 {
		return time.Time{}, fmt.Errorf("reset heartbeat next fire: channel_identity_id %s not found", channelIdentityID)
	}
	return nextFire, nil
}

// DeleteHeartbeat 删除某个 channel identity 的 heartbeat 调度。
func (ScheduledTriggersRepository) DeleteHeartbeat(
	ctx context.Context,
	db DB,
	channelID uuid.UUID,
	channelIdentityID uuid.UUID,
) error {
	if channelID == uuid.Nil {
		return errors.New("channel_id must not be empty")
	}
	_, err := db.Exec(ctx,
		`DELETE FROM scheduled_triggers WHERE channel_id = $1 AND channel_identity_id = $2`,
		channelID,
		channelIdentityID,
	)
	return err
}

// HeartbeatIdentityConfig 是从 channel_identities 读到的 heartbeat 配置。
type HeartbeatIdentityConfig struct {
	Enabled         bool
	IntervalMinutes int
	Model           string
}

func GetDMBindingHeartbeatConfig(ctx context.Context, db DB, channelID uuid.UUID, identityID uuid.UUID) (*HeartbeatIdentityConfig, error) {
	var enabledInt, interval int
	var model string
	err := db.QueryRow(ctx,
		`SELECT heartbeat_enabled, heartbeat_interval_minutes, heartbeat_model
		   FROM channel_identity_links
		  WHERE channel_id = $1 AND channel_identity_id = $2`,
		channelID,
		identityID,
	).Scan(&enabledInt, &interval, &model)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get dm binding heartbeat config: %w", err)
	}
	return &HeartbeatIdentityConfig{
		Enabled:         enabledInt != 0,
		IntervalMinutes: interval,
		Model:           model,
	}, nil
}

// GetGroupHeartbeatConfig 通过 channel_type + platform_subject_id 查群 identity 的 heartbeat 配置（cloud）。
// 返回 identityID 供 UpsertHeartbeat 使用。
func GetGroupHeartbeatConfig(ctx context.Context, db DB, channelType, platformSubjectID string) (uuid.UUID, *HeartbeatIdentityConfig, error) {
	var enabledInt, interval int
	var model, idStr string
	err := db.QueryRow(ctx,
		`SELECT id, heartbeat_enabled, heartbeat_interval_minutes, heartbeat_model
		   FROM channel_identities
		  WHERE channel_type = $1 AND platform_subject_id = $2`,
		channelType, platformSubjectID,
	).Scan(&idStr, &enabledInt, &interval, &model)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, nil, nil
		}
		return uuid.Nil, nil, fmt.Errorf("get group heartbeat config: %w", err)
	}
	identityID, _ := uuid.Parse(idStr)
	return identityID, &HeartbeatIdentityConfig{
		Enabled:         enabledInt != 0,
		IntervalMinutes: interval,
		Model:           model,
	}, nil
}

// GetChannelIdentityHeartbeatConfig 从 channel_identities 读取 heartbeat 配置（cloud）。
func GetChannelIdentityHeartbeatConfig(ctx context.Context, db DB, identityID uuid.UUID) (*HeartbeatIdentityConfig, error) {
	var enabledInt, interval int
	var model string
	err := db.QueryRow(ctx,
		`SELECT heartbeat_enabled, heartbeat_interval_minutes, heartbeat_model
		   FROM channel_identities WHERE id = $1`,
		identityID,
	).Scan(&enabledInt, &interval, &model)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get channel identity heartbeat config: %w", err)
	}
	return &HeartbeatIdentityConfig{
		Enabled:         enabledInt != 0,
		IntervalMinutes: interval,
		Model:           model,
	}, nil
}
