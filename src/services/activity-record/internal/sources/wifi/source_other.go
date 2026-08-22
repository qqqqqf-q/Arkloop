//go:build !darwin

package wifi

import (
	"context"
	"fmt"
	"runtime"

	"arkloop/services/activity-record/internal/store"
)

type Source struct{}

func New() *Source { return &Source{} }
func (s *Source) Name() string { return "wifi" }
func (s *Source) Sync(_ context.Context, _ *store.Store) (int, error) { return 0, nil }
func (s *Source) Run(ctx context.Context, _ *store.Store, events chan<- store.Event) error {
	return fmt.Errorf("wifi: not implemented on %s", runtime.GOOS)
}
