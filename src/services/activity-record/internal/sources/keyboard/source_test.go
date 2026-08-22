package keyboard

import (
	"strings"
	"sync"
	"testing"
	"time"

	"arkloop/services/activity-record/internal/store"
)

func TestTypingSession_FeedAndFlush(t *testing.T) {
	var s typingSession

	s.feed(keyEvent{chars: "h", timestampMs: 1000})
	s.feed(keyEvent{chars: "e", timestampMs: 1100})
	s.feed(keyEvent{chars: "l", timestampMs: 1200})
	s.feed(keyEvent{chars: "l", timestampMs: 1300})
	s.feed(keyEvent{chars: "o", timestampMs: 1400})

	events := make(chan store.Event, 10)
	s.forceFlush(time.Now(), events)
	close(events)

	var count int
	for ev := range events {
		count++
		if ev.Action != "typing_session" {
			t.Fatalf("expected typing_session action, got %s", ev.Action)
		}
		if ev.Text != "hello" {
			t.Fatalf("expected 'hello' text, got %q", ev.Text)
		}
		if ev.Source != "keyboard" {
			t.Fatalf("expected keyboard source, got %s", ev.Source)
		}
		kc := ev.Metadata["keystroke_count"]
		if kc != 5 {
			t.Fatalf("expected 5 keystroke_count, got %v", kc)
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 event, got %d", count)
	}
}

func TestTypingSession_Backspace(t *testing.T) {
	var s typingSession

	s.feed(keyEvent{chars: "a"})
	s.feed(keyEvent{chars: "b"})
	s.feed(keyEvent{chars: "c"})
	s.feed(keyEvent{isBackspace: true})
	s.feed(keyEvent{chars: "d"})

	events := make(chan store.Event, 10)
	s.forceFlush(time.Now(), events)
	close(events)

	var ok bool
	for ev := range events {
		if ev.Text == "abd" {
			ok = true
		}
	}
	if !ok {
		t.Fatal("expected 'abd' after backspace and retype")
	}
}

func TestTypingSession_EnterAndTab(t *testing.T) {
	var s typingSession

	s.feed(keyEvent{chars: "a"})
	s.feed(keyEvent{isEnter: true})
	s.feed(keyEvent{chars: "b"})
	s.feed(keyEvent{isTab: true})
	s.feed(keyEvent{chars: "c"})

	events := make(chan store.Event, 10)
	s.forceFlush(time.Now(), events)
	close(events)

	for ev := range events {
		if ev.Text != "a\nb\tc" {
			t.Fatalf("expected 'a\\nb\\tc', got %q", ev.Text)
		}
	}
}

func TestTypingSession_TryFlushOnAppChange(t *testing.T) {
	var s typingSession
	s.setWindow("App1", "Win1")
	s.feed(keyEvent{chars: "y"})

	events := make(chan store.Event, 10)
	s.tryFlush(time.Now(), "App2", "Win1", events)
	close(events)

	for ev := range events {
		if ev.Action == "typing_session" && ev.Text == "y" {
			return
		}
	}
	t.Fatal("expected flush on app change")
}

func TestTypingSession_NoFlushSameWindow(t *testing.T) {
	var s typingSession
	s.setWindow("App1", "Win1")
	s.feed(keyEvent{chars: "z"})

	events := make(chan store.Event, 10)
	s.tryFlush(time.Now(), "App1", "Win1", events)
	close(events)

	for range events {
		t.Fatal("should not flush on same window")
	}
}

func TestTypingSession_EmptyFlush(t *testing.T) {
	var s typingSession
	events := make(chan store.Event, 10)

	s.feed(keyEvent{chars: "a"})
	s.feed(keyEvent{isBackspace: true})

	s.forceFlush(time.Now(), events)
	close(events)

	for range events {
		t.Fatal("expected no events for empty text")
	}
}

func TestTruncateRunes(t *testing.T) {
	text := strings.Repeat("a", 300)
	result := truncateRunes(text, 200)
	if len([]rune(result)) != 200 {
		t.Fatalf("expected 200 runes, got %d", len([]rune(result)))
	}

	short := "hello"
	result = truncateRunes(short, 200)
	if result != short {
		t.Fatalf("expected %q, got %q", short, result)
	}
}

func TestTypingSession_ConcurrentSafety(t *testing.T) {
	var s typingSession
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			s.feed(keyEvent{chars: "x"})
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			events := make(chan store.Event, 10)
			s.tryFlush(time.Now(), "App", "Win", events)
		}
	}()

	wg.Wait()
}
