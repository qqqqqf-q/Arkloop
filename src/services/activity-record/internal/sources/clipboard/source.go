package clipboard

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"arkloop/services/activity-record/internal/store"
)

type Source struct {
	pollInterval   time.Duration
	includeContent bool
}

func New(includeContent bool) *Source {
	return &Source{
		pollInterval:   2 * time.Second,
		includeContent: includeContent,
	}
}

func (s *Source) Name() string { return "clipboard" }

func (s *Source) Sync(_ context.Context, _ *store.Store) (int, error) {
	return 0, nil
}

func (s *Source) Run(ctx context.Context, _ *store.Store, events chan<- store.Event) error {
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	var lastHash string

	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			text, err := clipboardText()
			if err != nil || text == "" {
				continue
			}
			hash := hashContent(text)
			if hash == lastHash {
				continue
			}
			lastHash = hash

			title := truncate(text, 200)
			eventText := ""
			if s.includeContent {
				eventText = text
			}

			events <- store.Event{
				Source:        "clipboard",
				SourceEventID: fmt.Sprintf("clipboard:%d:%s", now.UnixMilli(), hash[:12]),
				OccurredAt:    now,
				Action:        "clipboard_changed",
				Title:         title,
				Text:          eventText,
				Metadata: map[string]any{
					"content_type":   "text",
					"content_length": len(text),
				},
			}
		}
	}
}

func hashContent(text string) string {
	sum := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%x", sum)
}

func truncate(text string, maxLen int) string {
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	return string(runes[:maxLen]) + "..."
}
