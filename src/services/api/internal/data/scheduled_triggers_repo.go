package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"arkloop/services/shared/runkind"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ScheduledTriggerRow represents one row from scheduled_triggers.
type ScheduledTriggerRow struct {
	ID                uuid.UUID
	ChannelID         uuid.UUID
	ChannelIdentityID uuid.UUID
	PersonaKey        string
	AccountID         uuid.UUID
	Model             string
	IntervalMin       int
	NextFireAt        time.Time
	TriggerKind       string
	JobID             uuid.UUID
}

// ScheduledTriggersRepository provides heartbeat scheduling operations.
type ScheduledTriggersRepository struct{}

const heartbeatActivityWindow = 24 * time.Hour

type HeartbeatThreadContext struct {
	ThreadID         uuid.UUID
	AccountID        uuid.UUID
	CreatedByUserID  *uuid.UUID
	ChannelID        uuid.UUID
	ChannelType      string
	PlatformChatID   string
	IdentityID       uuid.UUID
	ConversationType string
}

func normalizeHeartbeatInterval(intervalMin int) int {
	if intervalMin <= 0 {
		return runkind.DefaultHeartbeatIntervalMinutes
	}
	return intervalMin
}

// UpsertHeartbeat registers or updates a heartbeat schedule for a channel identity.
func (ScheduledTriggersRepository) UpsertHeartbeat(
	ctx context.Context,
	db Querier,
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
	triggerID := uuid.New()
	_, err := db.Exec(ctx, `
		INSERT INTO scheduled_triggers
		    (id, channel_id, channel_identity_id, persona_key, account_id, model, interval_min, next_fire_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now(), now())
		ON CONFLICT (channel_id, channel_identity_id) DO UPDATE
		    SET persona_key   = excluded.persona_key,
		        account_id    = excluded.account_id,
		        model         = excluded.model,
		        interval_min  = excluded.interval_min,
		        updated_at    = now()`,
		triggerID, channelID, channelIdentityID, personaKey, accountID, model, intervalMin, nextFire,
	)
	return err
}

