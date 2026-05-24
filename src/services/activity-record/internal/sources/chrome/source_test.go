package chrome

import (
	"database/sql"
	"testing"
	"time"
)

func TestChromeTime(t *testing.T) {
	tests := []struct {
		name     string
		input    int64
		wantYear int
	}{
		{"epoch", chromeEpochOffsetMicros, 1970},
		{"recent", 13380000000000000, 2024},
		{"zero", 0, 1601},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := chromeTime(tt.input)
			if result.Year() != tt.wantYear {
				t.Fatalf("chromeTime(%d) year=%d, want %d", tt.input, result.Year(), tt.wantYear)
			}
			if result.Location() != time.UTC {
				t.Fatalf("chromeTime should return UTC, got %v", result.Location())
			}
		})
	}
}

func TestSecondsFromNullableMicros(t *testing.T) {
	tests := []struct {
		name  string
		input sql.NullInt64
		want  float64
	}{
		{"null", sql.NullInt64{Valid: false}, 0},
		{"zero", sql.NullInt64{Int64: 0, Valid: true}, 0},
		{"negative", sql.NullInt64{Int64: -100, Valid: true}, 0},
		{"one_second", sql.NullInt64{Int64: 1000000, Valid: true}, 1.0},
		{"half_second", sql.NullInt64{Int64: 500000, Valid: true}, 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := secondsFromNullableMicros(tt.input)
			if got != tt.want {
				t.Fatalf("secondsFromNullableMicros(%v) = %f, want %f", tt.input, got, tt.want)
			}
		})
	}
}

func TestNullableInt64(t *testing.T) {
	if got := nullableInt64(sql.NullInt64{Valid: false}); got != 0 {
		t.Fatalf("expected 0 for null, got %d", got)
	}
	if got := nullableInt64(sql.NullInt64{Int64: 42, Valid: true}); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

func TestDiscoverProfiles(t *testing.T) {
	dir := t.TempDir()
	// No History files -> no profiles
	profiles := discoverProfiles([]browserDir{{"test", dir}})
	if len(profiles) != 0 {
		t.Fatalf("expected 0 profiles for empty dir, got %d", len(profiles))
	}
}
