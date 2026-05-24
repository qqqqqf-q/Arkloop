//go:build darwin

package safari

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"arkloop/services/activity-record/internal/store"
	_ "modernc.org/sqlite"
)

type Source struct {
	dbPath string
}

type cursor struct {
	MaxVisitTime float64 `json:"max_visit_time"`
}

func NewDefault() (*Source, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(home, "Library", "Safari", "History.db")
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("Safari History.db not accessible (Full Disk Access required): %w", err)
	}
	return &Source{dbPath: dbPath}, nil
}

func (s *Source) Name() string {
	return "safari"
}

func (s *Source) Sync(ctx context.Context, db *store.Store) (int, error) {
	var cur cursor
	if err := db.Cursor(ctx, s.Name(), &cur); err != nil {
		return 0, err
	}
	tmp, err := copyDB(s.dbPath)
	if err != nil {
		return 0, fmt.Errorf("copy Safari History.db: %w", err)
	}
	defer os.Remove(tmp)

	srcDB, err := sql.Open("sqlite", "file:"+tmp+"?mode=ro&immutable=1")
	if err != nil {
		return 0, err
	}
	defer srcDB.Close()

	rows, err := srcDB.QueryContext(ctx, `
SELECT
  hv.id,
  hv.visit_time,
  hi.url,
  COALESCE(hi.domain_expansion, ''),
  COALESCE(hv.title, ''),
  hi.visit_count,
  hv.load_successful,
  hv.score
FROM history_visits hv
JOIN history_items hi ON hi.id = hv.history_item
WHERE hv.visit_time > ?
ORDER BY hv.visit_time ASC
`, cur.MaxVisitTime)
	if err != nil {
		return 0, fmt.Errorf("query Safari History.db: %w", err)
	}

	var events []store.Event
	next := cur
	for rows.Next() {
		var (
			id              int64
			visitTime       float64
			url             string
			domainExpansion string
			title           string
			visitCount      int64
			loadSuccessful  int64
			score           int64
		)
		if err := rows.Scan(&id, &visitTime, &url, &domainExpansion, &title, &visitCount, &loadSuccessful, &score); err != nil {
			rows.Close()
			return 0, err
		}
		if visitTime > next.MaxVisitTime {
			next.MaxVisitTime = visitTime
		}
		ev := store.Event{
			Source:        "safari",
			SourceEventID: fmt.Sprintf("safari:visit:%d", id),
			OccurredAt:    cocoaTime(visitTime),
			App:           "Safari",
			URL:           url,
			Action:        "visited",
			Title:         title,
			Metadata: map[string]any{
				"domain":          domainExpansion,
				"visit_count":     visitCount,
				"load_successful": loadSuccessful == 1,
				"score":           score,
			},
			RefKind: "url",
			RefKey:  url,
		}
		if ev.Title == "" {
			ev.Title = domainExpansion
		}
		if ev.Title == "" {
			ev.Title = url
		}
		events = append(events, ev)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	inserted, err := db.UpsertEvents(ctx, events)
	if err != nil {
		_ = db.SaveCursor(ctx, s.Name(), cur, err)
		return 0, err
	}
	if err := db.SaveCursor(ctx, s.Name(), next, nil); err != nil {
		return 0, err
	}
	return inserted, nil
}

var cocoaEpoch = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

func cocoaTime(seconds float64) time.Time {
	return cocoaEpoch.Add(time.Duration(seconds * float64(time.Second))).UTC()
}

func copyDB(path string) (string, error) {
	input, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer input.Close()
	tmp, err := os.CreateTemp("", "arkloop-safari-*.sqlite")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	if _, err := io.Copy(tmp, input); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return "", err
	}
	return tmpPath, nil
}
