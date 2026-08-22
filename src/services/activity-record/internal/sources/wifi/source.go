//go:build darwin

package wifi

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

func (s *Source) Name() string { return "wifi" }

func (s *Source) Sync(_ context.Context, _ *store.Store) (int, error) { return 0, nil }

func (s *Source) Run(ctx context.Context, _ *store.Store, events chan<- store.Event) error {
	var prev wifiSample
	first := true

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			ws, err := sampleWiFi()
			if err != nil {
				log.Printf("wifi: %v", err)
				continue
			}
			if first || ws.SSID != prev.SSID || ws.BSSID != prev.BSSID {
				events <- store.Event{
					Source:        "wifi",
					SourceEventID: fmt.Sprintf("wifi:%d", now.Unix()),
					OccurredAt:    now,
					Action:        "wifi_connected",
					Title:         fmt.Sprintf("WiFi %s rssi=%d", ws.SSID, ws.RSSI),
					Metadata: map[string]any{
						"ssid":  ws.SSID,
						"bssid": ws.BSSID,
						"rssi":  ws.RSSI,
					},
				}
				prev = ws
				first = false
			}
		}
	}
}
