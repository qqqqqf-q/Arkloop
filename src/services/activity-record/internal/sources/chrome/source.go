package chrome

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"arkloop/services/activity-record/internal/store"
	_ "modernc.org/sqlite"
)

const chromeEpochOffsetMicros = 11644473600 * 1000 * 1000

type Source struct {
	profiles []profile
}

type profile struct {
	Name string
	Path string
}

type cursor struct {
	Profiles map[string]profileCursor `json:"profiles"`
}

type profileCursor struct {
	MaxVisitTime        int64 `json:"max_visit_time"`
	MaxDownloadTime     int64 `json:"max_download_time"`
	MaxSearchTermURLTime int64 `json:"max_search_term_url_time"`
}

func NewDefault() (*Source, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	browsers := chromeBrowserDirs(home)
	profiles := discoverProfiles(browsers)
	return &Source{profiles: profiles}, nil
}

type browserDir struct {
	Browser string
	Dir     string
}

func chromeBrowserDirs(home string) []browserDir {
	switch runtime.GOOS {
	case "darwin":
		base := filepath.Join(home, "Library", "Application Support")
		return []browserDir{
			{"chrome", filepath.Join(base, "Google", "Chrome")},
			{"chrome-canary", filepath.Join(base, "Google", "Chrome Canary")},
			{"chromium", filepath.Join(base, "Chromium")},
			{"edge", filepath.Join(base, "Microsoft Edge")},
		}
	case "linux":
		return []browserDir{
			{"chrome", filepath.Join(home, ".config", "google-chrome")},
			{"chromium", filepath.Join(home, ".config", "chromium")},
			{"edge", filepath.Join(home, ".config", "microsoft-edge")},
		}
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = filepath.Join(home, "AppData", "Local")
		}
		return []browserDir{
			{"chrome", filepath.Join(localAppData, "Google", "Chrome", "User Data")},
			{"chrome-canary", filepath.Join(localAppData, "Google", "Chrome SxS", "User Data")},
			{"chromium", filepath.Join(localAppData, "Chromium", "User Data")},
			{"edge", filepath.Join(localAppData, "Microsoft", "Edge", "User Data")},
		}
	default:
		return nil
	}
}

func discoverProfiles(browsers []browserDir) []profile {
	var profiles []profile
	for _, browser := range browsers {
		if _, err := os.Stat(browser.Dir); err != nil {
			continue
		}
		candidates := []string{"Default"}
		entries, err := os.ReadDir(browser.Dir)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() && strings.HasPrefix(entry.Name(), "Profile ") {
					candidates = append(candidates, entry.Name())
				}
			}
		}
		for _, profileDir := range candidates {
			historyPath := filepath.Join(browser.Dir, profileDir, "History")
			if _, err := os.Stat(historyPath); err == nil {
				name := browser.Browser
				if profileDir != "Default" {
					name = browser.Browser + "-" + strings.ToLower(strings.ReplaceAll(profileDir, " ", ""))
				}
				profiles = append(profiles, profile{Name: name, Path: historyPath})
			}
		}
	}
	return profiles
}

func (s *Source) Name() string {
	return "chrome"
}

func (s *Source) Sync(ctx context.Context, db *store.Store) (int, error) {
	var cur cursor
	if err := db.Cursor(ctx, s.Name(), &cur); err != nil {
		return 0, err
	}
	if cur.Profiles == nil {
		cur.Profiles = map[string]profileCursor{}
	}
	events := make([]store.Event, 0)
	for _, profile := range s.profiles {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}
		profileCur := cur.Profiles[profile.Name]
		nextCur, profileEvents, err := syncProfile(ctx, profile, profileCur)
		if err != nil {
			_ = db.SaveCursor(ctx, s.Name(), cur, err)
			return 0, err
		}
		cur.Profiles[profile.Name] = nextCur
		events = append(events, profileEvents...)
	}
	inserted, err := db.UpsertEvents(ctx, events)
	if err != nil {
		_ = db.SaveCursor(ctx, s.Name(), cur, err)
		return 0, err
	}
	if err := db.SaveCursor(ctx, s.Name(), cur, nil); err != nil {
		return 0, err
	}
	return inserted, nil
}

