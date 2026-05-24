package window

import (
	"context"
	"fmt"
	"log"
	"time"

	"arkloop/services/activity-record/internal/store"
)

type Source struct {
	pollInterval  time.Duration
	idleThreshold time.Duration
}

type WindowInfo struct {
	App         string
	WindowTitle string
	PID         int
}

func New(idleThreshold time.Duration) *Source {
	if idleThreshold <= 0 {
		idleThreshold = 5 * time.Minute
	}
	return &Source{
		pollInterval:  5 * time.Second,
		idleThreshold: idleThreshold,
	}
}

func (s *Source) Name() string { return "window" }

func (s *Source) Sync(_ context.Context, _ *store.Store) (int, error) {
	return 0, nil
}

func (s *Source) Run(ctx context.Context, db *store.Store, events chan<- store.Event) error {
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	var current WindowInfo
	var focusedAt time.Time
	wasIdle := false

	for {
		select {
		case <-ctx.Done():
			if current.App != "" {
				s.emitFocused(events, current, focusedAt, time.Now())
			}
			return nil
		case now := <-ticker.C:
			idle, err := idleSeconds()
			if err != nil {
				log.Printf("window: idle check: %v", err)
				continue
			}
			isIdle := time.Duration(idle)*time.Second >= s.idleThreshold
			if isIdle && !wasIdle {
				wasIdle = true
				events <- store.Event{
					Source:        "window",
					SourceEventID: fmt.Sprintf("window:idle_start:%d", now.UnixMilli()),
					OccurredAt:    now,
					App:           current.App,
					Action:        "idle_start",
					Title:         "idle",
					Metadata: map[string]any{
						"idle_threshold_sec": int(s.idleThreshold.Seconds()),
					},
				}
				continue
			}
			if !isIdle && wasIdle {
				wasIdle = false
				events <- store.Event{
					Source:        "window",
					SourceEventID: fmt.Sprintf("window:idle_end:%d", now.UnixMilli()),
					OccurredAt:    now,
					App:           current.App,
					Action:        "idle_end",
					Title:         "idle",
				}
			}
			if isIdle {
				continue
			}

			info, err := ActiveWindow()
			if err != nil {
				continue
			}
			if info.App == current.App && info.WindowTitle == current.WindowTitle {
				continue
			}
			if current.App != "" {
				s.emitFocused(events, current, focusedAt, now)
			}
			current = info
			focusedAt = now
		}
	}
}

func (s *Source) emitFocused(events chan<- store.Event, info WindowInfo, start, end time.Time) {
	duration := end.Sub(start).Seconds()
	if duration < 1 {
		return
	}
	events <- store.Event{
		Source:        "window",
		SourceEventID: fmt.Sprintf("window:focused:%d", start.UnixMilli()),
		OccurredAt:    start,
		App:           info.App,
		WindowTitle:   info.WindowTitle,
		Action:        "focused",
		Title:         info.App + " - " + info.WindowTitle,
		Metadata: map[string]any{
			"duration_sec": duration,
			"pid":          info.PID,
		},
	}
}
