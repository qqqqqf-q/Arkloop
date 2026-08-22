//go:build darwin

package location

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"arkloop/services/activity-record/internal/store"
)

type Source struct {
	pollInterval time.Duration
	httpClient   *http.Client
}

type geoIPResponse struct {
	Country     string  `json:"country"`
	City        string  `json:"city"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	Timezone    string  `json:"timezone"`
	ISP         string  `json:"isp"`
}

func New() *Source {
	return &Source{
		pollInterval: 300 * time.Second,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *Source) Name() string { return "location" }

func (s *Source) Sync(_ context.Context, _ *store.Store) (int, error) { return 0, nil }

func (s *Source) Run(ctx context.Context, _ *store.Store, events chan<- store.Event) error {
	// Emit initial environment info.
	s.emitEnv(events)

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			s.checkGeoIP(now, events)
			s.checkUSB(now, events)
		}
	}
}

func (s *Source) emitEnv(events chan<- store.Event) {
	now := time.Now()
	tz, _ := time.Now().Zone()

	lang := os.Getenv("LANG")
	if lang == "" {
		lang = os.Getenv("LC_ALL")
	}

	events <- store.Event{
		Source:        "location",
		SourceEventID: fmt.Sprintf("env:%d", now.Unix()),
		OccurredAt:    now,
		Action:        "environment",
		Title:         fmt.Sprintf("tz=%s lang=%s", tz, lang),
		Metadata: map[string]any{
			"timezone": tz,
			"lang":     lang,
		},
	}
}

func (s *Source) checkGeoIP(now time.Time, events chan<- store.Event) {
	resp, err := s.httpClient.Get("http://ip-api.com/json/?fields=country,city,lat,lon,timezone,isp")
	if err != nil {
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return
	}

	var geo geoIPResponse
	if err := json.Unmarshal(body, &geo); err != nil {
		return
	}
	if geo.Country == "" {
		return
	}

	events <- store.Event{
		Source:        "location",
		SourceEventID: fmt.Sprintf("geoip:%d", now.Unix()),
		OccurredAt:    now,
		Action:        "geoip",
		Title:         fmt.Sprintf("%s, %s (%s)", geo.City, geo.Country, geo.ISP),
		Metadata: map[string]any{
			"country":  geo.Country,
			"city":     geo.City,
			"lat":      geo.Lat,
			"lon":      geo.Lon,
			"timezone": geo.Timezone,
			"isp":      geo.ISP,
		},
	}
}

func (s *Source) checkUSB(now time.Time, events chan<- store.Event) {
	// Poll /dev for USB serial devices as a lightweight indicator.
	entries, err := os.ReadDir("/dev")
	if err != nil {
		return
	}
	var devices []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "cu.") || strings.HasPrefix(name, "tty.") {
			if strings.Contains(name, "usb") || strings.Contains(name, "USB") {
				devices = append(devices, name)
			}
		}
	}
	if len(devices) > 0 {
		events <- store.Event{
			Source:        "location",
			SourceEventID: fmt.Sprintf("usb:%d", now.Unix()),
			OccurredAt:    now,
			Action:        "usb_devices",
			Title:         fmt.Sprintf("%d USB serial devices", len(devices)),
			Metadata: map[string]any{
				"devices": devices,
			},
		}
	}

	// Also check /Volumes for mounted external drives.
	volEntries, err := os.ReadDir("/Volumes")
	if err != nil {
		return
	}
	for _, e := range volEntries {
		if e.Name() == "Macintosh HD" || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		events <- store.Event{
			Source:        "location",
			SourceEventID: fmt.Sprintf("volume:%s:%d", e.Name(), now.Unix()),
			OccurredAt:    now,
			Action:        "volume_mounted",
			Title:         fmt.Sprintf("Volume: %s", e.Name()),
			Metadata: map[string]any{
				"volume": e.Name(),
				"path":   filepath.Join("/Volumes", e.Name()),
			},
		}
	}
}
