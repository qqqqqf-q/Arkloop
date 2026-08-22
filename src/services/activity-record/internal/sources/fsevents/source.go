//go:build darwin

package fsevents

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"arkloop/services/activity-record/internal/store"
)

type Source struct {
	pollInterval time.Duration
}

func New() *Source {
	return &Source{pollInterval: 60 * time.Second}
}

func (s *Source) Name() string { return "fs-events" }

func (s *Source) Sync(_ context.Context, _ *store.Store) (int, error) { return 0, nil }

func (s *Source) Run(ctx context.Context, _ *store.Store, events chan<- store.Event) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("fs-events: home dir: %w", err)
	}

	trashDir := filepath.Join(home, ".Trash")
	downloadsDir := filepath.Join(home, "Downloads")

	var lastTrashCount, lastDownloadCount int
	first := true

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			s.checkTrash(now, trashDir, &lastTrashCount, first, events)
			s.checkDownloads(now, downloadsDir, &lastDownloadCount, first, events)
			first = false
		}
	}
}

func (s *Source) checkTrash(now time.Time, dir string, lastCount *int, first bool, events chan<- store.Event) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	count := len(entries)
	if !first && count != *lastCount {
		events <- store.Event{
			Source:        "fs-events",
			SourceEventID: fmt.Sprintf("trash:%d", now.Unix()),
			OccurredAt:    now,
			Action:        "trash_changed",
			Title:         fmt.Sprintf("Trash: %d items", count),
			Metadata: map[string]any{
				"item_count": count,
				"prev_count": *lastCount,
			},
		}
	}
	*lastCount = count
}

func (s *Source) checkDownloads(now time.Time, dir string, lastCount *int, first bool, events chan<- store.Event) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	// Look for new files (modified within the poll interval).
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		age := time.Since(info.ModTime())
		if age < s.pollInterval {
			events <- store.Event{
				Source:        "fs-events",
				SourceEventID: fmt.Sprintf("download:%s:%d", e.Name(), now.Unix()),
				OccurredAt:    info.ModTime(),
				Action:        "file_downloaded",
				Title:         fmt.Sprintf("Downloaded: %s", e.Name()),
				Metadata: map[string]any{
					"filename": e.Name(),
					"size":     info.Size(),
					"path":     filepath.Join(dir, e.Name()),
				},
			}
		}
	}

	count := len(entries)
	if !first && count > *lastCount {
		events <- store.Event{
			Source:        "fs-events",
			SourceEventID: fmt.Sprintf("downloads:%d", now.Unix()),
			OccurredAt:    now,
			Action:        "downloads_changed",
			Title:         fmt.Sprintf("Downloads: %d files", count),
			Metadata: map[string]any{
				"file_count": count,
			},
		}
	}
	*lastCount = count
}
