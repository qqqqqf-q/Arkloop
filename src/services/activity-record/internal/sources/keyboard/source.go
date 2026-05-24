package keyboard

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"arkloop/services/activity-record/internal/sources/window"
	"arkloop/services/activity-record/internal/store"
)

type Source struct {
	emitInterval time.Duration
}

func New() *Source {
	return &Source{emitInterval: 30 * time.Second}
}

func (s *Source) Name() string { return "keyboard" }

func (s *Source) Sync(_ context.Context, _ *store.Store) (int, error) {
	return 0, nil
}

func (s *Source) Run(ctx context.Context, _ *store.Store, events chan<- store.Event) error {
	var count atomic.Int64

	go func() {
		if err := listenKeystrokes(ctx, &count); err != nil && ctx.Err() == nil {
			log.Printf("keyboard: listener: %v", err)
		}
	}()

	ticker := time.NewTicker(s.emitInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			n := count.Swap(0)
			if n == 0 {
				continue
			}
			app, windowTitle := currentWindow()
			title := fmt.Sprintf("%d keystrokes", n)
			if app != "" {
				title = fmt.Sprintf("%d keystrokes in %s", n, app)
			}
			events <- store.Event{
				Source:        "keyboard",
				SourceEventID: fmt.Sprintf("keyboard:%d", now.UnixMilli()),
				OccurredAt:    now,
				Action:        "keystroke_count",
				Title:         title,
				Metadata: map[string]any{
					"count":        n,
					"interval_sec": int(s.emitInterval.Seconds()),
					"app":          app,
					"window_title": windowTitle,
				},
			}
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
