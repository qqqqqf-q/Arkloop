package mouse

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"arkloop/services/activity-record/internal/sources/window"
	"arkloop/services/activity-record/internal/store"
)

type counters struct {
	clicks  atomic.Int64
	scrolls atomic.Int64
}

type Source struct {
	emitInterval time.Duration
}

func New() *Source {
	return &Source{emitInterval: 30 * time.Second}
}

func (s *Source) Name() string { return "mouse" }

func (s *Source) Sync(_ context.Context, _ *store.Store) (int, error) {
	return 0, nil
}

func (s *Source) Run(ctx context.Context, _ *store.Store, events chan<- store.Event) error {
	var c counters

	go func() {
		if err := listenMouse(ctx, &c); err != nil && ctx.Err() == nil {
			log.Printf("mouse: listener: %v", err)
		}
	}()

	ticker := time.NewTicker(s.emitInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			clicks := c.clicks.Swap(0)
			scrolls := c.scrolls.Swap(0)
			if clicks == 0 && scrolls == 0 {
				continue
			}
			app, windowTitle := currentWindow()
			title := fmt.Sprintf("%d clicks, %d scrolls", clicks, scrolls)
			if app != "" {
				title = fmt.Sprintf("%d clicks, %d scrolls in %s", clicks, scrolls, app)
			}
			events <- store.Event{
				Source:        "mouse",
				SourceEventID: fmt.Sprintf("mouse:%d", now.UnixMilli()),
				OccurredAt:    now,
				Action:        "mouse_activity",
				Title:         title,
				Metadata: map[string]any{
					"clicks":       clicks,
					"scrolls":      scrolls,
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
