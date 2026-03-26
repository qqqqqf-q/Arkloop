package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"arkloop/services/shared/runkind"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ScheduledTriggerRow represents one row from scheduled_triggers.
type ScheduledTriggerRow struct {
	ID                uuid.UUID
	ChannelIdentityID uuid.UUID
	PersonaKey        string
	AccountID         uuid.UUID
	Model             string
	IntervalMin       int
	NextFireAt        time.Time
}

// ScheduledTriggersRepository provides heartbeat scheduling operations.
type ScheduledTriggersRepository struct{}

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
	channelIdentityID uuid.UUID,
	personaKey string,
	model string,
	intervalMin int,
) error {
	if channelIdentityID == uuid.Nil {
		return errors.New("channel_identity_id must not be empty")
	}
	intervalMin = normalizeHeartbeatInterval(intervalMin)
	nextFire := time.Now().UTC().Add(time.Duration(intervalMin) * time.Minute)
	_, err := db.Exec(ctx, `
		INSERT INTO scheduled_triggers
		    (id, channel_identity_id, persona_key, account_id, model, interval_min, next_fire_at)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6)
		ON CONFLICT (channel_identity_id) DO UPDATE
		    SET persona_key   = excluded.persona_key,
		        account_id    = excluded.account_id,
		        model         = excluded.model,
		        interval_min  = excluded.interval_min,
		        updated_at    = now()`,
		channelIdentityID, personaKey, accountID, model, intervalMin, nextFire,
	)
	return err
}

// GetHeartbeat returns the existing trigger for a channel identity.
func (ScheduledTriggersRepository) GetHeartbeat(
	ctx context.Context,
	db Querier,
	channelIdentityID uuid.UUID,
) (*ScheduledTriggerRow, error) {
	if channelIdentityID == uuid.Nil {
		return nil, errors.New("channel_identity_id must not be empty")
	}

	var row ScheduledTriggerRow
	err := db.QueryRow(ctx, `
		SELECT id, channel_identity_id, persona_key, account_id, model, interval_min, next_fire_at
		  FROM scheduled_triggers
		 WHERE channel_identity_id = $1`,
		channelIdentityID,
	).Scan(
		&row.ID,
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
	channelIdentityID uuid.UUID,
	intervalMin int,
) (time.Time, error) {
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
		 WHERE channel_identity_id = $3`,
		intervalMin, nextFire, channelIdentityID,
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
	channelIdentityID uuid.UUID,
) error {
	_, err := db.Exec(ctx,
		`DELETE FROM scheduled_triggers WHERE channel_identity_id = $1`,
		channelIdentityID,
	)
	return err
}

// GetEarliestHeartbeatDue returns the earliest scheduled next_fire_at.
func (ScheduledTriggersRepository) GetEarliestHeartbeatDue(
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

// ClaimDueHeartbeats fetches up to limit rows whose next_fire_at is due,
// advances next_fire_at based on the original schedule, and returns the claimed rows.
func (ScheduledTriggersRepository) ClaimDueHeartbeats(
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
                  scheduled_triggers.channel_identity_id,
                  scheduled_triggers.persona_key,
                  scheduled_triggers.account_id,
                  scheduled_triggers.model,
                  scheduled_triggers.interval_min,
                  scheduled_triggers.next_fire_at`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ScheduledTriggerRow
	for rows.Next() {
		var r ScheduledTriggerRow
		if err := rows.Scan(&r.ID, &r.ChannelIdentityID, &r.PersonaKey, &r.AccountID, &r.Model, &r.IntervalMin, &r.NextFireAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PostponeHeartbeat delays the next fire time by duration (used on error).
func (ScheduledTriggersRepository) PostponeHeartbeat(
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
	channelIdentityID uuid.UUID,
) (*Thread, error) {
	var t Thread
	err := db.QueryRow(ctx,
		`SELECT t.id, t.account_id, t.created_by_user_id, t.deleted_at
		   FROM threads t
		   JOIN channel_dm_threads cdt ON cdt.thread_id = t.id
		  WHERE cdt.channel_identity_id = $1
		  LIMIT 1`,
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

// GetThreadByHeartbeatTrigger 统一按 heartbeat identity + persona_key 定位目标 thread。
func (ScheduledTriggersRepository) GetThreadByHeartbeatTrigger(
	ctx context.Context,
	db Querier,
	row ScheduledTriggerRow,
) (*Thread, error) {
	var t Thread
	err := db.QueryRow(ctx,
		`WITH heartbeat_identity AS (
		     SELECT platform_subject_id
		       FROM channel_identities
		      WHERE id = $1
		 ),
		 target_persona AS (
		     SELECT id
		       FROM personas
		      WHERE account_id = $2
		        AND key = $3
		        AND deleted_at IS NULL
		      ORDER BY created_at DESC
		      LIMIT 1
		 )
		 SELECT id, account_id, created_by_user_id, deleted_at
		   FROM (
		         SELECT t.id, t.account_id, t.created_by_user_id, t.deleted_at, 0 AS ord
		           FROM threads t
		           JOIN channel_group_threads cgt ON cgt.thread_id = t.id
		           JOIN heartbeat_identity hi ON hi.platform_subject_id = cgt.platform_chat_id
		           JOIN target_persona tp ON tp.id = cgt.persona_id
		          WHERE t.account_id = $2
		            AND t.deleted_at IS NULL
		         UNION ALL
		         SELECT t.id, t.account_id, t.created_by_user_id, t.deleted_at, 1 AS ord
		           FROM threads t
		           JOIN channel_dm_threads cdt ON cdt.thread_id = t.id
		          WHERE cdt.channel_identity_id = $1
		            AND t.account_id = $2
		            AND t.deleted_at IS NULL
		        ) candidates
		  ORDER BY ord ASC
		  LIMIT 1`,
		row.ChannelIdentityID,
		row.AccountID,
		row.PersonaKey,
	).Scan(&t.ID, &t.AccountID, &t.CreatedByUserID, &t.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}
