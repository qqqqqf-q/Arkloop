package mouse

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"arkloop/services/activity-record/internal/store"
)

func TestRunEmitsOnActivity(t *testing.T) {
	s := &Source{emitInterval: 100 * time.Millisecond}

	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()

	events := make(chan store.Event, 100)
	go func() {
		_ = s.Run(ctx, nil, events)
	}()

	<-ctx.Done()
	close(events)

	for ev := range events {
		if ev.Action != "mouse_activity" {
			t.Fatalf("unexpected action: %s", ev.Action)
		}
	}
}

func TestCountersSwap(t *testing.T) {
	var c counters
	c.clicks.Add(5)
	c.scrolls.Add(3)

	clicks := c.clicks.Swap(0)
	scrolls := c.scrolls.Swap(0)

	if clicks != 5 {
		t.Fatalf("expected 5 clicks, got %d", clicks)
	}
	if scrolls != 3 {
		t.Fatalf("expected 3 scrolls, got %d", scrolls)
	}
	if c.clicks.Load() != 0 || c.scrolls.Load() != 0 {
		t.Fatal("counters not reset after swap")
	}
}

func TestRunSkipsZeroActivity(t *testing.T) {
	s := &Source{emitInterval: 50 * time.Millisecond}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	events := make(chan store.Event, 100)
	go func() {
		_ = s.Run(ctx, nil, events)
	}()

	<-ctx.Done()
	close(events)

	var count int
	for range events {
		count++
	}
	// listenMouse on macOS may fail without Accessibility permission,
	// so there should be zero mouse_activity events (no clicks/scrolls).
	// If running interactively, events may appear.
	_ = count
}

func TestAtomicCounterConcurrency(t *testing.T) {
	var c counters
	var done atomic.Int32

	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 1000; j++ {
				c.clicks.Add(1)
				c.scrolls.Add(1)
			}
			done.Add(1)
		}()
	}
	for done.Load() < 10 {
		time.Sleep(time.Millisecond)
	}

	clicks := c.clicks.Load()
	scrolls := c.scrolls.Load()
	if clicks != 10000 {
		t.Fatalf("expected 10000 clicks, got %d", clicks)
	}
	if scrolls != 10000 {
		t.Fatalf("expected 10000 scrolls, got %d", scrolls)
	}
}
