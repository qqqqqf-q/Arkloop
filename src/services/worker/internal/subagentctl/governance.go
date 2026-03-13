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

type BackpressureConfig struct {
	Enabled        bool
	QueueThreshold int    // 单 root run 下排队数触发背压
	Strategy       string // "serial" | "reject" | "pause"
}

type BackpressureResult struct {
	Level    BackpressureLevel
	Strategy string
}

type BackpressureLevel int

const (
	BackpressureNone     BackpressureLevel = iota
	BackpressureCritical
)

const (
	BackpressureStrategySerial = "serial"
	BackpressureStrategyReject = "reject"
	BackpressureStrategyPause  = "pause"
)

type SpawnGovernor struct {
	limits       SubAgentLimits
	backpressure BackpressureConfig
}

func NewSpawnGovernor(limits SubAgentLimits, bp BackpressureConfig) *SpawnGovernor {
	return &SpawnGovernor{limits: limits, backpressure: bp}
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

func (g *SpawnGovernor) EvaluateBackpressure(ctx context.Context, tx pgx.Tx, rootRunID uuid.UUID) (BackpressureResult, error) {
	if !g.backpressure.Enabled || g.backpressure.QueueThreshold <= 0 {
		return BackpressureResult{Level: BackpressureNone}, nil
	}
	count, err := (data.SubAgentRepository{}).CountActiveByRootRun(ctx, tx, rootRunID)
	if err != nil {
		return BackpressureResult{}, fmt.Errorf("evaluate backpressure: %w", err)
	}
	if count >= g.backpressure.QueueThreshold {
		return BackpressureResult{Level: BackpressureCritical, Strategy: g.backpressure.Strategy}, nil
	}
	return BackpressureResult{Level: BackpressureNone}, nil
}

func (g *SpawnGovernor) ValidateBackpressureForSpawn(ctx context.Context, tx pgx.Tx, rootRunID uuid.UUID) error {
	result, err := g.EvaluateBackpressure(ctx, tx, rootRunID)
	if err != nil {
		return err
	}
	if result.Level == BackpressureCritical && result.Strategy == BackpressureStrategyReject {
		count, _ := (data.SubAgentRepository{}).CountActiveByRootRun(ctx, tx, rootRunID)
		return fmt.Errorf("spawn rejected: backpressure threshold %d reached (active: %d)", g.backpressure.QueueThreshold, count)
	}
	return nil
}

func (g *SpawnGovernor) ValidateBackpressureForResume(ctx context.Context, tx pgx.Tx, rootRunID uuid.UUID) error {
	result, err := g.EvaluateBackpressure(ctx, tx, rootRunID)
	if err != nil {
		return err
	}
	if result.Level == BackpressureCritical {
		return fmt.Errorf("resume rejected: backpressure threshold reached")
	}
	return nil
}

func (g *SpawnGovernor) ValidateBackpressureForSendInput(ctx context.Context, tx pgx.Tx, rootRunID uuid.UUID, isInterrupt bool) error {
	result, err := g.EvaluateBackpressure(ctx, tx, rootRunID)
	if err != nil {
		return err
	}
	if result.Level == BackpressureCritical && !isInterrupt {
		return fmt.Errorf("send_input rejected: backpressure threshold reached, only interrupt allowed")
	}
	return nil
}
