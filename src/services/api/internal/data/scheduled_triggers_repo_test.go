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
	identity := uuid.New()
	account := uuid.New()

	if err := repo.UpsertHeartbeat(ctx, pool, account, identity, "persona", "model", 1); err != nil {
		t.Fatalf("upsert heartbeat: %v", err)
	}
	first := readNextFireAt(t, ctx, pool, identity)

	if err := repo.UpsertHeartbeat(ctx, pool, account, identity, "persona", "model", 1); err != nil {
		t.Fatalf("second upsert heartbeat: %v", err)
	}
	second := readNextFireAt(t, ctx, pool, identity)

	if !second.Equal(first) {
		t.Fatalf("expected next_fire_at to stay fixed, first=%s second=%s", first, second)
	}
}

func TestScheduledTriggersRepositoryResetHeartbeatNextFire(t *testing.T) {
	repo, pool, ctx := setupScheduledTriggersRepo(t)
	identity := uuid.New()
	account := uuid.New()

	if err := repo.UpsertHeartbeat(ctx, pool, account, identity, "persona", "model", 5); err != nil {
		t.Fatalf("upsert heartbeat: %v", err)
	}

	nowBefore := time.Now().UTC()
	reset, err := repo.ResetHeartbeatNextFire(ctx, pool, identity, 1)
	if err != nil {
		t.Fatalf("reset heartbeat next fire: %v", err)
	}

	if !reset.Equal(readNextFireAt(t, ctx, pool, identity)) {
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
	identity := uuid.New()
	account := uuid.New()

	if err := repo.UpsertHeartbeat(ctx, pool, account, identity, "persona", "model", 2); err != nil {
		t.Fatalf("upsert heartbeat: %v", err)
	}
	id := readTriggerID(t, ctx, pool, identity)
	target := time.Now().UTC().Add(5 * time.Minute)

	if err := repo.RescheduleHeartbeatNextFireAt(ctx, pool, id, target); err != nil {
		t.Fatalf("reschedule heartbeat next fire: %v", err)
	}

	got := readNextFireAt(t, ctx, pool, identity)
	if d := got.Sub(target); d < -time.Millisecond || d > time.Millisecond {
		t.Fatalf("unexpected next_fire_at after reschedule, got=%s want=%s", got, target)
	}
}

func TestScheduledTriggersRepositoryClaimDueHeartbeatsAdvancesFromOriginalSchedule(t *testing.T) {
	repo, pool, ctx := setupScheduledTriggersRepo(t)
	triggerID := uuid.New()
	account := uuid.New()
	identity := uuid.New()
	now := time.Now().UTC()
	originalNextFire := now.Add(-20 * time.Second)
	intervalMin := 1

	insertScheduledTrigger(t, ctx, pool, triggerID, identity, "persona", account, "model", intervalMin, originalNextFire)

	callNow := time.Now().UTC()
	rows, err := repo.ClaimDueHeartbeats(ctx, pool, 1)
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
	if diff := updated.Sub(originalNextFire); diff%intervalDuration != 0 {
		t.Fatalf("expected update to advance by whole intervals, diff=%s interval=%s", diff, intervalDuration)
	}
}

func TestScheduledTriggersRepositoryGetEarliestHeartbeatDue(t *testing.T) {
	repo, pool, ctx := setupScheduledTriggersRepo(t)
	identityA := uuid.New()
	identityB := uuid.New()
	account := uuid.New()
	now := time.Now().UTC()
	earliest := now.Add(30 * time.Second)
	later := now.Add(2 * time.Minute)

	insertScheduledTrigger(t, ctx, pool, uuid.New(), identityA, "persona-a", account, "model", 1, earliest)
	insertScheduledTrigger(t, ctx, pool, uuid.New(), identityB, "persona-b", account, "model", 1, later)

	got, err := repo.GetEarliestHeartbeatDue(ctx, pool)
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

func insertScheduledTrigger(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, identity uuid.UUID, persona string, account uuid.UUID, model string, interval int, nextFire time.Time) {
	t.Helper()

	if _, err := pool.Exec(ctx, `
		INSERT INTO scheduled_triggers
		    (id, channel_identity_id, persona_key, account_id, model, interval_min, next_fire_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now(), now())`,
		id, identity, persona, account, model, interval, nextFire,
	); err != nil {
		t.Fatalf("insert scheduled trigger: %v", err)
	}
}

func readNextFireAt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, identity uuid.UUID) time.Time {
	t.Helper()

	var next time.Time
	if err := pool.QueryRow(ctx,
		`SELECT next_fire_at FROM scheduled_triggers WHERE channel_identity_id = $1`,
		identity,
	).Scan(&next); err != nil {
		t.Fatalf("read next_fire_at: %v", err)
	}
	return next
}

func readTriggerID(t *testing.T, ctx context.Context, pool *pgxpool.Pool, identity uuid.UUID) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM scheduled_triggers WHERE channel_identity_id = $1`,
		identity,
	).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("trigger not found: %v", err)
		}
		t.Fatalf("read trigger id: %v", err)
	}
	return id
}
