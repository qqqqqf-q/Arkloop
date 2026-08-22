package keyboard

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"arkloop/services/activity-record/internal/sources/window"
	"arkloop/services/activity-record/internal/store"
)

// keyEvent is a single captured keystroke from the platform listener.
type keyEvent struct {
	timestampMs uint64
	chars       string
	isBackspace bool
	isEnter     bool
	isTab       bool
}

// ---------------------------------------------------------------------------
// typing session (mutex-protected)
// ---------------------------------------------------------------------------

const sessionIdleTimeout = 2 * time.Second

type typingSession struct {
	mu          sync.Mutex
	app         string
	windowTitle string
	buf         strings.Builder
	startedAt   time.Time
	lastKeyAt   time.Time
	keyCount    int
	bsCount     int
	hasPending  bool
}

func (s *typingSession) feed(kev keyEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.hasPending {
		s.buf.Reset()
		s.startedAt = time.Now()
		s.keyCount = 0
		s.bsCount = 0
		s.hasPending = true
	}

	s.lastKeyAt = time.Now()
	s.keyCount++

	if kev.isBackspace {
		s.bsCount++
		content := s.buf.String()
		if len(content) > 0 {
			runes := []rune(content)
			s.buf.Reset()
			s.buf.WriteString(string(runes[:len(runes)-1]))
		}
		return
	}

	if kev.isEnter {
		s.buf.WriteByte('\n')
		return
	}

	if kev.isTab {
		s.buf.WriteByte('\t')
		return
	}

	if kev.chars != "" {
		s.buf.WriteString(kev.chars)
	}
}

func (s *typingSession) flushLocked(now time.Time, events chan<- store.Event) {
	// Caller must hold s.mu.
	if !s.hasPending {
		return
	}
	text := strings.TrimSpace(s.buf.String())
	if text == "" {
		s.buf.Reset()
		s.keyCount = 0
		s.bsCount = 0
		s.hasPending = false
		return
	}
	dur := s.lastKeyAt.Sub(s.startedAt).Seconds()
	if dur < 0.1 {
		dur = 0.1
	}
	events <- store.Event{
		Source:        "keyboard",
		SourceEventID: fmt.Sprintf("keyboard:%d", s.startedAt.UnixMilli()),
		OccurredAt:    s.startedAt,
		App:           s.app,
		WindowTitle:   s.windowTitle,
		Action:        "typing_session",
		Title:         truncateRunes(text, 200),
		Text:          text,
		Metadata: map[string]any{
			"keystroke_count": s.keyCount,
			"backspace_count": s.bsCount,
			"duration_sec":    dur,
		},
	}
	s.buf.Reset()
	s.keyCount = 0
	s.bsCount = 0
	s.hasPending = false
}

func (s *typingSession) tryFlush(now time.Time, currentApp, currentWindow string, events chan<- store.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.hasPending {
		return
	}
	text := strings.TrimSpace(s.buf.String())
	if text == "" {
		return
	}

	shouldFlush := false
	if currentApp != "" && currentApp != s.app {
		shouldFlush = true
	} else if currentWindow != "" && currentWindow != s.windowTitle {
		shouldFlush = true
	} else if time.Since(s.lastKeyAt) >= sessionIdleTimeout {
		shouldFlush = true
	}

	if shouldFlush {
		s.flushLocked(now, events)
	}
}

func (s *typingSession) setWindow(app, winTitle string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if app != "" {
		s.app = app
	}
	if winTitle != "" {
		s.windowTitle = winTitle
	}
}

func (s *typingSession) forceFlush(now time.Time, events chan<- store.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flushLocked(now, events)
}

func truncateRunes(text string, maxLen int) string {
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	return string(runes[:maxLen])
}

// ---------------------------------------------------------------------------
// Source
// ---------------------------------------------------------------------------

type Source struct {
	pollInterval time.Duration
}

func New() *Source {
	return &Source{pollInterval: 1 * time.Second}
}

func (s *Source) Name() string { return "keyboard" }

func (s *Source) Sync(_ context.Context, _ *store.Store) (int, error) {
	return 0, nil
}

func (s *Source) Run(ctx context.Context, _ *store.Store, events chan<- store.Event) error {
	var session typingSession

	go func() {
		if err := listenKeystrokes(ctx, &session); err != nil && ctx.Err() == nil {
			log.Printf("keyboard: listener: %v", err)
		}
	}()

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			session.forceFlush(time.Now(), events)
			return nil
		case now := <-ticker.C:
			app, winTitle := currentWindow()
			session.setWindow(app, winTitle)
			session.tryFlush(now, app, winTitle, events)
		}
	}
}

func currentWindow() (string, string) {
	info, err := window.ActiveWindow()
	if err != nil {
		return "", ""
	}
	return info.App, info.WindowTitle
}
