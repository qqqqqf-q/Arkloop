//go:build desktop

package skillseed

import (
	"context"
	"fmt"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
)

// SeedDesktopSkills scans the built-in skills directory and synchronises any
// new or changed skills into the local SQLite database and filesystem object
// store. It skips the PostgreSQL advisory-lock election used in the cloud
// seeder — desktop runs as a single process so no coordination is needed.
func SeedDesktopSkills(
	ctx context.Context,
	root string,
	repo *data.SkillPackagesRepository,
	store objectStore,
	logger *observability.JSONLogger,
) error {
	if root == "" {
		return fmt.Errorf("skills root must not be empty")
	}
	if repo == nil {
		return fmt.Errorf("skill packages repo must not be nil")
	}
	if store == nil {
		return fmt.Errorf("skill store must not be nil")
	}
	s := NewSeederDirect(root, repo, store, logger)
	return s.SyncOnceDirect(ctx)
}
