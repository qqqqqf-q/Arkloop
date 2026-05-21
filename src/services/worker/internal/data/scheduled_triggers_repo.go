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

// ErrHeartbeatSnapshotStale 表示 heartbeat 执行期间有新消息到达，快照保护阻止了冷却更新。
var ErrHeartbeatSnapshotStale = errors.New("heartbeat snapshot stale")

// ScheduledTriggerRow 是 scheduled_triggers 表的一行。
type ScheduledTriggerRow struct {
	ID                    uuid.UUID
	ChannelID             uuid.UUID
	ChannelIdentityID     uuid.UUID
	ThreadID              *uuid.UUID
	PersonaKey            string
	AccountID             uuid.UUID
	Model                 string
	ResolveModelAtRuntime bool
	IntervalMin           int
	NextFireAt            time.Time
	CooldownLevel         int
	LastUserMsgAt         *time.Time
	BurstStartAt          *time.Time
}

func (ScheduledTriggersRepository) UpsertHeartbeatForThread(
	ctx context.Context,
	db DB,
	accountID uuid.UUID,
	channelID uuid.UUID,
	channelIdentityID uuid.UUID,
	threadID uuid.UUID,
	personaKey string,
	model string,
	resolveModelAtRuntime bool,
	intervalMin int,
) error {
	if threadID == uuid.Nil {
		return errors.New("thread_id must not be empty")
	}
	if channelID == uuid.Nil {
		return errors.New("channel_id must not be empty")
	}
	if channelIdentityID == uuid.Nil {
		return errors.New("channel_identity_id must not be empty")
	}
	intervalMin = normalizeHeartbeatInterval(intervalMin)
	nextFire := time.Now().UTC().Add(time.Duration(intervalMin) * time.Minute)
	if _, err := db.Exec(ctx, `
		DELETE FROM scheduled_triggers
		 WHERE channel_id = $1
		   AND channel_identity_id = $2
		   AND thread_id IS NULL`,
		channelID, channelIdentityID,
	); err != nil {
		return err
	}
	_, err := db.Exec(ctx, `
		INSERT INTO scheduled_triggers
		    (id, channel_id, channel_identity_id, thread_id, persona_key, account_id, model, resolve_model_at_runtime, interval_min, next_fire_at, trigger_kind)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (thread_id) WHERE thread_id IS NOT NULL DO UPDATE
		    SET thread_id      = excluded.thread_id,
		        channel_id     = excluded.channel_id,
		        channel_identity_id = excluded.channel_identity_id,
		        persona_key    = excluded.persona_key,
		        account_id     = excluded.account_id,
		        model          = excluded.model,
		        resolve_model_at_runtime = excluded.resolve_model_at_runtime,
		        interval_min   = excluded.interval_min,
		        trigger_kind   = excluded.trigger_kind,
		        cooldown_level = 0,
		        updated_at     = now()`,
		channelID, channelIdentityID, threadID, personaKey, accountID, model, resolveModelAtRuntime, intervalMin, nextFire, runkind.Discuss,
	)
	return err
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
		    (id, channel_id, channel_identity_id, persona_key, account_id, model, interval_min, next_fire_at, trigger_kind)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (channel_id, channel_identity_id) WHERE thread_id IS NULL DO UPDATE
		    SET persona_key   = excluded.persona_key,
		        account_id    = excluded.account_id,
		        model         = excluded.model,
		        interval_min  = excluded.interval_min,
		        trigger_kind  = excluded.trigger_kind,
		        updated_at    = now()`,
		channelID, channelIdentityID, personaKey, accountID, model, intervalMin, nextFire, runkind.Discuss,
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
		SELECT id, channel_id, channel_identity_id, thread_id, persona_key, account_id, model, resolve_model_at_runtime, interval_min, next_fire_at, cooldown_level, last_user_msg_at, burst_start_at
		  FROM scheduled_triggers
		 WHERE channel_id = $1
		   AND channel_identity_id = $2
		   AND thread_id IS NULL`,
		channelID,
		channelIdentityID,
	).Scan(
		&row.ID,
		&row.ChannelID,
		&row.ChannelIdentityID,
		&row.ThreadID,
		&row.PersonaKey,
		&row.AccountID,
		&row.Model,
		&row.ResolveModelAtRuntime,
		&row.IntervalMin,
		&row.NextFireAt,
		&row.CooldownLevel,
		&row.LastUserMsgAt,
		&row.BurstStartAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func (ScheduledTriggersRepository) GetHeartbeatForThread(
	ctx context.Context,
	db DB,
	threadID uuid.UUID,
) (*ScheduledTriggerRow, error) {
	if threadID == uuid.Nil {
		return nil, errors.New("thread_id must not be empty")
	}
	var row ScheduledTriggerRow
	err := db.QueryRow(ctx, `
		SELECT id, channel_id, channel_identity_id, thread_id, persona_key, account_id, model, resolve_model_at_runtime, interval_min, next_fire_at, cooldown_level, last_user_msg_at, burst_start_at
		  FROM scheduled_triggers
		 WHERE thread_id = $1`,
		threadID,
	).Scan(
		&row.ID,
		&row.ChannelID,
		&row.ChannelIdentityID,
		&row.ThreadID,
		&row.PersonaKey,
		&row.AccountID,
		&row.Model,
		&row.ResolveModelAtRuntime,
		&row.IntervalMin,
		&row.NextFireAt,
		&row.CooldownLevel,
		&row.LastUserMsgAt,
		&row.BurstStartAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

// ResetCooldownForMessage updates cooldown state when a new message arrives.
func (ScheduledTriggersRepository) ResetCooldownForMessage(
	ctx context.Context,
	db DB,
	channelID uuid.UUID,
	channelIdentityID uuid.UUID,
	nextFireAt time.Time,
	lastUserMsgAt time.Time,
	burstStartAt time.Time,
) error {
	if channelID == uuid.Nil {
		return errors.New("channel_id must not be empty")
	}
	if channelIdentityID == uuid.Nil {
		return errors.New("channel_identity_id must not be empty")
	}
	_, err := db.Exec(ctx, `
		UPDATE scheduled_triggers
		   SET cooldown_level = 0,
		       next_fire_at = $1,
		       last_user_msg_at = $2,
		       burst_start_at = $3,
		       updated_at = now()
		 WHERE channel_id = $4
		   AND channel_identity_id = $5
		   AND thread_id IS NULL`,
		nextFireAt, lastUserMsgAt, burstStartAt, channelID, channelIdentityID,
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
	lastUserMsgSnapshot *time.Time,
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
		   AND channel_identity_id = $4
		   AND (last_user_msg_at IS NOT DISTINCT FROM $5)
		   AND thread_id IS NULL`,
		newCooldownLevel, nextFireAt, channelID, channelIdentityID, lastUserMsgSnapshot,
	)
	return err
}

func (ScheduledTriggersRepository) UpdateCooldownAfterHeartbeatForThread(
	ctx context.Context,
	db DB,
	threadID uuid.UUID,
	newCooldownLevel int,
	nextFireAt time.Time,
	lastUserMsgSnapshot *time.Time,
) error {
	if threadID == uuid.Nil {
		return errors.New("thread_id must not be empty")
	}
	_, err := db.Exec(ctx, `
		UPDATE scheduled_triggers
		   SET cooldown_level = $1,
		       next_fire_at = $2,
		       updated_at = now()
		 WHERE thread_id = $3
		   AND (last_user_msg_at IS NOT DISTINCT FROM $4)`,
		newCooldownLevel, nextFireAt, threadID, lastUserMsgSnapshot,
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
		   AND channel_identity_id = $4
		   AND thread_id IS NULL`,
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

func (ScheduledTriggersRepository) ResetHeartbeatNextFireForThread(
	ctx context.Context,
	db DB,
	threadID uuid.UUID,
	intervalMin int,
) (time.Time, error) {
	if threadID == uuid.Nil {
		return time.Time{}, errors.New("thread_id must not be empty")
	}
	intervalMin = normalizeHeartbeatInterval(intervalMin)
	nextFire := time.Now().UTC().Add(time.Duration(intervalMin) * time.Minute)
	cmd, err := db.Exec(ctx, `
		UPDATE scheduled_triggers
		   SET interval_min = $1,
		       next_fire_at = $2,
		       cooldown_level = 0,
		       updated_at = now()
		 WHERE thread_id = $3`,
		intervalMin, nextFire, threadID,
	)
	if err != nil {
		return time.Time{}, err
	}
	if cmd.RowsAffected() == 0 {
		return time.Time{}, fmt.Errorf("reset heartbeat next fire: thread_id %s not found", threadID)
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
		`DELETE FROM scheduled_triggers WHERE channel_id = $1 AND channel_identity_id = $2 AND thread_id IS NULL`,
		channelID,
		channelIdentityID,
	)
	return err
}

func (ScheduledTriggersRepository) DeleteHeartbeatForThread(
	ctx context.Context,
	db DB,
	threadID uuid.UUID,
) error {
	if threadID == uuid.Nil {
		return errors.New("thread_id must not be empty")
	}
	_, err := db.Exec(ctx, `DELETE FROM scheduled_triggers WHERE thread_id = $1`, threadID)
	return err
}

// HeartbeatIdentityConfig 是 thread 级 heartbeat 配置。
type HeartbeatIdentityConfig struct {
	Enabled         bool
	IntervalMinutes int
	Model           string
}

func GetChannelIdentityIDBySubject(ctx context.Context, db DB, channelType, platformSubjectID string) (uuid.UUID, error) {
	var idStr string
	err := db.QueryRow(ctx,
		`SELECT id
		   FROM channel_identities
		  WHERE channel_type = $1 AND platform_subject_id = $2`,
		channelType, platformSubjectID,
	).Scan(&idStr)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, nil
		}
		return uuid.Nil, fmt.Errorf("get channel identity id: %w", err)
	}
	identityID, _ := uuid.Parse(idStr)
	return identityID, nil
}
