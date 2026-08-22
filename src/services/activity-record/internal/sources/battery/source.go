//go:build darwin

package battery

import (
	"context"
	"fmt"
	"log"
	"time"

	"arkloop/services/activity-record/internal/store"
)

type Source struct {
	pollInterval time.Duration
}

func New() *Source {
	return &Source{pollInterval: 30 * time.Second}
}

func (s *Source) Name() string { return "battery" }

func (s *Source) Sync(_ context.Context, _ *store.Store) (int, error) { return 0, nil }

func (s *Source) Run(ctx context.Context, _ *store.Store, events chan<- store.Event) error {
	var prev battSample
	first := true

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			bs, err := sampleBattery()
			if err != nil {
				log.Printf("battery: %v", err)
				continue
			}
			if !bs.Present {
				continue
			}
			if first || bs.Pct != prev.Pct || bs.IsCharging != prev.IsCharging {
				action := "battery_update"
				title := fmt.Sprintf("battery %d%%", bs.Pct)
				if bs.IsCharging {
					title += " (charging)"
				}
				events <- store.Event{
					Source:        "battery",
					SourceEventID: fmt.Sprintf("battery:%d", now.Unix()),
					OccurredAt:    now,
					Action:        action,
					Title:         title,
					Metadata: map[string]any{
						"pct":        bs.Pct,
						"charging":   bs.IsCharging,
					},
				}
				prev = bs
				first = false
			}
		}
	}
}
