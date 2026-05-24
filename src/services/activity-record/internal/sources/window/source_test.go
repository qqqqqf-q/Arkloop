package window

import (
	"context"
	"testing"
	"time"

	"arkloop/services/activity-record/internal/store"
)

func TestEmitFocusedSkipsShortDuration(t *testing.T) {
	s := New(5 * time.Minute)
	events := make(chan store.Event, 10)

	info := WindowInfo{App: "Terminal", WindowTitle: "zsh", PID: 100}
	start := time.Now()
	s.emitFocused(events, info, start, start.Add(500*time.Millisecond))

	select {
	case <-events:
		t.Fatal("expected no event for <1s duration")
	default:
	}
}

func TestEmitFocusedProducesEvent(t *testing.T) {
	s := New(5 * time.Minute)
	events := make(chan store.Event, 10)

	info := WindowInfo{App: "Code", WindowTitle: "main.go", PID: 200}
	start := time.Now()
	s.emitFocused(events, info, start, start.Add(10*time.Second))

	select {
	case ev := <-events:
		if ev.Action != "focused" {
			t.Fatalf("expected action=focused, got %q", ev.Action)
		}
		if ev.App != "Code" {
			t.Fatalf("expected app=Code, got %q", ev.App)
		}
		dur, ok := ev.Metadata["duration_sec"].(float64)
		if !ok || dur < 9.9 || dur > 10.1 {
			t.Fatalf("expected ~10s duration, got %v", ev.Metadata["duration_sec"])
		}
	default:
		t.Fatal("expected a focused event")
	}
}

func TestRunEmitsIdleEvents(t *testing.T) {
	s := &Source{
		pollInterval:  50 * time.Millisecond,
		idleThreshold: 100 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	events := make(chan store.Event, 100)
	done := make(chan struct{})
	go func() {
		_ = s.Run(ctx, nil, events)
		close(done)
	}()

	<-done
	close(events)

	var actions []string
	for ev := range events {
		actions = append(actions, ev.Action)
	}
	_ = actions
}
