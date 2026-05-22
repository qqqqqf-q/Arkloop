package ax

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"time"

	"arkloop/services/activity-record/internal/store"
)

type Source struct {
	captureInterval time.Duration
	idleThreshold   time.Duration
	maxDepth        int
	maxNodes        int
	walkTimeoutMs   float64
	maxTextLen      int
}

func New(idleThreshold time.Duration) *Source {
	if idleThreshold <= 0 {
		idleThreshold = 5 * time.Minute
	}
	return &Source{
		captureInterval: 5 * time.Second,
		idleThreshold:   idleThreshold,
		maxDepth:        30,
		maxNodes:        5000,
		walkTimeoutMs:   250.0,
		maxTextLen:      100 * 1024,
	}
}

func (s *Source) Name() string { return "ax" }

func (s *Source) Sync(_ context.Context, _ *store.Store) (int, error) {
	return 0, nil
}

func (s *Source) Run(ctx context.Context, _ *store.Store, events chan<- store.Event) error {
	ticker := time.NewTicker(s.captureInterval)
	defer ticker.Stop()

	var lastHash string
	permWarned := false

	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			idle, err := idleSeconds()
			if err != nil {
				continue
			}
			if time.Duration(idle)*time.Second >= s.idleThreshold {
				continue
			}

			result := walkOnThread(s.maxDepth, s.maxNodes, s.walkTimeoutMs)
			if result.Error != nil {
				if !permWarned {
					log.Printf("ax: %v", result.Error)
					permWarned = true
				}
				continue
			}
			if result.TextContent == "" {
				continue
			}

			text := result.TextContent
			if len(text) > s.maxTextLen {
				text = text[:s.maxTextLen]
			}

			hash := hashContent(text)
			if hash == lastHash {
				continue
			}
			lastHash = hash

			title := truncateRunes(result.AppName+" - "+result.WindowTitle, 200)
			meta := map[string]any{
				"element_count":    result.ElementCount,
				"text_length":      len(text),
				"walk_duration_ms": result.WalkDurationMs,
				"content_hash":     hash[:16],
				"pid":              result.PID,
			}
			if result.Truncated {
				meta["truncated"] = true
				meta["truncation_reason"] = result.TruncationReason
			}

			events <- store.Event{
				Source:        "ax",
				SourceEventID: fmt.Sprintf("ax:%d:%s", now.UnixMilli(), hash[:12]),
				OccurredAt:    now,
				App:           result.AppName,
				WindowTitle:   result.WindowTitle,
				URL:           result.BrowserURL,
				Action:        "ax_snapshot",
				Title:         title,
				Text:          text,
				Metadata:      meta,
			}
		}
	}
}

type WalkResult struct {
	AppName          string
	WindowTitle      string
	PID              int
	TextContent      string
	ElementCount     int
	Truncated        bool
	TruncationReason string
	WalkDurationMs   float64
	BrowserURL       string
	Error            error
}

func hashContent(text string) string {
	sum := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%x", sum)
}

func truncateRunes(text string, maxLen int) string {
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	return string(runes[:maxLen])
}

func CheckAXPermission() bool {
	return checkPermission()
}
