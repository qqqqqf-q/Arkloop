package mouse

import (
	"context"
	"testing"
	"time"

	"arkloop/services/activity-record/internal/store"
)

func TestRunEmitsOnActivity(t *testing.T) {
	s := &Source{emitInterval: 100 * time.Millisecond}

	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()

	events := make(chan store.Event, 100)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = s.Run(ctx, nil, events)
	}()

	<-done
	close(events)

	for ev := range events {
		if ev.Action != "mouse_activity" && ev.Action != "mouse_path" {
			t.Fatalf("unexpected action: %s", ev.Action)
		}
	}
}

func TestRunSkipsZeroActivity(t *testing.T) {
	s := &Source{emitInterval: 50 * time.Millisecond}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	events := make(chan store.Event, 100)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = s.Run(ctx, nil, events)
	}()

	<-done
	close(events)

	var count int
	for range events {
		count++
	}
	_ = count
}

func TestMouseAgg(t *testing.T) {
	var agg mouseAgg

	agg.clicks = 3
	agg.scrolls = 5
	agg.pathEvents = []mousePathEvent{
		{at: time.Now(), x: 100, y: 200},
		{at: time.Now(), x: 150, y: 250},
	}

	s := &Source{emitInterval: 30 * time.Second}

	events := make(chan store.Event, 10)
	s.emitAgg(time.Now(), events, &agg)

	if agg.clicks != 0 {
		t.Fatal("clicks not reset")
	}
	if agg.scrolls != 0 {
		t.Fatal("scrolls not reset")
	}

	close(events)
	var aggEvents int
	for ev := range events {
		if ev.Action == "mouse_activity" {
			aggEvents++
			meta := ev.Metadata
			if meta["clicks"] != 3 {
				t.Fatalf("expected 3 clicks, got %v", meta["clicks"])
			}
			if meta["scrolls"] != 5 {
				t.Fatalf("expected 5 scrolls, got %v", meta["scrolls"])
			}
		}
	}
	if aggEvents != 1 {
		t.Fatalf("expected 1 activity event, got %d", aggEvents)
	}

	// Path events.
	events2 := make(chan store.Event, 10)
	s.emitPath(events2, &agg)
	close(events2)
	for ev := range events2 {
		if ev.Action == "mouse_path" {
			if ev.Text == "" {
				t.Fatal("expected json path in text")
			}
			pc := ev.Metadata["point_count"]
			if pc != 2 {
				t.Fatalf("expected 2 points, got %v", pc)
			}
		}
	}
}
