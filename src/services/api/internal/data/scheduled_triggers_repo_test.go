package data

import (
	"context"
	"errors"
	"testing"
	"time"

	"arkloop/services/api/internal/migrate"
	"arkloop/services/api/internal/testutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func setupScheduledTriggersRepo(t *testing.T) (*ScheduledTriggersRepository, *pgxpool.Pool, context.Context) {
	t.Helper()

	db := testutil.SetupPostgresDatabase(t, "api_go_scheduled_triggers")
	ctx := context.Background()
	if _, err := migrate.Up(ctx, db.DSN); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	pool, err := NewPool(ctx, db.DSN, PoolLimits{MaxConns: 4, MinConns: 0})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
	})

	return &ScheduledTriggersRepository{}, pool, ctx
}

func TestScheduledTriggersRepositoryUpsertHeartbeatKeepsNextFireAt(t *testing.T) {
	repo, pool, ctx := setupScheduledTriggersRepo(t)
	channelID := uuid.New()
	identity := uuid.New()
	account := uuid.New()

	if err := repo.UpsertHeartbeat(ctx, pool, account, channelID, identity, "persona", "model", 1); err != nil {
		t.Fatalf("upsert heartbeat: %v", err)
	}
	first := readNextFireAt(t, ctx, pool, channelID, identity)

	if err := repo.UpsertHeartbeat(ctx, pool, account, channelID, identity, "persona", "model", 1); err != nil {
		t.Fatalf("second upsert heartbeat: %v", err)
	}
	second := readNextFireAt(t, ctx, pool, channelID, identity)

	if !second.Equal(first) {
		t.Fatalf("expected next_fire_at to stay fixed, first=%s second=%s", first, second)
	}
}

func TestScheduledTriggersRepositoryResetHeartbeatNextFire(t *testing.T) {
	repo, pool, ctx := setupScheduledTriggersRepo(t)
	channelID := uuid.New()
	identity := uuid.New()
	account := uuid.New()

	if err := repo.UpsertHeartbeat(ctx, pool, account, channelID, identity, "persona", "model", 5); err != nil {
		t.Fatalf("upsert heartbeat: %v", err)
	}

	nowBefore := time.Now().UTC()
	reset, err := repo.ResetHeartbeatNextFire(ctx, pool, channelID, identity, 1)
	if err != nil {
		t.Fatalf("reset heartbeat next fire: %v", err)
	}

	if !reset.Equal(readNextFireAt(t, ctx, pool, channelID, identity)) {
		t.Fatalf("reset result mismatch stored value, got=%s", reset)
	}

	intervalDuration := time.Duration(1) * time.Minute
	if reset.Sub(nowBefore) < intervalDuration {
		t.Fatalf("expected reset to schedule at least one interval ahead, delta=%s", reset.Sub(nowBefore))
	}
	if reset.Sub(nowBefore) > intervalDuration+3*time.Second {
		t.Fatalf("reset scheduled too far ahead, delta=%s", reset.Sub(nowBefore))
	}
}

func TestScheduledTriggersRepositoryRescheduleHeartbeatNextFireAt(t *testing.T) {
	repo, pool, ctx := setupScheduledTriggersRepo(t)
	channelID := uuid.New()
	identity := uuid.New()
	account := uuid.New()

	if err := repo.UpsertHeartbeat(ctx, pool, account, channelID, identity, "persona", "model", 2); err != nil {
		t.Fatalf("upsert heartbeat: %v", err)
	}
	id := readTriggerID(t, ctx, pool, channelID, identity)
	target := time.Now().UTC().Add(5 * time.Minute)

	if err := repo.RescheduleHeartbeatNextFireAt(ctx, pool, id, target); err != nil {
		t.Fatalf("reschedule heartbeat next fire: %v", err)
	}

	got := readNextFireAt(t, ctx, pool, channelID, identity)
	if d := got.Sub(target); d < -time.Millisecond || d > time.Millisecond {
		t.Fatalf("unexpected next_fire_at after reschedule, got=%s want=%s", got, target)
	}
}

func TestScheduledTriggersRepositoryClaimDueTriggersAdvancesFromOriginalSchedule(t *testing.T) {
	repo, pool, ctx := setupScheduledTriggersRepo(t)
	triggerID := uuid.New()
	channelID := uuid.New()
	account := uuid.New()
	identity := uuid.New()
	now := time.Now().UTC()
	originalNextFire := now.Add(-20 * time.Second)
	intervalMin := 1

	insertScheduledTrigger(t, ctx, pool, triggerID, channelID, identity, "persona", account, "model", intervalMin, originalNextFire)

	callNow := time.Now().UTC()
	rows, err := repo.ClaimDueTriggers(ctx, pool, 1)
	if err != nil {
		t.Fatalf("claim due heartbeats: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one claimed row, got %d", len(rows))
	}

	updated := rows[0].NextFireAt
	if !updated.After(callNow) {
		t.Fatalf("expected updated next_fire_at > now, got=%s now=%s", updated, callNow)
	}

	intervalDuration := time.Duration(normalizeHeartbeatInterval(intervalMin)) * time.Minute
	expectedNextFire := originalNextFire.Add(intervalDuration)
	if d := updated.Sub(expectedNextFire); d < -time.Second || d > time.Second {
		t.Fatalf("expected next_fire_at to advance by exactly one interval from original, got=%s want=%s", updated, expectedNextFire)
	}
}

