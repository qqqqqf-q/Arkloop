package mouse

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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

func (s *Source) Name() string { return "mouse" }

func (s *Source) Sync(_ context.Context, _ *store.Store) (int, error) {
	return 0, nil
}

func (s *Source) Run(ctx context.Context, _ *store.Store, events chan<- store.Event) error {
	var agg mouseAgg

	go func() {
		if err := listenMouse(ctx, &agg); err != nil && ctx.Err() == nil {
			log.Printf("mouse: listener: %v", err)
		}
	}()

	// Emit path events more frequently (every 5s) to avoid huge batches.
	pathTicker := time.NewTicker(5 * time.Second)
	defer pathTicker.Stop()

	ticker := time.NewTicker(s.emitInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.emitAgg(time.Now(), events, &agg)
			return nil
		case now := <-ticker.C:
			s.emitAgg(now, events, &agg)
		case <-pathTicker.C:
			s.emitPath(events, &agg)
		}
	}
}

func (s *Source) emitAgg(now time.Time, events chan<- store.Event, agg *mouseAgg) {
	clicks := agg.clicks
	scrolls := agg.scrolls
	agg.clicks = 0
	agg.scrolls = 0

	if clicks == 0 && scrolls == 0 {
		return
	}

	app, winTitle := currentWindow()
	title := fmt.Sprintf("%d clicks, %d scrolls", clicks, scrolls)
	if app != "" {
		title = fmt.Sprintf("%d clicks, %d scrolls in %s", clicks, scrolls, app)
	}
	events <- store.Event{
		Source:        "mouse",
		SourceEventID: fmt.Sprintf("mouse:activity:%d", now.UnixMilli()),
		OccurredAt:    now,
		Action:        "mouse_activity",
		Title:         title,
		Metadata: map[string]any{
			"clicks":       clicks,
			"scrolls":      scrolls,
			"interval_sec": int(s.emitInterval.Seconds()),
			"app":          app,
			"window_title": winTitle,
		},
	}
}

func (s *Source) emitPath(events chan<- store.Event, agg *mouseAgg) {
	pts := agg.pathEvents
	agg.pathEvents = agg.pathEvents[:0]

	if len(pts) == 0 {
		return
	}

	// Build a compact JSON path array: [[x,y,ts_ms], ...]
	type point [3]float64
	path := make([]point, len(pts))
	for i, p := range pts {
		path[i] = point{p.x, p.y, float64(p.at.UnixMilli())}
	}
	pathJSON, err := json.Marshal(path)
	if err != nil {
		return
	}

	app, winTitle := currentWindow()
	start := pts[0].at
	events <- store.Event{
		Source:        "mouse",
		SourceEventID: fmt.Sprintf("mouse:path:%d", start.UnixMilli()),
		OccurredAt:    start,
		App:           app,
		WindowTitle:   winTitle,
		Action:        "mouse_path",
		Title:         fmt.Sprintf("%d mouse points", len(pts)),
		Text:          string(pathJSON),
		Metadata: map[string]any{
			"point_count": len(pts),
			"app":         app,
			"window_title": winTitle,
		},
	}
}

func currentWindow() (string, string) {
	info, err := window.ActiveWindow()
	if err != nil {
		return "", ""
	}
	return info.App, info.WindowTitle
}
