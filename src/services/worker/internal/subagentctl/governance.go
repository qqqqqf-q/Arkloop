package subagentctl

import (
	"context"
	"fmt"

	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type SubAgentLimits struct {
	MaxDepth                 int
	MaxActivePerRootRun      int
	MaxParallelChildren      int
	MaxDescendantsPerRootRun int
	MaxPendingPerRootRun     int
}

type SpawnGovernor struct {
	limits SubAgentLimits
}

func NewSpawnGovernor(limits SubAgentLimits) *SpawnGovernor {
	return &SpawnGovernor{limits: limits}
}

func (g *SpawnGovernor) ValidateSpawn(ctx context.Context, tx pgx.Tx, parentRun data.Run, rootRunID uuid.UUID, depth int) error {
	if g.limits.MaxDepth > 0 && depth > g.limits.MaxDepth {
		return fmt.Errorf("sub-agent depth %d exceeds limit %d", depth, g.limits.MaxDepth)
	}

	repo := data.SubAgentRepository{}

	if g.limits.MaxActivePerRootRun > 0 {
		count, err := repo.CountActiveByRootRun(ctx, tx, rootRunID)
		if err != nil {
			return fmt.Errorf("count active sub-agents: %w", err)
		}
		if count >= g.limits.MaxActivePerRootRun {
			return fmt.Errorf("active sub-agent count %d reached limit %d for root run", count, g.limits.MaxActivePerRootRun)
		}
	}

	if g.limits.MaxParallelChildren > 0 {
		count, err := repo.CountActiveByParentRun(ctx, tx, parentRun.ID)
		if err != nil {
			return fmt.Errorf("count parallel children: %w", err)
		}
		if count >= g.limits.MaxParallelChildren {
			return fmt.Errorf("parallel children count %d reached limit %d", count, g.limits.MaxParallelChildren)
		}
	}

	if g.limits.MaxDescendantsPerRootRun > 0 {
		count, err := repo.CountByRootRun(ctx, tx, rootRunID)
		if err != nil {
			return fmt.Errorf("count descendants: %w", err)
		}
		if count >= g.limits.MaxDescendantsPerRootRun {
			return fmt.Errorf("descendant count %d reached limit %d for root run", count, g.limits.MaxDescendantsPerRootRun)
		}
	}

	return nil
}

func (g *SpawnGovernor) ValidatePendingInput(ctx context.Context, tx pgx.Tx, rootRunID uuid.UUID) error {
	if g.limits.MaxPendingPerRootRun <= 0 {
		return nil
	}
	count, err := (data.SubAgentPendingInputsRepository{}).CountByRootRun(ctx, tx, rootRunID)
	if err != nil {
		return fmt.Errorf("count pending inputs: %w", err)
	}
	if count >= g.limits.MaxPendingPerRootRun {
		return fmt.Errorf("pending input count %d reached limit %d for root run", count, g.limits.MaxPendingPerRootRun)
	}
	return nil
}
