package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
	mu sync.Mutex
}

type Event struct {
	Source        string
	SourceEventID string
	OccurredAt    time.Time
	App           string
	WindowTitle   string
	URL           string
	Action        string
	Title         string
	Text          string
	Metadata      map[string]any
	RefKind       string
	RefKey        string
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, err
	}
	store := &Store{db: db}
	if err := store.Migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, schemaSQL)
	return err
}

func (s *Store) Cursor(ctx context.Context, source string, dest any) error {
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT cursor_json FROM source_cursors WHERE source = ?`, source).Scan(&payload)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if payload == "" {
		return nil
	}
	return json.Unmarshal([]byte(payload), dest)
}

func (s *Store) SaveCursor(ctx context.Context, source string, cursor any, syncErr error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, err := json.Marshal(cursor)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	lastError := ""
	if syncErr != nil {
		lastError = syncErr.Error()
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO source_cursors (source, cursor_json, last_sync_at, last_success_at, last_error)
VALUES (?, ?, ?, CASE WHEN ? = '' THEN ? ELSE NULL END, ?)
ON CONFLICT(source) DO UPDATE SET
  cursor_json = excluded.cursor_json,
  last_sync_at = excluded.last_sync_at,
  last_success_at = CASE WHEN excluded.last_error = '' THEN excluded.last_sync_at ELSE source_cursors.last_success_at END,
  last_error = excluded.last_error
`, source, string(payload), now, lastError, now, lastError)
	return err
}

func (s *Store) UpsertEvents(ctx context.Context, events []Event) (int, error) {
	if len(events) == 0 {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	events = dedupeEvents(events)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	existingStmt, err := tx.PrepareContext(ctx, `
SELECT occurred_at, app, window_title, url, action, title, text, COALESCE(metadata_json, ''), ref_kind, ref_key
  FROM activity_events
 WHERE source = ? AND source_event_id = ?
`)
	if err != nil {
		return 0, err
	}
	defer existingStmt.Close()

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO activity_events (
  source, source_event_id, occurred_at, app, window_title, url, action, title, text,
  metadata_json, ref_kind, ref_key, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(source, source_event_id) DO UPDATE SET
  occurred_at = excluded.occurred_at,
  app = excluded.app,
  window_title = excluded.window_title,
  url = excluded.url,
  action = excluded.action,
  title = excluded.title,
  text = excluded.text,
  metadata_json = excluded.metadata_json,
  ref_kind = excluded.ref_kind,
  ref_key = excluded.ref_key
`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	changed := 0
	for _, event := range events {
		if event.Source == "" || event.SourceEventID == "" || event.OccurredAt.IsZero() || event.Action == "" || event.Title == "" {
			return 0, fmt.Errorf("invalid event source=%q id=%q action=%q title=%q", event.Source, event.SourceEventID, event.Action, event.Title)
		}
		metadata, err := marshalNullable(event.Metadata)
		if err != nil {
			return 0, err
		}
		metadataValue := ""
		if metadata != nil {
			metadataValue = *metadata
		}
		exists, same, err := existingEventSame(ctx, existingStmt, event, metadataValue)
		if err != nil {
			return 0, err
		}
		if exists && same {
			continue
		}
		if _, err := stmt.ExecContext(ctx,
			event.Source,
			event.SourceEventID,
			event.OccurredAt.UTC().Format(time.RFC3339Nano),
			event.App,
			event.WindowTitle,
			event.URL,
			event.Action,
			event.Title,
			event.Text,
			metadata,
			event.RefKind,
			event.RefKey,
			now,
		); err != nil {
			return 0, err
		}
		changed++
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return changed, nil
}

func dedupeEvents(events []Event) []Event {
	positions := make(map[string]int, len(events))
	out := make([]Event, 0, len(events))
	for _, event := range events {
		key := event.Source + "\x00" + event.SourceEventID
		position, ok := positions[key]
		if ok {
			out[position] = event
			continue
		}
		positions[key] = len(out)
		out = append(out, event)
	}
	return out
}

func existingEventSame(ctx context.Context, stmt *sql.Stmt, event Event, metadata string) (bool, bool, error) {
	var occurredAt string
	var app string
	var windowTitle string
	var url string
	var action string
	var title string
	var text string
	var existingMetadata string
	var refKind string
	var refKey string
	err := stmt.QueryRowContext(ctx, event.Source, event.SourceEventID).Scan(
		&occurredAt,
		&app,
		&windowTitle,
		&url,
		&action,
		&title,
		&text,
		&existingMetadata,
		&refKind,
		&refKey,
	)
	if err == sql.ErrNoRows {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	same := occurredAt == event.OccurredAt.UTC().Format(time.RFC3339Nano) &&
		app == event.App &&
		windowTitle == event.WindowTitle &&
		url == event.URL &&
		action == event.Action &&
		title == event.Title &&
		text == event.Text &&
		existingMetadata == metadata &&
		refKind == event.RefKind &&
		refKey == event.RefKey
	return true, same, nil
}

func marshalNullable(value map[string]any) (*string, error) {
	if len(value) == 0 {
		return nil, nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	out := string(payload)
	return &out, nil
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS activity_events (
  id INTEGER PRIMARY KEY,
  source TEXT NOT NULL,
  source_event_id TEXT NOT NULL,
  occurred_at TEXT NOT NULL,
  app TEXT NOT NULL DEFAULT '',
  window_title TEXT NOT NULL DEFAULT '',
  url TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL,
  title TEXT NOT NULL,
  text TEXT NOT NULL DEFAULT '',
  metadata_json TEXT,
  ref_kind TEXT NOT NULL DEFAULT '',
  ref_key TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE(source, source_event_id)
);

CREATE INDEX IF NOT EXISTS idx_activity_events_time ON activity_events(occurred_at);
CREATE INDEX IF NOT EXISTS idx_activity_events_source_time ON activity_events(source, occurred_at);
CREATE INDEX IF NOT EXISTS idx_activity_events_action_time ON activity_events(action, occurred_at);
CREATE INDEX IF NOT EXISTS idx_activity_events_url ON activity_events(url);
CREATE INDEX IF NOT EXISTS idx_activity_events_ref ON activity_events(ref_kind, ref_key);

CREATE TABLE IF NOT EXISTS source_cursors (
  source TEXT PRIMARY KEY,
  cursor_json TEXT NOT NULL DEFAULT '{}',
  last_sync_at TEXT NOT NULL DEFAULT '',
  last_success_at TEXT,
  last_error TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS activity_refs (
  ref_key TEXT PRIMARY KEY,
  source TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  storage_path TEXT NOT NULL,
  metadata_json TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
`
