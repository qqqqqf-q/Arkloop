//go:build desktop

package desktoprun

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/consumer"

	"github.com/google/uuid"
)

func TestDesktopRunLockerAllowsDifferentRuns(t *testing.T) {
	ctx := context.Background()
	rootRunA := uuid.New()
	rootRunB := uuid.New()

	locker := consumer.NewLocalRunLocker()

	unlockA, acquired, err := locker.TryAcquire(ctx, rootRunA)
	if err != nil {
		t.Fatalf("acquire root run A: %v", err)
	}
	if !acquired {
		t.Fatal("expected root run A to acquire")
	}

	if _, acquired, err := locker.TryAcquire(ctx, rootRunB); err != nil {
		t.Fatalf("acquire root run B: %v", err)
	} else if !acquired {
		t.Fatal("expected different runs to acquire concurrently")
	}

	if err := unlockA(ctx); err != nil {
		t.Fatalf("unlock root run A: %v", err)
	}
	if err := unlockA(ctx); err != nil {
		t.Fatalf("unlock root run A twice: %v", err)
	}
}

func TestDesktopRunLockerBlocksSameRunReentry(t *testing.T) {
	ctx := context.Background()
	runID := uuid.New()

	locker := consumer.NewLocalRunLocker()

	unlock, acquired, err := locker.TryAcquire(ctx, runID)
	if err != nil {
		t.Fatalf("acquire run: %v", err)
	}
	if !acquired {
		t.Fatal("expected run to acquire")
	}

	if _, acquired, err := locker.TryAcquire(ctx, runID); err != nil {
		t.Fatalf("reacquire same run: %v", err)
	} else if acquired {
		t.Fatal("expected same run to be blocked while already held")
	}

	if err := unlock(ctx); err != nil {
		t.Fatalf("unlock run: %v", err)
	}

	if _, acquired, err := locker.TryAcquire(ctx, runID); err != nil {
		t.Fatalf("acquire run after release: %v", err)
	} else if !acquired {
		t.Fatal("expected run to acquire after release")
	}
}
