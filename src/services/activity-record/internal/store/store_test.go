package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestUpsertEventsIsIdempotent(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "activity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	event := Event{
		Source:        "test",
		SourceEventID: "event-1",
		OccurredAt:    time.Date(2026, 5, 21, 1, 2, 3, 0, time.UTC),
		Action:        "visited",
		Title:         "First",
	}
	if inserted, err := db.UpsertEvents(context.Background(), []Event{event}); err != nil || inserted != 1 {
		t.Fatalf("first upsert inserted=%d err=%v", inserted, err)
	}
	event.Title = "Updated"
	if inserted, err := db.UpsertEvents(context.Background(), []Event{event}); err != nil || inserted != 1 {
		t.Fatalf("second upsert inserted=%d err=%v", inserted, err)
	}
	if inserted, err := db.UpsertEvents(context.Background(), []Event{event}); err != nil || inserted != 0 {
		t.Fatalf("third upsert inserted=%d err=%v", inserted, err)
	}
	var count int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM activity_events`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one row, got %d", count)
	}
	var title string
	if err := db.db.QueryRow(`SELECT title FROM activity_events WHERE source = 'test' AND source_event_id = 'event-1'`).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "Updated" {
		t.Fatalf("expected updated title, got %q", title)
	}
}

func TestUpsertEventsDeduplicatesInputBatch(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "activity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	first := Event{
		Source:        "test",
		SourceEventID: "event-1",
		OccurredAt:    time.Date(2026, 5, 21, 1, 2, 3, 0, time.UTC),
		Action:        "visited",
		Title:         "First",
	}
	second := first
	second.Title = "Second"
	changed, err := db.UpsertEvents(context.Background(), []Event{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if changed != 1 {
		t.Fatalf("expected one changed row, got %d", changed)
	}
	var count int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM activity_events`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one row, got %d", count)
	}
	var title string
	if err := db.db.QueryRow(`SELECT title FROM activity_events WHERE source = 'test' AND source_event_id = 'event-1'`).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "Second" {
		t.Fatalf("expected last event to win, got %q", title)
	}
}

func TestCursorRoundTrip(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "activity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	type cursorState struct {
		Last int `json:"last"`
	}
	if err := db.SaveCursor(context.Background(), "test", cursorState{Last: 42}, nil); err != nil {
		t.Fatal(err)
	}
	var got cursorState
	if err := db.Cursor(context.Background(), "test", &got); err != nil {
		t.Fatal(err)
	}
	if got.Last != 42 {
		t.Fatalf("expected cursor 42, got %d", got.Last)
	}
}
