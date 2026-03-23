package data

import (
	"context"
	"errors"
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
	NextFireAt       time.Time
}

// ScheduledTriggersRepository provides heartbeat scheduling operations.
type ScheduledTriggersRepository struct{}

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
	if intervalMin <= 0 {
		intervalMin = runkind.DefaultHeartbeatIntervalMinutes
	}
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
		        next_fire_at  = excluded.next_fire_at,
		        updated_at    = now()`,
		channelIdentityID, personaKey, accountID, model, intervalMin, nextFire,
	)
	return err
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

// ClaimDueHeartbeats fetches up to limit rows whose next_fire_at is due,
// advances next_fire_at by interval_min, and returns the claimed rows.
func (ScheduledTriggersRepository) ClaimDueHeartbeats(
	ctx context.Context,
	pool *pgxpool.Pool,
	limit int,
) ([]ScheduledTriggerRow, error) {
	if limit <= 0 {
		limit = 8
	}
	rows, err := pool.Query(ctx, `
		UPDATE scheduled_triggers
		   SET next_fire_at = now() + (interval_min * interval '1 minute'),
		       updated_at   = now()
		 WHERE id IN (
		     SELECT id FROM scheduled_triggers
		      WHERE next_fire_at <= now()
		      ORDER BY next_fire_at ASC
		      LIMIT $1
		      FOR UPDATE SKIP LOCKED
		 )
		RETURNING id, channel_identity_id, persona_key, account_id, model, interval_min, next_fire_at`,
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
