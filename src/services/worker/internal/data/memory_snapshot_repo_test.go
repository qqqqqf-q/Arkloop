package data

import (
	"context"
	"strings"
	"sync"
	"testing"

	"arkloop/services/worker/internal/testutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAppendMemoryLineToBlock(t *testing.T) {
	block := appendMemoryLineToBlock("\n\n<memory>\n- first\n</memory>", "second")
	if !strings.Contains(block, "- first") || !strings.Contains(block, "- second") {
		t.Fatalf("unexpected block: %q", block)
	}
	if strings.Count(block, "</memory>") != 1 {
		t.Fatalf("unexpected closing tag count: %q", block)
	}
}

func TestMemorySnapshotRepository_AppendMemoryLine_Concurrent(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "worker_memory_snapshot")
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	repo := MemorySnapshotRepository{}
	accountID := uuid.New()
	userID := uuid.New()
	agentID := "test-agent"
	lines := []string{"alpha", "beta", "gamma", "delta"}

	var wg sync.WaitGroup
	for _, line := range lines {
		line := line
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := repo.AppendMemoryLine(ctx, pool, accountID, userID, agentID, line); err != nil {
				t.Errorf("append %q failed: %v", line, err)
			}
		}()
	}
	wg.Wait()

	block, found, err := repo.Get(ctx, pool, accountID, userID, agentID)
	if err != nil {
		t.Fatalf("get snapshot failed: %v", err)
	}
	if !found {
		t.Fatal("expected snapshot to exist")
	}
	for _, line := range lines {
		if !strings.Contains(block, "- "+line) {
			t.Fatalf("missing line %q in block: %q", line, block)
		}
	}
}