func syncProfile(ctx context.Context, profile profile, cur profileCursor) (profileCursor, []store.Event, error) {
	tmp, err := copyHistory(profile.Path)
	if err != nil {
		return cur, nil, err
	}
	defer os.Remove(tmp)

	db, err := sql.Open("sqlite", "file:"+tmp+"?mode=ro&immutable=1")
	if err != nil {
		return cur, nil, err
	}
	defer db.Close()

	events := make([]store.Event, 0)
	next := cur

	visitRows, err := db.QueryContext(ctx, `
SELECT v.id, v.visit_time, v.visit_duration, u.url, u.title, COALESCE(ca.total_foreground_duration, 0)
  FROM visits v
  JOIN urls u ON v.url = u.id
  LEFT JOIN context_annotations ca ON ca.visit_id = v.id
 WHERE v.visit_time > ?
 ORDER BY v.visit_time ASC
`, cur.MaxVisitTime)
	if err != nil {
		return cur, nil, err
	}
	for visitRows.Next() {
		var id int64
		var visitTime int64
		var duration sql.NullInt64
		var url sql.NullString
		var title sql.NullString
		var foreground sql.NullInt64
		if err := visitRows.Scan(&id, &visitTime, &duration, &url, &title, &foreground); err != nil {
			visitRows.Close()
			return cur, nil, err
		}
		if title.String == "" {
			continue
		}
		if visitTime > next.MaxVisitTime {
			next.MaxVisitTime = visitTime
		}
		events = append(events, store.Event{
			Source:        "chrome",
			SourceEventID: fmt.Sprintf("%s:visit:%d", profile.Name, id),
			OccurredAt:    chromeTime(visitTime),
			App:           profile.Name,
			URL:           url.String,
			Action:        "visited",
			Title:         title.String,
			Metadata: map[string]any{
				"profile":        profile.Name,
				"duration_sec":   secondsFromNullableMicros(duration),
				"foreground_sec": secondsFromNullableMicros(foreground),
			},
			RefKind: "url",
			RefKey:  url.String,
		})
	}
	if err := visitRows.Close(); err != nil {
		return cur, nil, err
	}

	downloadRows, err := db.QueryContext(ctx, `
SELECT id, start_time, target_path, tab_url, total_bytes, mime_type
  FROM downloads
 WHERE start_time > ?
 ORDER BY start_time ASC
`, cur.MaxDownloadTime)
	if err != nil {
		return cur, nil, err
	}
	for downloadRows.Next() {
		var id int64
		var startTime int64
		var targetPath sql.NullString
		var tabURL sql.NullString
		var totalBytes sql.NullInt64
		var mimeType sql.NullString
		if err := downloadRows.Scan(&id, &startTime, &targetPath, &tabURL, &totalBytes, &mimeType); err != nil {
			downloadRows.Close()
			return cur, nil, err
		}
		if targetPath.String == "" {
			continue
		}
		if startTime > next.MaxDownloadTime {
			next.MaxDownloadTime = startTime
		}
		events = append(events, store.Event{
			Source:        "chrome",
			SourceEventID: fmt.Sprintf("%s:download:%d", profile.Name, id),
			OccurredAt:    chromeTime(startTime),
			App:           profile.Name,
			URL:           tabURL.String,
			Action:        "downloaded",
			Title:         filepath.Base(targetPath.String),
			Metadata: map[string]any{
				"profile":    profile.Name,
				"path":       targetPath.String,
				"size_bytes": nullableInt64(totalBytes),
				"mime_type":  mimeType.String,
			},
			RefKind: "url",
			RefKey:  tabURL.String,
		})
	}
	if err := downloadRows.Close(); err != nil {
		return cur, nil, err
	}

	searchRows, err := db.QueryContext(ctx, `
SELECT kst.url_id, kst.term, kst.normalized_term, u.url, u.title, u.last_visit_time
  FROM keyword_search_terms kst
  JOIN urls u ON u.id = kst.url_id
 WHERE u.last_visit_time > ?
 ORDER BY u.last_visit_time ASC
`, cur.MaxSearchTermURLTime)
	if err != nil {
		return cur, nil, err
	}
	for searchRows.Next() {
		var urlID int64
		var term string
		var normalizedTerm string
		var url sql.NullString
		var title sql.NullString
		var lastVisitTime int64
		if err := searchRows.Scan(&urlID, &term, &normalizedTerm, &url, &title, &lastVisitTime); err != nil {
			searchRows.Close()
			return cur, nil, err
		}
		if term == "" {
			continue
		}
		if lastVisitTime > next.MaxSearchTermURLTime {
			next.MaxSearchTermURLTime = lastVisitTime
		}
		events = append(events, store.Event{
			Source:        "chrome",
			SourceEventID: fmt.Sprintf("%s:search:%d", profile.Name, urlID),
			OccurredAt:    chromeTime(lastVisitTime),
			App:           profile.Name,
			URL:           url.String,
			Action:        "searched",
			Title:         term,
			Metadata: map[string]any{
				"profile":         profile.Name,
				"normalized_term": normalizedTerm,
			},
			RefKind: "url",
			RefKey:  url.String,
		})
	}
	if err := searchRows.Close(); err != nil {
		return cur, nil, err
	}
	return next, events, nil
}

func copyHistory(path string) (string, error) {
	input, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer input.Close()
	tmp, err := os.CreateTemp("", "arkloop-chrome-history-*.sqlite")
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

func chromeTime(value int64) time.Time {
	return time.UnixMicro(value - chromeEpochOffsetMicros).UTC()
}

func secondsFromNullableMicros(value sql.NullInt64) float64 {
	if !value.Valid || value.Int64 <= 0 {
		return 0
	}
	return float64(value.Int64) / 1000000
}

func nullableInt64(value sql.NullInt64) int64 {
	if !value.Valid {
		return 0
	}
	return value.Int64
}

