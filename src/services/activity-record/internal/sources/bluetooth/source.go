//go:build darwin

package bluetooth

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
	return &Source{pollInterval: 60 * time.Second}
}

func (s *Source) Name() string { return "bluetooth" }

func (s *Source) Sync(_ context.Context, _ *store.Store) (int, error) { return 0, nil }

func (s *Source) Run(ctx context.Context, _ *store.Store, events chan<- store.Event) error {
	prevState := make(map[string]bool)

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			devices, err := pollDevices()
			if err != nil {
				log.Printf("bluetooth: %v", err)
				continue
			}
			curState := make(map[string]bool, len(devices))
			for _, d := range devices {
				curState[d.Name] = d.Connected
				prevConnected, existed := prevState[d.Name]
				if !existed {
					if d.Connected {
						events <- store.Event{
							Source:        "bluetooth",
							SourceEventID: fmt.Sprintf("bt:connect:%s:%d", d.Name, now.Unix()),
							OccurredAt:    now,
							Action:        "bluetooth_connected",
							Title:         fmt.Sprintf("BT %s connected", d.Name),
							Metadata: map[string]any{
								"device": d.Name,
							},
						}
					}
				} else if prevConnected != d.Connected {
					action := "bluetooth_disconnected"
					title := fmt.Sprintf("BT %s disconnected", d.Name)
					if d.Connected {
						action = "bluetooth_connected"
						title = fmt.Sprintf("BT %s connected", d.Name)
					}
					events <- store.Event{
						Source:        "bluetooth",
						SourceEventID: fmt.Sprintf("bt:%s:%d", d.Name, now.Unix()),
						OccurredAt:    now,
						Action:        action,
						Title:         title,
						Metadata: map[string]any{
							"device": d.Name,
						},
					}
				}
			}
			// Detect disconnects for previously-seen devices that disappeared.
			for name, wasConnected := range prevState {
				if _, exists := curState[name]; !exists && wasConnected {
					events <- store.Event{
						Source:        "bluetooth",
						SourceEventID: fmt.Sprintf("bt:disconnect:%s:%d", name, now.Unix()),
						OccurredAt:    now,
						Action:        "bluetooth_disconnected",
						Title:         fmt.Sprintf("BT %s disconnected", name),
						Metadata: map[string]any{
							"device": name,
						},
					}
				}
			}
			prevState = curState
		}
	}
}
