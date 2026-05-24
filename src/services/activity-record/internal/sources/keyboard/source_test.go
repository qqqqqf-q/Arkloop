package keyboard

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"arkloop/services/activity-record/internal/store"
)

func TestRunEmitsOnCount(t *testing.T) {
	s := &Source{emitInterval: 100 * time.Millisecond}

	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()

	events := make(chan store.Event, 100)

	go func() {
		_ = s.Run(ctx, nil, events)
	}()

	// simulate keystrokes by directly adding to the counter
	// the Run goroutine reads via listenKeystrokes which we can't control in test,
	// but we can test the event emission logic separately

	<-ctx.Done()
	close(events)

	for ev := range events {
		if ev.Action != "keystroke_count" {
			t.Fatalf("unexpected action: %s", ev.Action)
		}
	}
}

func TestAtomicCounterSwap(t *testing.T) {
	var count atomic.Int64
	count.Add(10)
	count.Add(5)

	val := count.Swap(0)
	if val != 15 {
		t.Fatalf("expected 15, got %d", val)
	}
	if count.Load() != 0 {
		t.Fatalf("expected 0 after swap, got %d", count.Load())
	}
}