// GetHeartbeat returns the existing trigger for a channel identity.
func (ScheduledTriggersRepository) GetHeartbeat(
	ctx context.Context,
	db Querier,
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
		SELECT id, channel_id, channel_identity_id, persona_key, account_id, model, interval_min, next_fire_at
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
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

// ResetHeartbeatNextFire sets next_fire_at to now + interval_min for the provided channel identity.
func (ScheduledTriggersRepository) ResetHeartbeatNextFire(
	ctx context.Context,
	db Querier,
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

// RescheduleHeartbeatNextFireAt forces next_fire_at to the provided time for the given trigger ID.
func (ScheduledTriggersRepository) RescheduleHeartbeatNextFireAt(
	ctx context.Context,
	db Querier,
	id uuid.UUID,
	nextFireAt time.Time,
) error {
	if id == uuid.Nil {
		return errors.New("id must not be empty")
	}
	if nextFireAt.IsZero() {
		return errors.New("next_fire_at must not be zero")
	}
	cmd, err := db.Exec(ctx, `
		UPDATE scheduled_triggers
		   SET next_fire_at = $1,
		       updated_at = now()
		 WHERE id = $2`,
		nextFireAt, id,
	)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return fmt.Errorf("reschedule heartbeat: id %s not found", id)
	}
	return nil
}

// DeleteHeartbeat removes a channel identity's heartbeat schedule.
func (ScheduledTriggersRepository) DeleteHeartbeat(
	ctx context.Context,
	db Querier,
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

// SyncHeartbeatConfig updates an existing trigger's interval/model for the given channel binding.
// Missing rows are ignored because the scheduler will create them after the next successful run.
func (ScheduledTriggersRepository) SyncHeartbeatConfig(
	ctx context.Context,
	db Querier,
	channelID uuid.UUID,
	channelIdentityID uuid.UUID,
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
		UPDATE scheduled_triggers
		   SET interval_min = $1,
		       model = $2,
		       next_fire_at = CASE
		           WHEN interval_min <> $1 THEN $3
		           ELSE next_fire_at
		       END,
		       updated_at = now()
		 WHERE channel_id = $4
		   AND channel_identity_id = $5`,
		intervalMin,
		model,
		nextFire,
		channelID,
		channelIdentityID,
	)
	return err
}

// GetEarliestDue returns the earliest scheduled next_fire_at.
func (ScheduledTriggersRepository) GetEarliestDue(
	ctx context.Context,
	pool *pgxpool.Pool,
) (*time.Time, error) {
	var next time.Time
	err := pool.QueryRow(ctx, `
		SELECT next_fire_at
		  FROM scheduled_triggers
		 ORDER BY next_fire_at ASC
		 LIMIT 1`,
	).Scan(&next)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &next, nil
}

// ClaimDueTriggers fetches up to limit rows whose next_fire_at is due,
// advances next_fire_at based on the original schedule, and returns the claimed rows.
func (ScheduledTriggersRepository) ClaimDueTriggers(
	ctx context.Context,
	pool *pgxpool.Pool,
	limit int,
) ([]ScheduledTriggerRow, error) {
	if limit <= 0 {
		limit = 8
	}
	rows, err := pool.Query(ctx, `
        WITH due AS (
             SELECT id,
                    channel_id,
                    channel_identity_id,
                    persona_key,
                    account_id,
                    model,
                    interval_min,
                    next_fire_at,
                    GREATEST(interval_min, 1) AS effective_interval
               FROM scheduled_triggers
              WHERE next_fire_at <= now()
              ORDER BY next_fire_at ASC
              LIMIT $1
              FOR UPDATE SKIP LOCKED
        ),
        due_with_steps AS (
             SELECT *,
                    1 + CAST(FLOOR(EXTRACT(EPOCH FROM now() - next_fire_at) / (effective_interval * 60.0)) AS INT) AS intervals
               FROM due
        )
        UPDATE scheduled_triggers
           SET next_fire_at = due_with_steps.next_fire_at + (due_with_steps.effective_interval * due_with_steps.intervals) * interval '1 minute',
               updated_at   = now()
          FROM due_with_steps
         WHERE scheduled_triggers.id = due_with_steps.id
        RETURNING scheduled_triggers.id,
                  scheduled_triggers.channel_id,
                  scheduled_triggers.channel_identity_id,
                  scheduled_triggers.persona_key,
                  scheduled_triggers.account_id,
                  scheduled_triggers.model,
                  scheduled_triggers.interval_min,
                  scheduled_triggers.next_fire_at,
                  scheduled_triggers.trigger_kind,
                  scheduled_triggers.job_id`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ScheduledTriggerRow
	for rows.Next() {
		var r ScheduledTriggerRow
		if err := rows.Scan(&r.ID, &r.ChannelID, &r.ChannelIdentityID, &r.PersonaKey, &r.AccountID, &r.Model, &r.IntervalMin, &r.NextFireAt, &r.TriggerKind, &r.JobID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpdateTriggerNextFire updates next_fire_at for any trigger by id.
func (ScheduledTriggersRepository) UpdateTriggerNextFire(
	ctx context.Context,
	db Querier,
	id uuid.UUID,
	nextFireAt time.Time,
) error {
	if id == uuid.Nil {
		return errors.New("id must not be empty")
	}
	if nextFireAt.IsZero() {
		return errors.New("next_fire_at must not be zero")
	}
	_, err := db.Exec(ctx,
		`UPDATE scheduled_triggers SET next_fire_at = $1, updated_at = now() WHERE id = $2`,
		nextFireAt, id,
	)
	return err
}

// DeleteTriggerByJobID removes the trigger associated with a job.
func (ScheduledTriggersRepository) DeleteTriggerByJobID(
	ctx context.Context,
	db Querier,
	jobID uuid.UUID,
) error {
	if jobID == uuid.Nil {
		return errors.New("job_id must not be empty")
	}
	_, err := db.Exec(ctx, `DELETE FROM scheduled_triggers WHERE job_id = $1`, jobID)
	return err
}

// PostponeTrigger delays the next fire time by duration (used on error).
func (ScheduledTriggersRepository) PostponeTrigger(
	ctx context.Context,
	db Querier,
	id uuid.UUID,
	delay time.Duration,
) error {
	next := time.Now().UTC().Add(delay)
	_, err := db.Exec(ctx,
		`UPDATE scheduled_triggers SET next_fire_at = $1 WHERE id = $2 AND next_fire_at <= $1`,
		next, id,
	)
	return err
}

// GetThreadByChannelIdentity looks up the thread_id for a channel_identity from channel_dm_threads.
func (ScheduledTriggersRepository) GetThreadByChannelIdentity(
	ctx context.Context,
	db Querier,
	channelID uuid.UUID,
	channelIdentityID uuid.UUID,
) (*Thread, error) {
	var t Thread
	err := db.QueryRow(ctx,
		`SELECT t.id, t.account_id, t.created_by_user_id, t.deleted_at
		   FROM threads t
		   JOIN channel_dm_threads cdt ON cdt.thread_id = t.id
		  WHERE cdt.channel_id = $1
		    AND cdt.channel_identity_id = $2
		  LIMIT 1`,
		channelID,
		channelIdentityID,
	).Scan(&t.ID, &t.AccountID, &t.CreatedByUserID, &t.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

// ResolveHeartbeatThread resolves the target thread and delivery context for a heartbeat trigger.
func (ScheduledTriggersRepository) ResolveHeartbeatThread(
	ctx context.Context,
	db Querier,
	row ScheduledTriggerRow,
) (*HeartbeatThreadContext, error) {
	var platformSubjectID, channelType string
	if err := db.QueryRow(ctx,
		`SELECT platform_subject_id, channel_type
		   FROM channel_identities
		  WHERE id = $1`,
		row.ChannelIdentityID,
	).Scan(&platformSubjectID, &channelType); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	var personaID uuid.UUID
	err := db.QueryRow(ctx,
		`SELECT id
		   FROM personas
		  WHERE account_id = $1
		    AND persona_key = $2
		    AND deleted_at IS NULL
		  ORDER BY created_at DESC
		  LIMIT 1`,
		row.AccountID,
		row.PersonaKey,
	).Scan(&personaID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	var (
		threadID        uuid.UUID
		accountID       uuid.UUID
		createdByUserID *uuid.UUID
		deletedAt       *time.Time
	)
	if personaID != uuid.Nil {
		err = db.QueryRow(ctx,
			`SELECT t.id, t.account_id, t.created_by_user_id, t.deleted_at
			   FROM threads t
			   JOIN channel_group_threads cgt ON cgt.thread_id = t.id
			  WHERE cgt.channel_id = $1
			    AND cgt.platform_chat_id = $2
			    AND cgt.persona_id = $3
			    AND t.account_id = $4
			    AND t.deleted_at IS NULL
			  ORDER BY cgt.created_at DESC
			  LIMIT 1`,
			row.ChannelID,
			platformSubjectID,
			personaID,
			row.AccountID,
		).Scan(&threadID, &accountID, &createdByUserID, &deletedAt)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if err == nil {
			return &HeartbeatThreadContext{
				ThreadID:         threadID,
				AccountID:        accountID,
				CreatedByUserID:  createdByUserID,
				ChannelID:        row.ChannelID,
				ChannelType:      channelType,
				PlatformChatID:   platformSubjectID,
				IdentityID:       row.ChannelIdentityID,
				ConversationType: resolveGroupConversationType(channelType),
			}, nil
		}
	}

	err = db.QueryRow(ctx,
		`SELECT t.id, t.account_id, t.created_by_user_id, t.deleted_at
		   FROM threads t
		   JOIN channel_dm_threads cdt ON cdt.thread_id = t.id
		  WHERE cdt.channel_id = $1
		    AND cdt.channel_identity_id = $2
		    AND t.account_id = $3
		    AND t.deleted_at IS NULL
		  LIMIT 1`,
		row.ChannelID,
		row.ChannelIdentityID,
		row.AccountID,
	).Scan(&threadID, &accountID, &createdByUserID, &deletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	platformConversationID := strings.TrimSpace(platformSubjectID)
	if err := db.QueryRow(ctx,
		`SELECT platform_conversation_id
		   FROM channel_message_ledger
		  WHERE channel_id = $1
		    AND sender_channel_identity_id = $2
		    AND created_at >= now() - interval '24 hours'
		    AND platform_conversation_id <> ''
		  ORDER BY created_at DESC
		  LIMIT 1`,
		row.ChannelID,
		row.ChannelIdentityID,
	).Scan(&platformConversationID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	return &HeartbeatThreadContext{
		ThreadID:         threadID,
		AccountID:        accountID,
		CreatedByUserID:  createdByUserID,
		ChannelID:        row.ChannelID,
		ChannelType:      channelType,
		PlatformChatID:   strings.TrimSpace(platformConversationID),
		IdentityID:       row.ChannelIdentityID,
		ConversationType: "private",
	}, nil
}

func (ScheduledTriggersRepository) DeleteInactiveHeartbeats(
	ctx context.Context,
	db Querier,
	activityWindow time.Duration,
) (int64, error) {
	if activityWindow <= 0 {
		activityWindow = heartbeatActivityWindow
	}
	tag, err := db.Exec(ctx, `
		DELETE FROM scheduled_triggers st
		WHERE st.trigger_kind = 'heartbeat'
		  AND NOT EXISTS (
			SELECT 1
			  FROM channel_message_ledger cml
			 WHERE cml.channel_id = st.channel_id
			   AND cml.sender_channel_identity_id = st.channel_identity_id
			   AND cml.created_at >= $1
		)
		  AND NOT EXISTS (
			SELECT 1
			  FROM channel_identities ci
			  JOIN channel_message_ledger cml
			    ON cml.channel_id = st.channel_id
			   AND cml.platform_conversation_id = ci.platform_subject_id
			   AND cml.created_at >= $1
			 WHERE ci.id = st.channel_identity_id
		)`,
		time.Now().UTC().Add(-activityWindow),
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func resolveGroupConversationType(channelType string) string {
	switch strings.TrimSpace(channelType) {
	case "qq":
		return "group"
	default:
		return "supergroup"
	}
}
