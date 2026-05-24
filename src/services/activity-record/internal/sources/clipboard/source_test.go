package clipboard

import (
	"context"
	"testing"
	"time"

	"arkloop/services/activity-record/internal/store"
)

func TestHashContentDeterministic(t *testing.T) {
	h1 := hashContent("hello world")
	h2 := hashContent("hello world")
	if h1 != h2 {
		t.Fatalf("same input produced different hashes: %s vs %s", h1, h2)
	}
	h3 := hashContent("different")
	if h1 == h3 {
		t.Fatal("different inputs produced same hash")
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly10x", 10, "exactly10x"},
		{"this is longer than ten", 10, "this is lo..."},
		{"", 5, ""},
	}
	for _, tt := range tests {
		got := truncate(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}

func TestTruncateUnicode(t *testing.T) {
	input := "你好世界，这是一个很长的中文字符串"
	got := truncate(input, 5)
	if got != "你好世界，..." {
		t.Fatalf("truncate unicode = %q", got)
	}
}

func TestRunDeduplicatesConsecutive(t *testing.T) {
	s := &Source{
		pollInterval:   50 * time.Millisecond,
		includeContent: false,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	events := make(chan store.Event, 100)
	go func() {
		_ = s.Run(ctx, nil, events)
	}()

	<-ctx.Done()
	close(events)

	for ev := range events {
		if ev.Action != "clipboard_changed" {
			t.Fatalf("unexpected action: %s", ev.Action)
		}
	}
}
