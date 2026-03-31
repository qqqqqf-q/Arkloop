package jobs

import (
	"context"
	"log/slog"
	"time"

	"arkloop/services/api/internal/data"
)

const (
	storageGovernanceInterval = time.Hour
	channelLedgerRetention    = 14 * 24 * time.Hour
	heartbeatInactiveWindow   = 24 * time.Hour
)

type StorageGovernance struct {
	channelLedgerRepo *data.ChannelMessageLedgerRepository
	triggerRepo       data.ScheduledTriggersRepository
	db                data.Querier
	logger            *slog.Logger
}

func NewStorageGovernance(
	db data.Querier,
	channelLedgerRepo *data.ChannelMessageLedgerRepository,
	logger *slog.Logger,
) *StorageGovernance {
	return &StorageGovernance{
		channelLedgerRepo: channelLedgerRepo,
		triggerRepo:       data.ScheduledTriggersRepository{},
		db:                db,
		logger:            logger,
	}
}

func (g *StorageGovernance) Run(ctx context.Context) {
	g.reap(ctx)
	ticker := time.NewTicker(storageGovernanceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			g.reap(ctx)
		}
	}
}

func (g *StorageGovernance) reap(ctx context.Context) {
	if g.channelLedgerRepo != nil {
		count, err := g.channelLedgerRepo.DeleteOlderThan(ctx, time.Now().UTC().Add(-channelLedgerRetention))
		if err != nil {
			g.logger.Error("channel ledger reap failed", "error", err.Error())
		} else if count > 0 {
			g.logger.Info("channel ledger reaped", "count", count)
		}
	}
	if g.db != nil {
		count, err := g.triggerRepo.DeleteInactiveHeartbeats(ctx, g.db, heartbeatInactiveWindow)
		if err != nil {
			g.logger.Error("inactive heartbeat reap failed", "error", err.Error())
		} else if count > 0 {
			g.logger.Info("inactive heartbeats reaped", "count", count)
		}
	}
}
