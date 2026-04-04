package pipeline

import (
    "context"
    "os"
    "strings"
    "testing"
    "time"

    "arkloop/services/worker/internal/data"
    "arkloop/services/worker/internal/memory"
    "arkloop/services/worker/internal/memory/openviking"

    "github.com/google/uuid"
    "github.com/jackc/pgx/v5/pgxpool"
)

func TestSnapshotRefreshWithRealOpenViking(t *testing.T) {
    cfg := openviking.LoadConfigFromEnv()
    if !cfg.Enabled() {
        t.Skip("ARKLOOP_OPENVIKING_BASE_URL not set")
    }
    provider := openviking.NewProvider(cfg)
    if provider == nil {
        t.Skip("openviking provider disabled")
    }

    dsn := strings.TrimSpace(os.Getenv("ARKLOOP_DATABASE_URL"))
    if dsn == "" {
        dsn = strings.TrimSpace(os.Getenv("DATABASE_URL"))
    }
    if dsn == "" {
        t.Skip("ARKLOOP_DATABASE_URL not set")
    }

    pool, err := pgxpool.New(context.Background(), dsn)
    if err != nil {
        t.Fatalf("pgxpool.New: %v", err)
    }
    t.Cleanup(pool.Close)

    prevWindow := snapshotRefreshWindow
    prevInterval := snapshotRefreshRetryInterval
    prevAttempts := snapshotRefreshMaxAttempts
    snapshotRefreshWindow = 90 * time.Second
    snapshotRefreshRetryInterval = 2 * time.Second
    snapshotRefreshMaxAttempts = 45
    t.Cleanup(func() {
        snapshotRefreshWindow = prevWindow
        snapshotRefreshRetryInterval = prevInterval
        snapshotRefreshMaxAttempts = prevAttempts
    })

    ident := memory.MemoryIdentity{
        AccountID: uuid.New(),
        UserID:    uuid.New(),
        AgentID:   "snapshot-refresh-real-openviking",
    }
    query := "snapshot refresh probe " + uuid.NewString()

    if err := provider.Write(context.Background(), ident, memory.MemoryScopeUser, memory.MemoryEntry{Content: query}); err != nil {
        t.Fatalf("provider.Write: %v", err)
    }

    snap := NewPgxMemorySnapshotStore(pool)
    scheduleSnapshotRefresh(provider, snap, nil, uuid.Nil, "trace-snapshot-real", ident, "", map[string][]string{
        string(memory.MemoryScopeUser): {query},
    }, "", "write")

    deadline := time.Now().Add(120 * time.Second)
    for time.Now().Before(deadline) {
        block, found, err := data.MemorySnapshotRepository{}.Get(context.Background(), pool, ident.AccountID, ident.UserID, ident.AgentID)
        if err != nil {
            t.Fatalf("load snapshot: %v", err)
        }
        if found && strings.Contains(block, query) {
            hits, hitsFound, err := data.MemorySnapshotRepository{}.GetHits(context.Background(), pool, ident.AccountID, ident.UserID, ident.AgentID)
            if err != nil {
                t.Fatalf("load hits: %v", err)
            }
            if !hitsFound || len(hits) == 0 {
                t.Fatal("expected cached hits after snapshot rebuild")
            }
            return
        }
        time.Sleep(2 * time.Second)
    }

    block, found, err := data.MemorySnapshotRepository{}.Get(context.Background(), pool, ident.AccountID, ident.UserID, ident.AgentID)
    if err != nil {
        t.Fatalf("final load snapshot: %v", err)
    }
    t.Fatalf("snapshot not rebuilt in time, found=%v block=%q", found, block)
}
