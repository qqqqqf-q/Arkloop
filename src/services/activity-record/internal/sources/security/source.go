//go:build darwin

package security

import (
	"context"
	"fmt"
	"log"

	"arkloop/services/activity-record/internal/store"
)

type Source struct{}

func New() *Source { return &Source{} }

func (s *Source) Name() string { return "security" }

func (s *Source) Sync(_ context.Context, _ *store.Store) (int, error) { return 0, nil }

func (s *Source) Run(ctx context.Context, _ *store.Store, events chan<- store.Event) error {
	ch := make(chan secEvent, 64)

	go func() {
		if err := listenSecurity(ctx, ch); err != nil && ctx.Err() == nil {
			log.Printf("security: %v", err)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-ch:
			if !ok {
				return nil
			}
			action := "screen_unlocked"
			title := "screen unlocked"
			if ev.IsLock {
				action = "screen_locked"
				title = "screen locked"
			}
			events <- store.Event{
				Source:        "security",
				SourceEventID: fmt.Sprintf("security:%s:%d", action, ev.At.UnixMilli()),
				OccurredAt:    ev.At,
				Action:        action,
				Title:         title,
			}
		}
	}
}
