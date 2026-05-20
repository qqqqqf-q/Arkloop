package pipeline

import (
	"testing"
	"time"

	"arkloop/services/worker/internal/llm"
)

func TestFormatElapsed(t *testing.T) {
	base := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		prev     time.Time
		current  time.Time
		expected string
	}{
		{"zero prev", time.Time{}, base, ""},
		{"zero current", base, time.Time{}, ""},
		{"same time", base, base, ""},
		{"30 seconds", base, base.Add(30 * time.Second), "+30s"},
		{"90 seconds", base, base.Add(90 * time.Second), "+2m"},
		{"45 minutes", base, base.Add(45 * time.Minute), "+45m"},
		{"3 hours", base, base.Add(3 * time.Hour), "+3h"},
		{"36 hours", base, base.Add(36 * time.Hour), "+36h"},
		{"3 days", base, base.Add(72 * time.Hour), "+3d"},
		{"negative", base.Add(time.Hour), base, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatElapsed(tc.prev, tc.current)
			if got != tc.expected {
				t.Errorf("formatElapsed(%v, %v) = %q, want %q", tc.prev, tc.current, got, tc.expected)
			}
		})
	}
}

func TestFormatEnvelopeTime(t *testing.T) {
	ts := time.Date(2026, 5, 17, 14, 30, 5, 0, time.UTC)
	got := formatEnvelopeTime(ts)
	if got != "Sun 14:30:05" {
		t.Errorf("formatEnvelopeTime = %q, want %q", got, "Sun 14:30:05")
	}
}

func TestFormatEnvelopeTimeShort(t *testing.T) {
	ts := time.Date(2026, 5, 17, 14, 30, 5, 0, time.UTC)
	got := formatEnvelopeTimeShort(ts)
	if got != "Sun 14:30" {
		t.Errorf("formatEnvelopeTimeShort = %q, want %q", got, "Sun 14:30")
	}
}

func TestParseEnvelopeTime(t *testing.T) {
	tests := []struct {
		name  string
		input string
		zero  bool
	}{
		{"rfc3339", "2026-05-17T14:30:05Z", false},
		{"rfc3339nano", "2026-05-17T14:30:05.123456789Z", false},
		{"custom with tz", "2026-05-17 14:30:05 [UTC+8]", false},
		{"empty", "", true},
		{"garbage", "not-a-time", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseEnvelopeTime(tc.input)
			if tc.zero && !got.IsZero() {
				t.Errorf("expected zero time for %q, got %v", tc.input, got)
			}
			if !tc.zero && got.IsZero() {
				t.Errorf("expected non-zero time for %q", tc.input)
			}
		})
	}
}

func TestFormatInternalEnvelope(t *testing.T) {
	ts := time.Date(2026, 5, 17, 14, 30, 0, 0, time.UTC)
	got := formatInternalEnvelope(ts, "")
	if got != "[Sun 14:30]" {
		t.Errorf("without elapsed = %q, want %q", got, "[Sun 14:30]")
	}
	got = formatInternalEnvelope(ts, "+5m")
	if got != "[+5m Sun 14:30]" {
		t.Errorf("with elapsed = %q, want %q", got, "[+5m Sun 14:30]")
	}
}

func TestPrependUserMessageTimestamp(t *testing.T) {
	ts := time.Date(2026, 5, 17, 14, 30, 0, 0, time.UTC)
	prevTs := time.Date(2026, 5, 17, 14, 25, 0, 0, time.UTC)

	t.Run("plain text message", func(t *testing.T) {
		parts := []llm.ContentPart{{Type: "text", Text: "hello"}}
		result := prependUserMessageTimestamp(parts, ts, time.Time{}, nil)
		if len(result) != 1 {
			t.Fatalf("expected 1 part, got %d", len(result))
		}
		expected := "[Sun 14:30] hello"
		if result[0].Text != expected {
			t.Errorf("got %q, want %q", result[0].Text, expected)
		}
	})

	t.Run("with elapsed", func(t *testing.T) {
		parts := []llm.ContentPart{{Type: "text", Text: "hello"}}
		result := prependUserMessageTimestamp(parts, ts, prevTs, nil)
		expected := "[+5m Sun 14:30] hello"
		if result[0].Text != expected {
			t.Errorf("got %q, want %q", result[0].Text, expected)
		}
	})

	t.Run("yaml envelope skipped", func(t *testing.T) {
		parts := []llm.ContentPart{{Type: "text", Text: "---\ndisplay-name: Alice\n---\nhello"}}
		result := prependUserMessageTimestamp(parts, ts, time.Time{}, nil)
		if result[0].Text != parts[0].Text {
			t.Errorf("YAML envelope should not be modified, got %q", result[0].Text)
		}
	})

	t.Run("empty parts", func(t *testing.T) {
		result := prependUserMessageTimestamp(nil, ts, time.Time{}, nil)
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})

	t.Run("preserves other fields", func(t *testing.T) {
		parts := []llm.ContentPart{{Type: "text", Text: "hello", TrustSource: "user"}}
		result := prependUserMessageTimestamp(parts, ts, time.Time{}, nil)
		if result[0].TrustSource != "user" {
			t.Errorf("TrustSource lost, got %q", result[0].TrustSource)
		}
	})

	t.Run("does not mutate original", func(t *testing.T) {
		parts := []llm.ContentPart{{Type: "text", Text: "hello"}}
		_ = prependUserMessageTimestamp(parts, ts, time.Time{}, nil)
		if parts[0].Text != "hello" {
			t.Errorf("original mutated to %q", parts[0].Text)
		}
	})

	t.Run("uses user timezone", func(t *testing.T) {
		utcTs := time.Date(2026, 5, 17, 6, 30, 0, 0, time.UTC)
		loc, _ := time.LoadLocation("Asia/Shanghai")
		parts := []llm.ContentPart{{Type: "text", Text: "hello"}}
		result := prependUserMessageTimestamp(parts, utcTs, time.Time{}, loc)
		expected := "[Sun 14:30] hello"
		if result[0].Text != expected {
			t.Errorf("got %q, want %q", result[0].Text, expected)
		}
	})
}
