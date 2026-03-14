package acptoken

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestStore(t *testing.T) (*Store, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return NewStore(rdb), mr
}

func TestStoreRegisterAndGet(t *testing.T) {
	store, mr := newTestStore(t)
	defer mr.Close()
	ctx := context.Background()

	state := TokenState{
		RunID:     "run-123",
		AccountID: "acc-456",
		Models:    []string{"claude-sonnet-4-5"},
		Budget:    100000,
		CreatedAt: time.Now().Unix(),
	}

	if err := store.Register(ctx, "run-123", state, 5*time.Minute); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get(ctx, "run-123")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected non-nil state")
	}
	if got.RunID != "run-123" {
		t.Errorf("RunID = %q, want %q", got.RunID, "run-123")
	}
	if got.Budget != 100000 {
		t.Errorf("Budget = %d, want %d", got.Budget, 100000)
	}
}

func TestStoreRecordUsageWithinBudget(t *testing.T) {
	store, mr := newTestStore(t)
	defer mr.Close()
	ctx := context.Background()

	state := TokenState{
		RunID:  "run-1",
		Budget: 1000,
	}
	_ = store.Register(ctx, "run-1", state, 5*time.Minute)

	allowed, err := store.RecordUsage(ctx, "run-1", 500)
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Error("expected allowed=true, got false")
	}

	got, _ := store.Get(ctx, "run-1")
	if got.TokensUsed != 500 {
		t.Errorf("TokensUsed = %d, want 500", got.TokensUsed)
	}
}

func TestStoreRecordUsageExceedsBudget(t *testing.T) {
	store, mr := newTestStore(t)
	defer mr.Close()
	ctx := context.Background()

	state := TokenState{
		RunID:  "run-2",
		Budget: 100,
	}
	_ = store.Register(ctx, "run-2", state, 5*time.Minute)

	allowed, err := store.RecordUsage(ctx, "run-2", 200)
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Error("expected allowed=false, got true")
	}
}

func TestStoreUnlimitedBudget(t *testing.T) {
	store, mr := newTestStore(t)
	defer mr.Close()
	ctx := context.Background()

	state := TokenState{
		RunID:  "run-3",
		Budget: 0, // unlimited
	}
	_ = store.Register(ctx, "run-3", state, 5*time.Minute)

	allowed, err := store.RecordUsage(ctx, "run-3", 999999)
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Error("expected allowed=true for unlimited budget")
	}
}

func TestStoreRevoke(t *testing.T) {
	store, mr := newTestStore(t)
	defer mr.Close()
	ctx := context.Background()

	state := TokenState{RunID: "run-4"}
	_ = store.Register(ctx, "run-4", state, 5*time.Minute)

	if err := store.Revoke(ctx, "run-4"); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get(ctx, "run-4")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Error("expected nil after revoke")
	}
}

func TestStoreIsActive(t *testing.T) {
	store, mr := newTestStore(t)
	defer mr.Close()
	ctx := context.Background()

	active, err := store.IsActive(ctx, "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if active {
		t.Error("expected inactive for nonexistent run")
	}

	_ = store.Register(ctx, "run-5", TokenState{RunID: "run-5"}, 5*time.Minute)
	active, err = store.IsActive(ctx, "run-5")
	if err != nil {
		t.Fatal(err)
	}
	if !active {
		t.Error("expected active after register")
	}
}

func TestStoreNilRedis(t *testing.T) {
	store := NewStore(nil)
	ctx := context.Background()

	// All operations should be no-op with nil Redis
	if err := store.Register(ctx, "x", TokenState{}, time.Minute); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, "x")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Error("expected nil from nil Redis store")
	}
	allowed, err := store.RecordUsage(ctx, "x", 100)
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Error("expected allowed from nil Redis store")
	}
}
