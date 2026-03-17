//go:build desktop

package sqlitepgx

import (
	"testing"
	"time"
)

func TestParseTimeAcceptsSQLiteDriverTimeString(t *testing.T) {
	got, err := parseTime("2026-03-17 06:10:25.123456789 +0000 UTC")
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}

	want := time.Date(2026, time.March, 17, 6, 10, 25, 123456789, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("unexpected parsed time: got %s want %s", got.Format(time.RFC3339Nano), want.Format(time.RFC3339Nano))
	}
}
