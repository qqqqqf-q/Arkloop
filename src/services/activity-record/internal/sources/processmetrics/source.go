//go:build darwin

package processmetrics

import (
	"context"
	"fmt"
	"log"
	"time"

	"arkloop/services/activity-record/internal/store"
)

type Source struct {
	sampleInterval time.Duration
}

func New() *Source {
	return &Source{sampleInterval: 30 * time.Second}
}

func (s *Source) Name() string { return "process-metrics" }

func (s *Source) Sync(_ context.Context, _ *store.Store) (int, error) {
	return 0, nil
}

func (s *Source) Run(ctx context.Context, _ *store.Store, events chan<- store.Event) error {
	// First sample to establish baseline.
	procs, hostTicks, err := sampleProcs()
	if err != nil {
		return fmt.Errorf("process-metrics: initial sample: %w", err)
	}
	net, err := sampleNet()
	if err != nil {
		log.Printf("process-metrics: initial net sample: %v", err)
	}

	prevHost := hostSample{ticks: hostTicks, netRX: net.RXBytes, netTX: net.TXBytes}
	prevProcs := make(map[int]procSample, len(procs))
	for _, p := range procs {
		prevProcs[p.PID] = p
	}

	// Emit initial process starts.
	now := time.Now()
	for _, p := range procs {
		events <- store.Event{
			Source:        "process-metrics",
			SourceEventID: fmt.Sprintf("proc:start:%d:%d", p.PID, now.Unix()),
			OccurredAt:    now,
			App:           p.Name,
			Action:        "process_start",
			Title:         fmt.Sprintf("%s (pid %d)", p.Name, p.PID),
			Metadata: map[string]any{
				"pid":  p.PID,
				"name": p.Name,
			},
		}
	}

	ticker := time.NewTicker(s.sampleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			newEvents, curProcs, curHost, sampleErr := collect(prevHost, prevProcs, now)
			if sampleErr != nil {
				log.Printf("process-metrics: %v", sampleErr)
				continue
			}
			for _, ev := range newEvents {
				events <- ev
			}
			prevHost = curHost
			prevProcs = curProcs
		}
	}
}

func collect(prevHost hostSample, prevProcs map[int]procSample, now time.Time) ([]store.Event, map[int]procSample, hostSample, error) {
	samples, hostTicks, err := sampleProcs()
	if err != nil {
		return nil, nil, hostSample{}, err
	}
	net, err := sampleNet()
	if err != nil {
		return nil, nil, hostSample{}, err
	}

	cur := hostSample{ticks: hostTicks, netRX: net.RXBytes, netTX: net.TXBytes}
	tickDelta := cur.ticks - prevHost.ticks
	netRXRate := float64(cur.netRX-prevHost.netRX) / 30.0 / 1024.0
	netTXRate := float64(cur.netTX-prevHost.netTX) / 30.0 / 1024.0

	curProcs := make(map[int]procSample, len(samples))
	seen := make(map[int]bool, len(samples))
	var events []store.Event

	for _, s := range samples {
		curProcs[s.PID] = s
		prev, existed := prevProcs[s.PID]

		cpuPct := 0.0
		if existed && tickDelta > 0 {
			cpuPct = (s.CPUTicks - prev.CPUTicks) / tickDelta * 100.0
			if cpuPct < 0 {
				cpuPct = 0
			}
		}

		rssDelta := int64(0)
		if existed {
			rssDelta = int64(s.RSS) - int64(prev.RSS)
		}

		shouldEmit := cpuPct > 0.1 || rssDelta > 50*1024*1024 || rssDelta < -50*1024*1024
		if shouldEmit {
			events = append(events, store.Event{
				Source:        "process-metrics",
				SourceEventID: fmt.Sprintf("proc:%d:%d", s.PID, now.Unix()),
				OccurredAt:    now,
				App:           s.Name,
				Action:        "process_metrics",
				Title:         fmt.Sprintf("%s cpu=%.1f%% rss=%dM", s.Name, cpuPct, s.RSS/(1024*1024)),
				Metadata: map[string]any{
					"pid":     s.PID,
					"name":    s.Name,
					"cpu_pct": cpuPct,
					"rss_mb":  float64(s.RSS) / (1024 * 1024),
					"vsz_mb":  float64(s.VSZ) / (1024 * 1024),
				},
			})
		}
		seen[s.PID] = true
	}

	// Process exits.
	for pid, prev := range prevProcs {
		if !seen[pid] {
			events = append(events, store.Event{
				Source:        "process-metrics",
				SourceEventID: fmt.Sprintf("proc:exit:%d:%d", pid, now.Unix()),
				OccurredAt:    now,
				App:           prev.Name,
				Action:        "process_exit",
				Title:         fmt.Sprintf("%s (pid %d) exited", prev.Name, pid),
				Metadata: map[string]any{
					"pid":  pid,
					"name": prev.Name,
				},
			})
		}
	}

	// Process starts.
	for pid, s := range curProcs {
		if _, existed := prevProcs[pid]; !existed {
			events = append(events, store.Event{
				Source:        "process-metrics",
				SourceEventID: fmt.Sprintf("proc:start:%d:%d", pid, now.Unix()),
				OccurredAt:    now,
				App:           s.Name,
				Action:        "process_start",
				Title:         fmt.Sprintf("%s (pid %d) started", s.Name, pid),
				Metadata: map[string]any{
					"pid":  pid,
					"name": s.Name,
				},
			})
		}
	}

	// System network.
	if netRXRate > 1.0 || netTXRate > 1.0 {
		events = append(events, store.Event{
			Source:        "process-metrics",
			SourceEventID: fmt.Sprintf("net:%d", now.Unix()),
			OccurredAt:    now,
			Action:        "network_io",
			Title:         fmt.Sprintf("net rx=%.1fKB/s tx=%.1fKB/s", netRXRate, netTXRate),
			Metadata: map[string]any{
				"rx_kbps": netRXRate,
				"tx_kbps": netTXRate,
			},
		})
	}

	return events, curProcs, cur, nil
}