func TestScheduledTriggersRepositoryGetEarliestDue(t *testing.T) {
	repo, pool, ctx := setupScheduledTriggersRepo(t)
	channelA := uuid.New()
	channelB := uuid.New()
	identityA := uuid.New()
	identityB := uuid.New()
	account := uuid.New()
	now := time.Now().UTC()
	earliest := now.Add(30 * time.Second)
	later := now.Add(2 * time.Minute)

	insertScheduledTrigger(t, ctx, pool, uuid.New(), channelA, identityA, "persona-a", account, "model", 1, earliest)
	insertScheduledTrigger(t, ctx, pool, uuid.New(), channelB, identityB, "persona-b", account, "model", 1, later)

	got, err := repo.GetEarliestDue(ctx, pool)
	if err != nil {
		t.Fatalf("get earliest heartbeat due: %v", err)
	}
	if got == nil {
		t.Fatal("expected earliest time")
	}
	if !got.Equal(earliest) {
		t.Fatalf("unexpected earliest time, got=%s want=%s", got, earliest)
	}
}

func insertScheduledTrigger(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, channelID uuid.UUID, identity uuid.UUID, persona string, account uuid.UUID, model string, interval int, nextFire time.Time) {
	t.Helper()

	if _, err := pool.Exec(ctx, `
		INSERT INTO scheduled_triggers
		    (id, channel_id, channel_identity_id, persona_key, account_id, model, interval_min, next_fire_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now(), now())`,
		id, channelID, identity, persona, account, model, interval, nextFire,
	); err != nil {
		t.Fatalf("insert scheduled trigger: %v", err)
	}
}

func readNextFireAt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, channelID uuid.UUID, identity uuid.UUID) time.Time {
	t.Helper()

	var next time.Time
	if err := pool.QueryRow(ctx,
		`SELECT next_fire_at FROM scheduled_triggers WHERE channel_id = $1 AND channel_identity_id = $2`,
		channelID,
		identity,
	).Scan(&next); err != nil {
		t.Fatalf("read next_fire_at: %v", err)
	}
	return next
}

func readTriggerID(t *testing.T, ctx context.Context, pool *pgxpool.Pool, channelID uuid.UUID, identity uuid.UUID) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM scheduled_triggers WHERE channel_id = $1 AND channel_identity_id = $2`,
		channelID,
		identity,
	).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("trigger not found: %v", err)
		}
		t.Fatalf("read trigger id: %v", err)
	}
	return id
}

func TestScheduledTriggersRepositoryDeleteInactiveHeartbeats(t *testing.T) {
	repo, pool, ctx := setupScheduledTriggersRepo(t)
	channelID := uuid.New()
	identity := uuid.New()
	account := uuid.New()
	triggerID := uuid.New()

	insertScheduledTrigger(t, ctx, pool, triggerID, channelID, identity, "persona", account, "model", 1, time.Now().UTC())

	count, err := repo.DeleteInactiveHeartbeats(ctx, pool, time.Hour)
	if err != nil {
		t.Fatalf("DeleteInactiveHeartbeats: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 deleted trigger, got %d", count)
	}
}

func TestScheduledTriggersRepositoryDeleteInactiveHeartbeatsKeepsActiveGroupIdentity(t *testing.T) {
	repo, pool, ctx := setupScheduledTriggersRepo(t)
	channelID := uuid.New()
	groupIdentityID := uuid.New()
	userIdentityID := uuid.New()
	accountID := uuid.New()
	triggerID := uuid.New()

	if _, err := pool.Exec(ctx, `
		INSERT INTO channel_identities (id, channel_type, platform_subject_id, display_name, metadata)
		VALUES ($1, 'telegram', 'group-123', 'group', '{}'::jsonb),
		       ($2, 'telegram', 'user-123', 'user', '{}'::jsonb);
		INSERT INTO channels (id, account_id, channel_type, persona_id, owner_user_id, webhook_secret, webhook_url, is_active, config_json)
		VALUES ($3, $4, 'telegram', NULL, NULL, 'whsec', 'https://example.com', true, '{}'::jsonb);
		INSERT INTO channel_message_ledger (
			channel_id, channel_type, direction, platform_conversation_id, platform_message_id, sender_channel_identity_id, metadata_json, created_at
		) VALUES (
			$3, 'telegram', 'inbound', 'group-123', 'msg-1', $2, '{}'::jsonb, now()
		)`,
		groupIdentityID,
		userIdentityID,
		channelID,
		accountID,
	); err != nil {
		t.Fatalf("seed identities/ledger: %v", err)
	}

	insertScheduledTrigger(t, ctx, pool, triggerID, channelID, groupIdentityID, "persona", accountID, "model", 1, time.Now().UTC())

	count, err := repo.DeleteInactiveHeartbeats(ctx, pool, time.Hour)
	if err != nil {
		t.Fatalf("DeleteInactiveHeartbeats: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected trigger to remain, deleted=%d", count)
	}
}
